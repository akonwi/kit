package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/subagent"
)

const (
	maxRequestMessageBytes             = 16 << 10
	maxRequestReplyBytes               = 16 << 10
	maxOutstandingRequestsPerOwner     = 256
	maxOutstandingRequestsPerRecipient = 64
	maxRequestLifetime                 = 24 * time.Hour
	maxRequestIdentityBytes            = 256
)

// CreateSubagentRequest durably records a reply-capable request and its pending
// inbox delivery. It never loads either child's droids store.
func (s *Store) CreateSubagentRequest(ctx context.Context, admission subagent.RequestAdmission) (subagent.Request, error) {
	if s == nil || s.db == nil {
		return subagent.Request{}, fmt.Errorf("store is closed")
	}
	admission.Message = strings.TrimSpace(admission.Message)
	if admission.OwnerSessionID == "" || admission.RecipientName == "" || admission.CallIdentity == "" ||
		admission.Message == "" || !utf8.ValidString(admission.Message) || len(admission.Message) > maxRequestMessageBytes ||
		!validRequestIdentity(admission.CallIdentity) || len(admission.RecipientName) > 128 || !utf8.ValidString(admission.RecipientName) ||
		(admission.DeliveryMode != "ask" && admission.DeliveryMode != "send") ||
		(admission.SenderConversationID != "" && admission.DeliveryMode == "ask") {
		return subagent.Request{}, subagent.ErrInvalidInput
	}
	now := admission.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	deadline := admission.Deadline.UTC()
	if deadline.IsZero() {
		deadline = now.Add(time.Hour)
	}
	if !deadline.After(now) || deadline.After(now.Add(maxRequestLifetime)) {
		return subagent.Request{}, subagent.ErrInvalidInput
	}
	senderKind, senderIdentity := "parent", "parent:"+admission.OwnerSessionID
	senderName := "parent"
	if admission.SenderConversationID != "" {
		senderKind, senderIdentity = "child", "child:"+string(admission.SenderConversationID)
	}
	id, err := identifier.New("subrequest_")
	if err != nil {
		return subagent.Request{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subagent.Request{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var persistent int
	if err := tx.QueryRowContext(ctx, `SELECT persistent FROM sessions WHERE id = ? AND archived_at IS NULL`, admission.OwnerSessionID).Scan(&persistent); err != nil {
		return subagent.Request{}, mapSubagentNotFound(err)
	}
	if persistent != 1 {
		return subagent.Request{}, subagent.ErrTemporaryUnavailable
	}
	if existing, err := loadSubagentRequestByCall(ctx, tx, admission.OwnerSessionID, senderIdentity, admission.CallIdentity); err == nil {
		if existing.RecipientName != admission.RecipientName || existing.Message != admission.Message || existing.DeliveryMode != admission.DeliveryMode {
			return subagent.Request{}, subagent.ErrConflict
		}
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return subagent.Request{}, err
	}
	if senderKind == "child" {
		sender, err := loadSubagentConversation(ctx, tx, admission.SenderConversationID, false)
		if err != nil || sender.OwnerSessionID != admission.OwnerSessionID || sender.DismissedAt != nil {
			return subagent.Request{}, subagent.ErrNotFound
		}
		senderName = sender.Agent.Name
	}
	recipient, err := scanSubagentConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+`
		FROM subagent_conversations c WHERE c.owner_session_id = ? AND c.agent_name = ? AND c.dismissed_at IS NULL`, admission.OwnerSessionID, admission.RecipientName))
	if errors.Is(err, sql.ErrNoRows) && admission.NewRecipient != nil {
		// A child may initialize a configured sibling, but cannot silently
		// recreate one the owner explicitly dismissed.
		if senderKind == "child" {
			var dismissed int
			if checkErr := tx.QueryRowContext(ctx, `SELECT 1 FROM subagent_conversations
				WHERE owner_session_id = ? AND agent_name = ? AND dismissed_at IS NOT NULL LIMIT 1`,
				admission.OwnerSessionID, admission.RecipientName).Scan(&dismissed); checkErr == nil {
				return subagent.Request{}, subagent.ErrNotFound
			} else if !errors.Is(checkErr, sql.ErrNoRows) {
				return subagent.Request{}, checkErr
			}
		}
		definition := *admission.NewRecipient
		if definition.Name != admission.RecipientName || admission.CWD == "" || admission.Model == "" || definition.Description == "" {
			return subagent.Request{}, subagent.ErrInvalidInput
		}
		recipientID, newErr := identifier.New("subagent_")
		if newErr != nil {
			return subagent.Request{}, newErr
		}
		recipient = subagent.Conversation{ID: subagent.ConversationID(recipientID), OwnerSessionID: admission.OwnerSessionID,
			Agent: definition, CWD: admission.CWD, Model: admission.Model, ThinkingLevel: admission.ThinkingLevel}
		_, err = tx.ExecContext(ctx, `INSERT INTO subagent_conversations(
			id, owner_session_id, agent_name, agent_description, agent_model, agent_instructions,
			source_kind, source_path, source_plugin_id, cwd, model, thinking_level,
			state, generation, created_at, updated_at
		) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), 'idle', 1, ?, ?)`,
			recipient.ID, admission.OwnerSessionID, definition.Name, definition.Description, definition.Model,
			definition.Instructions, definition.Source.Kind, definition.Source.Path, definition.Source.PluginID,
			recipient.CWD, recipient.Model, recipient.ThinkingLevel, formatTimestamp(now), formatTimestamp(now))
	}
	if err != nil {
		return subagent.Request{}, mapSubagentNotFound(err)
	}
	if senderKind == "child" && recipient.ID == admission.SenderConversationID {
		return subagent.Request{}, subagent.ErrInvalidInput
	}
	var ownerCount, recipientCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subagent_requests WHERE owner_session_id = ? AND state = 'open'`, admission.OwnerSessionID).Scan(&ownerCount); err != nil {
		return subagent.Request{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subagent_requests WHERE recipient_conversation_id = ? AND state = 'open'`, recipient.ID).Scan(&recipientCount); err != nil {
		return subagent.Request{}, err
	}
	if ownerCount >= maxOutstandingRequestsPerOwner || recipientCount >= maxOutstandingRequestsPerRecipient {
		return subagent.Request{}, subagent.ErrQueueFull
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subagent_requests(
		id, owner_session_id, sender_kind, sender_conversation_id, recipient_conversation_id,
		recipient_name, sender_name, sender_identity, call_identity, delivery_mode, message, state, created_at, deadline_at
	) VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)`,
		id, admission.OwnerSessionID, senderKind, admission.SenderConversationID, recipient.ID,
		recipient.Agent.Name, senderName, senderIdentity, admission.CallIdentity, admission.DeliveryMode, admission.Message,
		formatTimestamp(now), formatTimestamp(deadline)); err != nil {
		return subagent.Request{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subagent_request_deliveries(request_id, kind) VALUES (?, 'request')`, id); err != nil {
		return subagent.Request{}, err
	}
	if err := tx.Commit(); err != nil {
		return subagent.Request{}, err
	}
	return subagent.Request{ID: id, OwnerSessionID: admission.OwnerSessionID, SenderKind: senderKind,
		SenderConversationID: admission.SenderConversationID, RecipientConversationID: recipient.ID,
		RecipientName: recipient.Agent.Name, SenderName: senderName, SenderIdentity: senderIdentity, CallIdentity: admission.CallIdentity,
		DeliveryMode: admission.DeliveryMode, Message: admission.Message, State: subagent.RequestOpen,
		CreatedAt: now, DeadlineAt: deadline}, nil
}

