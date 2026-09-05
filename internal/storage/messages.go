package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/akonwi/kit/internal/identifier"
)

// AppendMessages appends an ordered batch to one session turn.
func (s *Store) AppendMessages(
	ctx context.Context,
	sessionID string,
	turnID string,
	messages []NewMessageRecord,
) ([]MessageRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is closed")
	}
	if len(messages) == 0 {
		return nil, nil
	}
	if sessionID == "" || turnID == "" {
		return nil, fmt.Errorf("session and turn ids are required")
	}
	assignedIDs := make(map[string]struct{}, len(messages))
	for index, message := range messages {
		if message.ID != "" {
			if !identifier.Valid(message.ID, "message_") {
				return nil, fmt.Errorf("message %d has invalid id %q", index, message.ID)
			}
			if _, duplicate := assignedIDs[message.ID]; duplicate {
				return nil, fmt.Errorf("message %d duplicates id %q", index, message.ID)
			}
			assignedIDs[message.ID] = struct{}{}
		}
		if !validMessageRole(message.Role) {
			return nil, fmt.Errorf("message %d has invalid role %q", index, message.Role)
		}
		if !json.Valid(message.PayloadJSON) {
			return nil, fmt.Errorf("message %d payload is not valid JSON", index)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin message append: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var turnExists int
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM turns
			WHERE id = ? AND session_id = ? AND status = 'running'
		)
	`, turnID, sessionID).Scan(&turnExists); err != nil {
		return nil, fmt.Errorf("verify running turn: %w", err)
	}
	if turnExists != 1 {
		return nil, fmt.Errorf("running turn %q: %w", turnID, ErrNotFound)
	}

	var firstSequence int64
	err = tx.QueryRowContext(ctx, `
		UPDATE sessions
		SET next_message_sequence = next_message_sequence + ?,
		    updated_at = ?
		WHERE id = ? AND archived_at IS NULL
		RETURNING next_message_sequence - ?
	`, len(messages), time.Now().UTC().Format(timestampLayout), sessionID, len(messages)).Scan(&firstSequence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("allocate message sequence: %w", err)
	}

	records := make([]MessageRecord, 0, len(messages))
	for index, message := range messages {
		messageID := message.ID
		if messageID == "" {
			messageID, err = identifier.New("message_")
			if err != nil {
				return nil, err
			}
		}
		createdAt := message.CreatedAt.UTC()
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		sequence := firstSequence + int64(index)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO messages(id, session_id, turn_id, sequence, role, payload_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, messageID, sessionID, turnID, sequence, message.Role, message.PayloadJSON, createdAt.Format(timestampLayout)); err != nil {
			return nil, fmt.Errorf("append message %q: %w", messageID, err)
		}
		records = append(records, MessageRecord{
			ID:          messageID,
			SessionID:   sessionID,
			TurnID:      turnID,
			Sequence:    sequence,
			Role:        message.Role,
			PayloadJSON: append([]byte(nil), message.PayloadJSON...),
			CreatedAt:   createdAt,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit message append: %w", err)
	}
	return records, nil
}

// CreateBashExecution appends one standalone running bash transcript message.
func (s *Store) CreateBashExecution(
	ctx context.Context,
	sessionID string,
	message NewMessageRecord,
) (MessageRecord, error) {
	if s == nil || s.db == nil {
		return MessageRecord{}, fmt.Errorf("store is closed")
	}
	if sessionID == "" || !identifier.Valid(message.ID, "bash_") {
		return MessageRecord{}, fmt.Errorf("session id and valid bash message id are required")
	}
	if message.Role != "bash" || !json.Valid(message.PayloadJSON) {
		return MessageRecord{}, fmt.Errorf("bash message role and valid payload are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MessageRecord{}, fmt.Errorf("begin bash message append: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	createdAt := message.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	var sequence int64
	err = tx.QueryRowContext(ctx, `
		UPDATE sessions
		SET next_message_sequence = next_message_sequence + 1,
		    updated_at = ?
		WHERE id = ? AND archived_at IS NULL
		RETURNING next_message_sequence - 1
	`, createdAt.Format(timestampLayout), sessionID).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageRecord{}, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	if err != nil {
		return MessageRecord{}, fmt.Errorf("allocate bash message sequence: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages(id, session_id, turn_id, sequence, role, payload_json, created_at)
		VALUES (?, ?, NULL, ?, 'bash', ?, ?)
	`, message.ID, sessionID, sequence, message.PayloadJSON, createdAt.Format(timestampLayout)); err != nil {
		return MessageRecord{}, fmt.Errorf("append bash message %q: %w", message.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return MessageRecord{}, fmt.Errorf("commit bash message append: %w", err)
	}
	return MessageRecord{
		ID: message.ID, SessionID: sessionID, Sequence: sequence, Role: "bash",
		PayloadJSON: append([]byte(nil), message.PayloadJSON...), CreatedAt: createdAt,
	}, nil
}