// ReplySubagentRequest commits one reply from the addressed child and records
// durable return delivery. A different tool call cannot replace a first reply.
func (s *Store) ReplySubagentRequest(ctx context.Context, recipient subagent.ConversationID, receipt, callIdentity, message string) (subagent.Request, error) {
	message = strings.TrimSpace(message)
	if recipient == "" || receipt == "" || callIdentity == "" || message == "" ||
		!utf8.ValidString(message) || len(message) > maxRequestReplyBytes || !validRequestIdentity(callIdentity) {
		return subagent.Request{}, subagent.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subagent.Request{}, err
	}
	defer func() { _ = tx.Rollback() }()
	request, err := loadSubagentRequest(ctx, tx, receipt)
	if err != nil {
		return subagent.Request{}, mapSubagentNotFound(err)
	}
	if request.RecipientConversationID != recipient {
		return subagent.Request{}, subagent.ErrNotFound
	}
	if request.State != subagent.RequestOpen {
		var recordedCall string
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(reply_call_identity, '') FROM subagent_requests WHERE id = ?`, receipt).Scan(&recordedCall); err == nil && recordedCall == callIdentity && request.State == subagent.RequestReplied && request.Reply == message {
			return request, nil
		}
		return subagent.Request{}, subagent.ErrConflict
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subagent_conversations WHERE id = ? AND owner_session_id = ? AND dismissed_at IS NULL`, recipient, request.OwnerSessionID).Scan(&active); err != nil || active != 1 {
		return subagent.Request{}, subagent.ErrNotFound
	}
	now := time.Now().UTC()
	if !now.Before(request.DeadlineAt) {
		return subagent.Request{}, subagent.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subagent_requests SET state = 'replied', reply = ?, reply_call_identity = ?, resolved_at = ? WHERE id = ? AND state = 'open'`, message, callIdentity, formatTimestamp(now), receipt); err != nil {
		return subagent.Request{}, err
	}
	if request.SenderKind == "child" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO subagent_request_deliveries(request_id, kind) VALUES (?, 'reply')`, receipt); err != nil {
			return subagent.Request{}, err
		}
	} else if request.DeliveryMode == "send" {
		if err := insertParentRequestDelivery(ctx, tx, request.ID, request.OwnerSessionID,
			request.RecipientName, subagent.TaskCompleted, message, "", now); err != nil {
			return subagent.Request{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return subagent.Request{}, err
	}
	request.State, request.Reply, request.ResolvedAt = subagent.RequestReplied, message, &now
	return request, nil
}

func validRequestIdentity(identity string) bool {
	return identity != "" && len(identity) <= maxRequestIdentityBytes && utf8.ValidString(identity) && !strings.ContainsRune(identity, 0)
}

func loadSubagentRequestByCall(ctx context.Context, tx *sql.Tx, owner, sender, call string) (subagent.Request, error) {
	return scanSubagentRequest(tx.QueryRowContext(ctx, `SELECT `+requestColumns+` FROM subagent_requests WHERE owner_session_id = ? AND sender_identity = ? AND call_identity = ?`, owner, sender, call))
}

const requestColumns = `id, owner_session_id, sender_kind, COALESCE(sender_conversation_id, ''),
	COALESCE(recipient_conversation_id, ''), recipient_name, sender_name, sender_identity, call_identity,
	delivery_mode, message, state, COALESCE(reply, ''), COALESCE(failure, ''),
	created_at, deadline_at, resolved_at`

func loadSubagentRequest(ctx context.Context, tx *sql.Tx, receipt string) (subagent.Request, error) {
	return scanSubagentRequest(tx.QueryRowContext(ctx, `SELECT `+requestColumns+` FROM subagent_requests WHERE id = ?`, receipt))
}

func scanSubagentRequest(row interface{ Scan(...any) error }) (subagent.Request, error) {
	var request subagent.Request
	var created, deadline string
	var resolved sql.NullString
	if err := row.Scan(&request.ID, &request.OwnerSessionID, &request.SenderKind, &request.SenderConversationID,
		&request.RecipientConversationID, &request.RecipientName, &request.SenderName, &request.SenderIdentity, &request.CallIdentity,
		&request.DeliveryMode, &request.Message, &request.State, &request.Reply, &request.Failure,
		&created, &deadline, &resolved); err != nil {
		return subagent.Request{}, err
	}
	var err error
	request.CreatedAt, err = parseTimestamp(created)
	if err != nil {
		return subagent.Request{}, err
	}
	request.DeadlineAt, err = parseTimestamp(deadline)
	if err != nil {
		return subagent.Request{}, err
	}
	if resolved.Valid {
		at, err := parseTimestamp(resolved.String)
		if err != nil {
			return subagent.Request{}, err
		}
		request.ResolvedAt = &at
	}
	return request, nil
}

// PendingSubagentDeliveryOwners returns owners with undelivered inbox work.
func (s *Store) PendingSubagentDeliveryOwners(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > 1024 {
		return nil, subagent.ErrInvalidInput
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT r.owner_session_id
		FROM subagent_request_deliveries d JOIN subagent_requests r ON r.id = d.request_id
		JOIN subagent_conversations c ON c.id = CASE WHEN d.kind = 'request' THEN r.recipient_conversation_id ELSE r.sender_conversation_id END
		WHERE d.task_id IS NULL AND c.dismissed_at IS NULL
		AND (d.kind = 'reply' OR (r.state = 'open' AND r.deadline_at > ?))
		ORDER BY r.owner_session_id LIMIT ?`, formatTimestamp(time.Now().UTC()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var owners []string
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return nil, err
		}
		owners = append(owners, owner)
	}
	return owners, rows.Err()
}

// AdmitPendingSubagentDelivery converts one owner's pending inbox delivery into
// an ordinary queued child task. A full conversation queue is skipped.
func (s *Store) AdmitPendingSubagentDelivery(ctx context.Context, owner string, limits subagent.Limits) (subagent.Task, error) {
	if err := limits.Validate(); err != nil {
		return subagent.Task{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subagent.Task{}, err
	}
	defer func() { _ = tx.Rollback() }()
	type candidate struct {
		receipt, kind  string
		conversationID subagent.ConversationID
	}
	rows, err := tx.QueryContext(ctx, `SELECT d.request_id, d.kind,
		CASE WHEN d.kind = 'request' THEN r.recipient_conversation_id ELSE r.sender_conversation_id END
		FROM subagent_request_deliveries d JOIN subagent_requests r ON r.id = d.request_id
		JOIN subagent_conversations c ON c.id = CASE WHEN d.kind = 'request' THEN r.recipient_conversation_id ELSE r.sender_conversation_id END
		WHERE d.task_id IS NULL AND r.owner_session_id = ? AND c.dismissed_at IS NULL
		AND (d.kind = 'reply' OR (r.state = 'open' AND r.deadline_at > ?))
		ORDER BY r.created_at, d.request_id, d.kind LIMIT 256`, owner, formatTimestamp(time.Now().UTC()))
	if err != nil {
		return subagent.Task{}, err
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.receipt, &item.kind, &item.conversationID); err != nil {
			_ = rows.Close()
			return subagent.Task{}, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return subagent.Task{}, err
	}
	_ = rows.Close()
	var receipt, kind string
	var conversation subagent.Conversation
	found := false
	for _, item := range candidates {
		conversation, err = loadSubagentConversation(ctx, tx, item.conversationID, false)
		if err != nil {
			return subagent.Task{}, err
		}
		if err := checkQueueBounds(ctx, tx, conversation.ID, conversation.OwnerSessionID, limits); errors.Is(err, subagent.ErrQueueFull) {
			continue
		} else if err != nil {
			return subagent.Task{}, err
		}
		receipt, kind, found = item.receipt, item.kind, true
		break
	}
	if !found {
		return subagent.Task{}, subagent.ErrNotFound
	}
	request, err := loadSubagentRequest(ctx, tx, receipt)
	if err != nil {
		return subagent.Task{}, err
	}
	message := "Subagent request " + receipt + " from " + request.SenderName + ":\n" + request.Message + "\nUse subagent_reply with this receipt to answer."
	origin := subagent.TaskOriginRequest
	if kind == "reply" {
		origin = subagent.TaskOriginReply
		message = "Subagent reply to " + receipt + " from " + request.RecipientName + ":\n" + request.Reply
		if request.State != subagent.RequestReplied {
			message = "Subagent request " + receipt + " failed: " + request.Failure
		}
	}
	taskID, err := identifier.New("task_")
	if err != nil {
		return subagent.Task{}, err
	}
	var sequence uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM subagent_tasks WHERE conversation_id = ?`, conversation.ID).Scan(&sequence); err != nil {
		return subagent.Task{}, err
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO subagent_tasks(
		id, conversation_id, owner_session_id, sequence, message, state,
		cancellation_generation, queued_at, origin_kind, request_id
	) VALUES (?, ?, ?, ?, ?, 'queued', 1, ?, ?, ?)`, taskID, conversation.ID,
		conversation.OwnerSessionID, sequence, message, formatTimestamp(now), origin, receipt); err != nil {
		return subagent.Task{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subagent_request_deliveries SET task_id = ? WHERE request_id = ? AND kind = ? AND task_id IS NULL`, taskID, receipt, kind); err != nil {
		return subagent.Task{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subagent_conversations SET state = 'running', updated_at = ? WHERE id = ?`, formatTimestamp(now), conversation.ID); err != nil {
		return subagent.Task{}, err
	}
	if err := insertSubagentEvent(ctx, tx, conversation.OwnerSessionID, conversation.ID, subagent.TaskID(taskID), "task.queued", now); err != nil {
		return subagent.Task{}, err
	}
	task, err := loadSubagentTask(ctx, tx, subagent.TaskID(taskID))
	if err != nil {
		return subagent.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return subagent.Task{}, err
	}
	return task, nil
}

// InspectSubagentRequest returns a receipt only to its sender or addressed child.
func (s *Store) InspectSubagentRequest(ctx context.Context, owner string, caller subagent.ConversationID, receipt string) (subagent.Request, error) {
	request, err := scanSubagentRequest(s.db.QueryRowContext(ctx, `SELECT `+requestColumns+` FROM subagent_requests WHERE id = ? AND owner_session_id = ?`, receipt, owner))
	if err != nil {
		return subagent.Request{}, mapSubagentNotFound(err)
	}
	var archived sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT archived_at FROM sessions WHERE id = ?`, owner).Scan(&archived); err != nil {
		return subagent.Request{}, mapSubagentNotFound(err)
	}
	if archived.Valid {
		return subagent.Request{}, subagent.ErrNotFound
	}
	if caller == "" && request.SenderKind == "parent" {
		if request.DeliveryMode == "send" && request.State != subagent.RequestOpen {
			var delivered sql.NullString
			if err := s.db.QueryRowContext(ctx, `SELECT delivered_at FROM subagent_parent_deliveries WHERE kind = 'request' AND request_id = ?`, receipt).Scan(&delivered); err != nil {
				return subagent.Request{}, err
			}
			if !delivered.Valid {
				// Inspection cannot race ahead of the automatic result path and
				// provoke a second parent reaction with the same answer.
				request.Reply, request.Failure, request.DeliveryPending = "", "", true
			}
		}
		return request, nil
	}
	if caller != "" && (caller == request.SenderConversationID || caller == request.RecipientConversationID) {
		return request, nil
	}
	return subagent.Request{}, subagent.ErrNotFound
}

// ListSubagentInbox lists outstanding requests addressed to a child in stable
// oldest-first order. Cursor is the prior request ID, not a storage offset.
func (s *Store) ListSubagentInbox(ctx context.Context, owner string, child subagent.ConversationID, after string, limit int) ([]subagent.Request, error) {
	if child == "" || limit < 1 || limit > 64 {
		return nil, subagent.ErrInvalidInput
	}
	if after != "" {
		var exists int
		err := s.db.QueryRowContext(ctx, `SELECT 1 FROM subagent_requests WHERE id = ? AND owner_session_id = ? AND recipient_conversation_id = ?`, after, owner, child).Scan(&exists)
		if err != nil {
			return nil, mapSubagentNotFound(err)
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+requestColumns+` FROM subagent_requests
		WHERE owner_session_id = ? AND recipient_conversation_id = ? AND state = 'open'
		AND (created_at, id) > (COALESCE((SELECT created_at FROM subagent_requests WHERE id = ? AND recipient_conversation_id = ?), ''), ?)
		ORDER BY created_at, id LIMIT ?`, owner, child, after, child, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []subagent.Request
	for rows.Next() {
		// Rows and Row both implement Scan; scanSubagentRequest accepts either.
		request, err := scanSubagentRequest(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, request)
	}
	return result, rows.Err()
}

// settleDismissedSenderRequests cancels requests whose child sender can no
// longer receive an answer. Undelivered request and reply records have no live
// destination and must not remain pending after the sender is tombstoned.
func settleDismissedSenderRequests(ctx context.Context, tx *sql.Tx, sender subagent.ConversationID, now time.Time) ([]subagent.Task, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE subagent_requests
		SET state = 'failed', failure = 'sender unavailable', resolved_at = ?
		WHERE sender_kind = 'child' AND sender_conversation_id = ? AND state = 'open'`,
		formatTimestamp(now), sender); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT t.id, t.conversation_id, t.owner_session_id
		FROM subagent_tasks t JOIN subagent_requests r ON r.id = t.request_id
		WHERE t.origin_kind = 'request' AND t.state = 'queued'
		AND r.sender_conversation_id = ? AND r.state = 'failed' AND r.failure = 'sender unavailable'`, sender)
	if err != nil {
		return nil, err
	}
	type queuedInbox struct {
		id           subagent.TaskID
		conversation subagent.ConversationID
		owner        string
	}
	var queued []queuedInbox
	for rows.Next() {
		var task queuedInbox
		if err := rows.Scan(&task.id, &task.conversation, &task.owner); err != nil {
			_ = rows.Close()
			return nil, err
		}
		queued = append(queued, task)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var canceled []subagent.Task
	for _, task := range queued {
		if _, err := tx.ExecContext(ctx, `UPDATE subagent_tasks
			SET state = 'aborted', cancellation_generation = cancellation_generation + 1,
			cancellation_requested_at = ?, cancellation_reason = 'sender unavailable',
			finished_at = ?, terminal_error = 'sender unavailable'
			WHERE id = ? AND state = 'queued'`, formatTimestamp(now), formatTimestamp(now), task.id); err != nil {
			return nil, err
		}
		if err := updateConversationAfterQueuedCancel(ctx, tx, task.conversation, now); err != nil {
			return nil, err
		}
		if err := insertSubagentEvent(ctx, tx, task.owner, task.conversation, task.id, "task.aborted", now); err != nil {
			return nil, err
		}
		updated, err := loadSubagentTask(ctx, tx, task.id)
		if err != nil {
			return nil, err
		}
		canceled = append(canceled, updated)
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM subagent_request_deliveries
		WHERE task_id IS NULL AND request_id IN (
			SELECT id FROM subagent_requests
			WHERE sender_kind = 'child' AND sender_conversation_id = ?
		)`, sender)
	return canceled, err
}

// settleArchivedOwnerRequests closes the entire routing domain when its owner
// archives; neither its children nor parent can consume undelivered results.
func settleArchivedOwnerRequests(ctx context.Context, tx *sql.Tx, owner string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE subagent_requests
		SET state = 'failed', failure = 'owner session was archived', resolved_at = ?
		WHERE owner_session_id = ? AND state = 'open'`, formatTimestamp(now), owner); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM subagent_request_deliveries
		WHERE task_id IS NULL AND request_id IN (
			SELECT id FROM subagent_requests WHERE owner_session_id = ?
		)`, owner); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM subagent_parent_deliveries
		WHERE kind = 'request' AND delivered_at IS NULL AND owner_session_id = ?`, owner)
	return err
}

// SettleSubagentRequests closes expired requests and requests to dismissed
// children, then records durable return delivery for each sender.
func (s *Store) SettleSubagentRequests(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit < 1 || limit > 256 {
		return 0, subagent.ErrInvalidInput
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT r.id, r.owner_session_id, r.recipient_name, r.sender_kind, r.delivery_mode,
		CASE WHEN c.id IS NULL OR c.dismissed_at IS NOT NULL THEN 1 ELSE 0 END
		FROM subagent_requests r LEFT JOIN subagent_conversations c ON c.id = r.recipient_conversation_id
		WHERE r.state = 'open' AND (r.deadline_at <= ? OR c.id IS NULL OR c.dismissed_at IS NOT NULL)
		ORDER BY r.deadline_at, r.id LIMIT ?`, formatTimestamp(now), limit)
	if err != nil {
		return 0, err
	}
	type pending struct {
		id, owner, recipient, sender, mode string
		unavailable                        bool
	}
	var entries []pending
	for rows.Next() {
		var entry pending
		if err := rows.Scan(&entry.id, &entry.owner, &entry.recipient, &entry.sender, &entry.mode, &entry.unavailable); err != nil {
			_ = rows.Close()
			return 0, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()
	for _, entry := range entries {
		state, failure := subagent.RequestExpired, "request expired"
		if entry.unavailable {
			state, failure = subagent.RequestFailed, "recipient unavailable"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subagent_requests SET state = ?, failure = ?, resolved_at = ? WHERE id = ? AND state = 'open'`, state, failure, formatTimestamp(now), entry.id); err != nil {
			return 0, err
		}
		if entry.sender == "child" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO subagent_request_deliveries(request_id, kind) VALUES (?, 'reply')`, entry.id); err != nil {
				return 0, err
			}
		} else if entry.mode == "send" {
			if err := insertParentRequestDelivery(ctx, tx, entry.id, entry.owner,
				entry.recipient, subagent.TaskFailed, "", failure, now); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(entries), nil
}

func parentRequestDeliveryID(receipt string) string {
	return "mail_" + strings.TrimPrefix(receipt, "subrequest_")
}

// insertParentRequestDelivery snapshots one terminal parent send result in the
// same transaction as the request resolution. Its ID is the original boundary
// receipt, including for deliveries migrated from the transitional mailbox.
func insertParentRequestDelivery(ctx context.Context, tx *sql.Tx, receipt, owner, agent string, state subagent.TaskState, summary, failure string, at time.Time) error {
	id := parentRequestDeliveryID(receipt)
	_, err := tx.ExecContext(ctx, `INSERT INTO subagent_parent_deliveries(
		id, owner_session_id, kind, request_id, agent_name, task_state,
		summary, terminal_error, generation, created_at
	) VALUES (?, ?, 'request', ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), 1, ?)`,
		id, owner, receipt, agent, state, summary, failure, formatTimestamp(at))
	return err
}