// UpdateBashExecution replaces the lifecycle payload of one bash message.
func (s *Store) UpdateBashExecution(
	ctx context.Context,
	sessionID, executionID string,
	payload []byte,
) (MessageRecord, error) {
	if s == nil || s.db == nil {
		return MessageRecord{}, fmt.Errorf("store is closed")
	}
	if sessionID == "" || !identifier.Valid(executionID, "bash_") || !json.Valid(payload) {
		return MessageRecord{}, fmt.Errorf("session id, valid bash id, and payload are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MessageRecord{}, fmt.Errorf("begin bash message update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var record MessageRecord
	var createdAt, currentStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT id, session_id, sequence, role, payload_json, created_at,
		       json_extract(payload_json, '$.status')
		FROM messages
		WHERE id = ? AND session_id = ? AND role = 'bash'
	`, executionID, sessionID).Scan(
		&record.ID, &record.SessionID, &record.Sequence, &record.Role,
		&record.PayloadJSON, &createdAt, &currentStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageRecord{}, fmt.Errorf("bash execution %q: %w", executionID, ErrNotFound)
	}
	if err != nil {
		return MessageRecord{}, fmt.Errorf("read bash message %q for update: %w", executionID, err)
	}
	record.CreatedAt, err = parseTimestamp(createdAt)
	if err != nil {
		return MessageRecord{}, fmt.Errorf("parse bash execution created_at: %w", err)
	}
	if currentStatus != "running" {
		if err := tx.Commit(); err != nil {
			return MessageRecord{}, fmt.Errorf("finish idempotent bash update: %w", err)
		}
		record.PayloadJSON = append([]byte(nil), record.PayloadJSON...)
		return record, nil
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE messages
		SET payload_json = ?
		WHERE id = ? AND session_id = ? AND role = 'bash'
		  AND json_extract(payload_json, '$.status') = 'running'
	`, payload, executionID, sessionID)
	if err != nil {
		return MessageRecord{}, fmt.Errorf("update bash message %q: %w", executionID, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return MessageRecord{}, fmt.Errorf("inspect bash message update: %w", err)
	}
	if count != 1 {
		return MessageRecord{}, fmt.Errorf("bash execution %q changed during settlement", executionID)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now().UTC().Format(timestampLayout), sessionID); err != nil {
		return MessageRecord{}, fmt.Errorf("touch bash session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return MessageRecord{}, fmt.Errorf("commit bash message update: %w", err)
	}
	record.PayloadJSON = append([]byte(nil), payload...)
	return record, nil
}

// GetBashExecution returns one standalone bash message.
func (s *Store) GetBashExecution(ctx context.Context, sessionID, executionID string) (MessageRecord, error) {
	if s == nil || s.db == nil {
		return MessageRecord{}, fmt.Errorf("store is closed")
	}
	var record MessageRecord
	var createdAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, sequence, role, payload_json, created_at
		FROM messages
		WHERE id = ? AND session_id = ? AND role = 'bash'
	`, executionID, sessionID).Scan(
		&record.ID, &record.SessionID, &record.Sequence, &record.Role,
		&record.PayloadJSON, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageRecord{}, fmt.Errorf("bash execution %q: %w", executionID, ErrNotFound)
	}
	if err != nil {
		return MessageRecord{}, fmt.Errorf("get bash execution %q: %w", executionID, err)
	}
	record.CreatedAt, err = parseTimestamp(createdAt)
	if err != nil {
		return MessageRecord{}, fmt.Errorf("parse bash execution created_at: %w", err)
	}
	record.PayloadJSON = append([]byte(nil), record.PayloadJSON...)
	return record, nil
}

// ClaimBashContext atomically assigns all settled included bash observations
// to the parent turn whose first provider request will include them.
func (s *Store) ClaimBashContext(ctx context.Context, sessionID, turnID string) (int64, error) {
	if s == nil || s.db == nil {
		return -1, fmt.Errorf("store is closed")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return -1, fmt.Errorf("begin bash context claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var turnExists int
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM turns
			WHERE id = ? AND session_id = ? AND status = 'pending'
		)
	`, turnID, sessionID).Scan(&turnExists); err != nil {
		return -1, fmt.Errorf("verify bash context turn: %w", err)
	}
	if turnExists != 1 {
		return -1, fmt.Errorf("pending turn %q: %w", turnID, ErrNotFound)
	}
	var sequence int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sequence), -1)
		FROM messages
		WHERE session_id = ? AND role = 'bash'
		  AND json_extract(payload_json, '$.status') IN ('completed', 'failed', 'aborted')
		  AND COALESCE(json_extract(payload_json, '$.excludeFromContext'), 0) = 0
		  AND json_extract(payload_json, '$.contextBeforeTurnId') IS NULL
	`, sessionID).Scan(&sequence); err != nil {
		return -1, fmt.Errorf("read claimable bash context: %w", err)
	}
	if sequence >= 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE messages
			SET payload_json = json_set(payload_json, '$.contextBeforeTurnId', ?)
			WHERE session_id = ? AND role = 'bash'
			  AND json_extract(payload_json, '$.status') IN ('completed', 'failed', 'aborted')
			  AND COALESCE(json_extract(payload_json, '$.excludeFromContext'), 0) = 0
			  AND json_extract(payload_json, '$.contextBeforeTurnId') IS NULL
		`, turnID, sessionID); err != nil {
			return -1, fmt.Errorf("claim bash context: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return -1, fmt.Errorf("commit bash context claim: %w", err)
	}
	return sequence, nil
}

// InspectBashContextClaim reconciles a possibly ambiguous context-claim commit.
func (s *Store) InspectBashContextClaim(ctx context.Context, sessionID, turnID string) (int64, int64, error) {
	if s == nil || s.db == nil {
		return -1, -1, fmt.Errorf("store is closed")
	}
	var claimed, unclaimed int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(MAX(CASE
				WHEN json_extract(payload_json, '$.contextBeforeTurnId') = ? THEN sequence
			END), -1),
			COALESCE(MAX(CASE
				WHEN json_extract(payload_json, '$.contextBeforeTurnId') IS NULL THEN sequence
			END), -1)
		FROM messages
		WHERE session_id = ? AND role = 'bash'
		  AND json_extract(payload_json, '$.status') IN ('completed', 'failed', 'aborted')
		  AND COALESCE(json_extract(payload_json, '$.excludeFromContext'), 0) = 0
	`, turnID, sessionID).Scan(&claimed, &unclaimed); err != nil {
		return -1, -1, fmt.Errorf("inspect bash context claim: %w", err)
	}
	return claimed, unclaimed, nil
}

// ListMessages returns every message, including diagnostics from incomplete runs.
func (s *Store) ListMessages(ctx context.Context, sessionID string) ([]MessageRecord, error) {
	return s.listMessages(ctx, sessionID, false)
}

// ListReplayMessages returns completed parent turns plus direct bash context
// explicitly claimed by an eligible parent turn.
func (s *Store) ListReplayMessages(ctx context.Context, sessionID string) ([]MessageRecord, error) {
	return s.listMessages(ctx, sessionID, true)
}

func (s *Store) listMessages(ctx context.Context, sessionID string, replayOnly bool) ([]MessageRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is closed")
	}
	query := `
		SELECT messages.id, messages.session_id, messages.turn_id,
		       messages.sequence, messages.role, messages.payload_json,
		       messages.created_at
		FROM messages`
	if replayOnly {
		query += `
		LEFT JOIN turns ON turns.id = messages.turn_id
		               AND turns.session_id = messages.session_id
		LEFT JOIN turns AS bash_boundary
		       ON bash_boundary.id = json_extract(messages.payload_json, '$.contextBeforeTurnId')
		      AND bash_boundary.session_id = messages.session_id`
	}
	query += `
		WHERE messages.session_id = ?`
	if replayOnly {
		query += `
		  AND (
			turns.status = 'completed'
			OR messages.role = 'bash'
			   AND json_extract(messages.payload_json, '$.contextBeforeTurnId') IS NOT NULL
			   AND bash_boundary.status IN ('pending', 'running', 'completed')
		  )`
	}
	query += `
		ORDER BY messages.sequence`
	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}
	defer rows.Close()

	var records []MessageRecord
	for rows.Next() {
		var record MessageRecord
		var turnID sql.NullString
		var createdAt string
		if err := rows.Scan(
			&record.ID,
			&record.SessionID,
			&turnID,
			&record.Sequence,
			&record.Role,
			&record.PayloadJSON,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("decode session message: %w", err)
		}
		parsed, err := parseTimestamp(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse message created_at: %w", err)
		}
		record.TurnID = turnID.String
		record.CreatedAt = parsed
		record.PayloadJSON = append([]byte(nil), record.PayloadJSON...)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}
	return records, nil
}

func validMessageRole(role string) bool {
	switch role {
	case "user", "assistant", "tool":
		return true
	default:
		return false
	}
}
