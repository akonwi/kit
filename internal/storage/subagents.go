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

const maxSubagentMessageBytes = 128 << 10

const conversationColumns = `
	c.id, c.owner_session_id, c.agent_name, c.agent_description,
	COALESCE(c.agent_model, ''), c.agent_instructions, c.source_kind,
	c.source_path, COALESCE(c.source_plugin_id, ''), c.cwd, c.model,
	COALESCE(c.thinking_level, ''), c.droid_initialized_at, c.state, c.generation,
	COALESCE(c.active_task_id, ''), COALESCE(c.last_completed_task_id, ''),
	COALESCE(c.last_result_summary, ''), c.created_at, c.updated_at,
	c.dismissed_at,
	(SELECT COUNT(*) FROM subagent_tasks q WHERE q.conversation_id = c.id AND q.state = 'queued')`

const taskColumns = `
	t.id, t.conversation_id, t.owner_session_id, t.sequence, t.message,
	t.state, COALESCE(t.retry_of_task_id, ''), COALESCE(t.child_turn_id, ''),
	t.cancellation_generation, t.queued_at, t.started_at, t.finished_at,
	COALESCE(t.result_summary, ''), COALESCE(t.terminal_error, '')`

// Admit creates or continues the one active conversation for an agent and
// transactionally inserts a queued task before returning its identity.
func (s *Store) Admit(ctx context.Context, admission subagent.Admission, limits subagent.Limits) (subagent.Conversation, subagent.Task, error) {
	if s == nil || s.db == nil {
		return subagent.Conversation{}, subagent.Task{}, fmt.Errorf("store is closed")
	}
	if err := validateSubagentAdmission(admission, limits); err != nil {
		return subagent.Conversation{}, subagent.Task{}, err
	}
	now := admission.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	taskID, err := identifier.New("task_")
	if err != nil {
		return subagent.Conversation{}, subagent.Task{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subagent.Conversation{}, subagent.Task{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var persistent int
	if err := tx.QueryRowContext(ctx, `SELECT persistent FROM sessions WHERE id = ? AND archived_at IS NULL`, admission.OwnerSessionID).Scan(&persistent); errors.Is(err, sql.ErrNoRows) {
		return subagent.Conversation{}, subagent.Task{}, fmt.Errorf("owner session %q: %w", admission.OwnerSessionID, subagent.ErrNotFound)
	} else if err != nil {
		return subagent.Conversation{}, subagent.Task{}, err
	}
	if persistent != 1 {
		return subagent.Conversation{}, subagent.Task{}, subagent.ErrTemporaryUnavailable
	}

	var conversation subagent.Conversation
	if admission.ConversationID != "" {
		conversation, err = scanSubagentConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+`
			FROM subagent_conversations c
			WHERE c.id = ? AND c.owner_session_id = ? AND c.dismissed_at IS NULL`, admission.ConversationID, admission.OwnerSessionID))
		if errors.Is(err, sql.ErrNoRows) {
			return subagent.Conversation{}, subagent.Task{}, subagent.ErrConflict
		}
		if err != nil {
			return subagent.Conversation{}, subagent.Task{}, fmt.Errorf("load exact subagent conversation: %w", err)
		}
		if conversation.Generation != admission.ExpectedGeneration || conversation.Agent.Name != admission.Definition.Name {
			return subagent.Conversation{}, subagent.Task{}, subagent.ErrConflict
		}
	} else {
		conversation, err = scanSubagentConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+`
			FROM subagent_conversations c
			WHERE c.owner_session_id = ? AND c.agent_name = ? AND c.dismissed_at IS NULL`, admission.OwnerSessionID, admission.Definition.Name))
		if errors.Is(err, sql.ErrNoRows) {
			conversationID, idErr := identifier.New("subagent_")
			if idErr != nil {
				return subagent.Conversation{}, subagent.Task{}, idErr
			}
			conversation = subagent.Conversation{
				ID: subagent.ConversationID(conversationID), OwnerSessionID: admission.OwnerSessionID,
				Agent: admission.Definition, CWD: admission.CWD, Model: admission.Model,
				ThinkingLevel: admission.ThinkingLevel, State: subagent.ConversationRunning,
				Generation: 1, CreatedAt: now, UpdatedAt: now,
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO subagent_conversations(
					id, owner_session_id, agent_name, agent_description, agent_model,
					agent_instructions, source_kind, source_path, source_plugin_id,
					cwd, model, thinking_level, state, generation, created_at, updated_at
				) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?, 1, ?, ?)`,
				conversation.ID, conversation.OwnerSessionID, conversation.Agent.Name,
				conversation.Agent.Description, conversation.Agent.Model, conversation.Agent.Instructions,
				conversation.Agent.Source.Kind, conversation.Agent.Source.Path, conversation.Agent.Source.PluginID,
				conversation.CWD, conversation.Model, conversation.ThinkingLevel, conversation.State,
				formatTimestamp(now), formatTimestamp(now)); err != nil {
				return subagent.Conversation{}, subagent.Task{}, fmt.Errorf("create subagent conversation: %w", err)
			}
		} else if err != nil {
			return subagent.Conversation{}, subagent.Task{}, fmt.Errorf("load subagent conversation: %w", err)
		}
	}

	if err := checkQueueBounds(ctx, tx, conversation.ID, admission.OwnerSessionID, limits); err != nil {
		return subagent.Conversation{}, subagent.Task{}, err
	}
	if admission.RetryOf != "" {
		var retryConversation string
		if err := tx.QueryRowContext(ctx, `SELECT conversation_id FROM subagent_tasks WHERE id = ?`, admission.RetryOf).Scan(&retryConversation); errors.Is(err, sql.ErrNoRows) {
			return subagent.Conversation{}, subagent.Task{}, fmt.Errorf("retry task %q: %w", admission.RetryOf, subagent.ErrNotFound)
		} else if err != nil {
			return subagent.Conversation{}, subagent.Task{}, err
		} else if retryConversation != string(conversation.ID) {
			return subagent.Conversation{}, subagent.Task{}, fmt.Errorf("%w: retry task belongs to another conversation", subagent.ErrInvalidInput)
		}
	}
	var sequence uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM subagent_tasks WHERE conversation_id = ?`, conversation.ID).Scan(&sequence); err != nil {
		return subagent.Conversation{}, subagent.Task{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO subagent_tasks(
			id, conversation_id, owner_session_id, sequence, message, state,
			retry_of_task_id, cancellation_generation, queued_at
		) VALUES (?, ?, ?, ?, ?, 'queued', NULLIF(?, ''), 1, ?)`,
		taskID, conversation.ID, admission.OwnerSessionID, sequence, admission.Message,
		admission.RetryOf, formatTimestamp(now)); err != nil {
		return subagent.Conversation{}, subagent.Task{}, fmt.Errorf("queue subagent task: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE subagent_conversations SET state = 'running', updated_at = ?
		WHERE id = ? AND dismissed_at IS NULL`, formatTimestamp(now), conversation.ID); err != nil {
		return subagent.Conversation{}, subagent.Task{}, err
	}
	if err := insertSubagentEvent(ctx, tx, admission.OwnerSessionID, conversation.ID, subagent.TaskID(taskID), "task.queued", now); err != nil {
		return subagent.Conversation{}, subagent.Task{}, err
	}
	conversation, err = loadSubagentConversation(ctx, tx, conversation.ID, false)
	if err != nil {
		return subagent.Conversation{}, subagent.Task{}, err
	}
	task, err := loadSubagentTask(ctx, tx, subagent.TaskID(taskID))
	if err != nil {
		return subagent.Conversation{}, subagent.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return subagent.Conversation{}, subagent.Task{}, fmt.Errorf("commit subagent admission: %w", err)
	}
	return conversation, task, nil
}

func validateSubagentAdmission(admission subagent.Admission, limits subagent.Limits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	if !identifier.Valid(admission.OwnerSessionID, "session_") {
		return fmt.Errorf("%w: invalid owner session id", subagent.ErrInvalidInput)
	}
	if (admission.ConversationID == "") != (admission.ExpectedGeneration == 0) ||
		(admission.ConversationID != "" && !identifier.Valid(string(admission.ConversationID), "subagent_")) {
		return fmt.Errorf("%w: exact conversation identity and generation must be supplied together", subagent.ErrInvalidInput)
	}
	if _, err := subagent.NewCatalog(admission.Definition); err != nil {
		return fmt.Errorf("%w: %v", subagent.ErrInvalidInput, err)
	}
	if strings.TrimSpace(admission.CWD) == "" || strings.TrimSpace(admission.Model) == "" {
		return fmt.Errorf("%w: cwd and resolved model are required", subagent.ErrInvalidInput)
	}
	if strings.TrimSpace(admission.Message) == "" || len(admission.Message) > maxSubagentMessageBytes || !utf8.ValidString(admission.Message) || strings.ContainsRune(admission.Message, 0) {
		return fmt.Errorf("%w: task message is empty, invalid, or oversized", subagent.ErrInvalidInput)
	}
	return nil
}

func checkQueueBounds(ctx context.Context, tx *sql.Tx, conversationID subagent.ConversationID, ownerSessionID string, limits subagent.Limits) error {
	queries := []struct {
		name  string
		query string
		arg   any
		limit int
	}{
		{"conversation", `SELECT COUNT(*) FROM subagent_tasks WHERE conversation_id = ? AND state = 'queued'`, conversationID, limits.PerConversationQueue},
		{"session", `SELECT COUNT(*) FROM subagent_tasks WHERE owner_session_id = ? AND state = 'queued'`, ownerSessionID, limits.PerSessionQueue},
		{"global", `SELECT COUNT(*) FROM subagent_tasks WHERE state = 'queued'`, nil, limits.GlobalQueue},
	}
	for _, check := range queries {
		var count int
		var err error
		if check.arg == nil {
			err = tx.QueryRowContext(ctx, check.query).Scan(&count)
		} else {
			err = tx.QueryRowContext(ctx, check.query, check.arg).Scan(&count)
		}
		if err != nil {
			return err
		}
		if count >= check.limit {
			return fmt.Errorf("%w: %s queue capacity reached", subagent.ErrQueueFull, check.name)
		}
	}
	return nil
}

// QueuedSessions returns owners with eligible queued work ordered by oldest task.
func (s *Store) QueuedSessions(ctx context.Context) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is closed")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.owner_session_id, MIN(t.queued_at) AS oldest
		FROM subagent_tasks t
		JOIN subagent_conversations c ON c.id = t.conversation_id
		JOIN sessions s ON s.id = t.owner_session_id
		WHERE t.state = 'queued' AND c.dismissed_at IS NULL AND s.archived_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM subagent_tasks active
			WHERE active.conversation_id = t.conversation_id AND active.state = 'running'
		  )
		GROUP BY t.owner_session_id
		ORDER BY oldest, t.owner_session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var owners []string
	for rows.Next() {
		var owner, ignored string
		if err := rows.Scan(&owner, &ignored); err != nil {
			return nil, err
		}
		owners = append(owners, owner)
	}
	return owners, rows.Err()
}

// ClaimNext transactionally claims the oldest eligible task for one owner.
func (s *Store) ClaimNext(ctx context.Context, ownerSessionID string, limits subagent.Limits, startedAt time.Time) (subagent.Claim, error) {
	if s == nil || s.db == nil {
		return subagent.Claim{}, fmt.Errorf("store is closed")
	}
	if err := limits.Validate(); err != nil {
		return subagent.Claim{}, err
	}
	startedAt = startedAt.UTC()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subagent.Claim{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var globalRunning, sessionRunning int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subagent_tasks WHERE state = 'running'`).Scan(&globalRunning); err != nil {
		return subagent.Claim{}, err
	}
	if globalRunning >= limits.GlobalRunning {
		return subagent.Claim{}, subagent.ErrNotFound
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subagent_tasks WHERE owner_session_id = ? AND state = 'running'`, ownerSessionID).Scan(&sessionRunning); err != nil {
		return subagent.Claim{}, err
	}
	if sessionRunning >= limits.PerSessionRunning {
		return subagent.Claim{}, subagent.ErrNotFound
	}
	var taskID string
	if err := tx.QueryRowContext(ctx, `
		SELECT t.id
		FROM subagent_tasks t
		JOIN subagent_conversations c ON c.id = t.conversation_id
		JOIN sessions s ON s.id = t.owner_session_id
		WHERE t.owner_session_id = ? AND t.state = 'queued'
		  AND c.dismissed_at IS NULL AND s.archived_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM subagent_tasks active
			WHERE active.conversation_id = t.conversation_id AND active.state = 'running'
		  )
		ORDER BY t.queued_at, t.sequence, t.id
		LIMIT 1`, ownerSessionID).Scan(&taskID); errors.Is(err, sql.ErrNoRows) {
		return subagent.Claim{}, subagent.ErrNotFound
	} else if err != nil {
		return subagent.Claim{}, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE subagent_tasks SET state = 'running', started_at = ?
		WHERE id = ? AND state = 'queued'`, formatTimestamp(startedAt), taskID)
	if err != nil {
		return subagent.Claim{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return subagent.Claim{}, subagent.ErrConflict
	}
	task, err := loadSubagentTask(ctx, tx, subagent.TaskID(taskID))
	if err != nil {
		return subagent.Claim{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE subagent_conversations
		SET state = 'running', active_task_id = ?, updated_at = ?
		WHERE id = ? AND dismissed_at IS NULL`, task.ID, formatTimestamp(startedAt), task.ConversationID); err != nil {
		return subagent.Claim{}, err
	}
	if err := insertSubagentEvent(ctx, tx, task.OwnerSessionID, task.ConversationID, task.ID, "task.running", startedAt); err != nil {
		return subagent.Claim{}, err
	}
	conversation, err := loadSubagentConversation(ctx, tx, task.ConversationID, false)
	if err != nil {
		return subagent.Claim{}, err
	}
	if err := tx.Commit(); err != nil {
		return subagent.Claim{}, err
	}
	return subagent.Claim{Conversation: conversation, Task: task}, nil
}

// MarkConversationInitialized records the one-time child store handshake.
func (s *Store) MarkConversationInitialized(ctx context.Context, conversationID subagent.ConversationID, initializedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE subagent_conversations
		SET droid_initialized_at = COALESCE(droid_initialized_at, ?)
		WHERE id = ? AND dismissed_at IS NULL`, formatTimestamp(initializedAt), conversationID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return subagent.ErrNotFound
	}
	return nil
}

// BindChildTurn records the droid-owned turn identity once admission succeeds.
func (s *Store) BindChildTurn(ctx context.Context, taskID subagent.TaskID, generation uint64, childTurnID string) (subagent.Task, error) {
	if childTurnID == "" {
		return subagent.Task{}, fmt.Errorf("%w: child turn id is required", subagent.ErrInvalidInput)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE subagent_tasks SET child_turn_id = ?
		WHERE id = ? AND state = 'running' AND cancellation_generation = ?
		  AND (child_turn_id IS NULL OR child_turn_id = ?)`, childTurnID, taskID, generation, childTurnID)
	if err != nil {
		return subagent.Task{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return subagent.Task{}, subagent.ErrConflict
	}
	return loadSubagentTask(ctx, s.db, taskID)
}

// Complete atomically settles a running task and inserts its mailbox item.
func (s *Store) Complete(ctx context.Context, completion subagent.Completion) (subagent.Task, *subagent.MailboxItem, error) {
	if !subagent.TerminalTask(completion.State) {
		return subagent.Task{}, nil, fmt.Errorf("%w: completion state is not terminal", subagent.ErrInvalidInput)
	}
	finishedAt := completion.FinishedAt.UTC()
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subagent.Task{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	task, err := loadSubagentTask(ctx, tx, completion.TaskID)
	if err != nil {
		return subagent.Task{}, nil, err
	}
	if subagent.TerminalTask(task.State) {
		if task.State != completion.State {
			return subagent.Task{}, nil, subagent.ErrConflict
		}
		mailbox, mailboxErr := loadMailboxByTask(ctx, tx, task.ID)
		if errors.Is(mailboxErr, sql.ErrNoRows) {
			return task, nil, nil
		}
		return task, &mailbox, mailboxErr
	}
	if task.State != subagent.TaskRunning || task.ConversationID != completion.ConversationID || task.CancellationGeneration != completion.CancellationGeneration {
		return subagent.Task{}, nil, subagent.ErrConflict
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE subagent_tasks
		SET state = ?, child_turn_id = COALESCE(NULLIF(?, ''), child_turn_id),
		    finished_at = ?, result_summary = NULLIF(?, ''), terminal_error = NULLIF(?, '')
		WHERE id = ? AND state = 'running' AND cancellation_generation = ?`,
		completion.State, completion.ChildTurnID, formatTimestamp(finishedAt),
		boundedSubagentSummary(completion.ResultSummary), boundedSubagentSummary(completion.Error),
		completion.TaskID, completion.CancellationGeneration)
	if err != nil {
		return subagent.Task{}, nil, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return subagent.Task{}, nil, subagent.ErrConflict
	}
	var queued int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subagent_tasks WHERE conversation_id = ? AND state = 'queued'`, task.ConversationID).Scan(&queued); err != nil {
		return subagent.Task{}, nil, err
	}
	conversationState := subagent.ConversationStateForTask(completion.State)
	if queued > 0 {
		conversationState = subagent.ConversationRunning
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE subagent_conversations
		SET state = ?, active_task_id = NULL, last_completed_task_id = ?,
		    last_result_summary = NULLIF(?, ''), updated_at = ?
		WHERE id = ? AND dismissed_at IS NULL`, conversationState, task.ID,
		boundedSubagentSummary(completion.ResultSummary), formatTimestamp(finishedAt), task.ConversationID); err != nil {
		return subagent.Task{}, nil, err
	}
	mailbox, err := insertMailbox(ctx, tx, task, completion.State, completion.ResultSummary, completion.Error, finishedAt)
	if err != nil {
		return subagent.Task{}, nil, err
	}
	if err := insertSubagentEvent(ctx, tx, task.OwnerSessionID, task.ConversationID, task.ID, "task."+string(completion.State), finishedAt); err != nil {
		return subagent.Task{}, nil, err
	}
	task, err = loadSubagentTask(ctx, tx, task.ID)
	if err != nil {
		return subagent.Task{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return subagent.Task{}, nil, err
	}
	return task, &mailbox, nil
}

// Cancel aborts queued work immediately or generation-safely requests running cancellation.
func (s *Store) Cancel(ctx context.Context, taskID subagent.TaskID, expectedGeneration uint64, reason string, canceledAt time.Time) (subagent.Task, error) {
	canceledAt = canceledAt.UTC()
	if canceledAt.IsZero() {
		canceledAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subagent.Task{}, err
	}
	defer func() { _ = tx.Rollback() }()
	task, err := loadSubagentTask(ctx, tx, taskID)
	if err != nil {
		return subagent.Task{}, err
	}
	if task.CancellationGeneration != expectedGeneration {
		return subagent.Task{}, subagent.ErrConflict
	}
	switch task.State {
	case subagent.TaskQueued:
		_, err = tx.ExecContext(ctx, `
			UPDATE subagent_tasks
			SET state = 'aborted', cancellation_generation = cancellation_generation + 1,
			    cancellation_requested_at = ?, cancellation_reason = NULLIF(?, ''),
			    finished_at = ?, terminal_error = NULLIF(?, '')
			WHERE id = ? AND state = 'queued' AND cancellation_generation = ?`,
			formatTimestamp(canceledAt), boundedSubagentSummary(reason), formatTimestamp(canceledAt),
			boundedSubagentSummary(reason), taskID, expectedGeneration)
		if err == nil {
			err = updateConversationAfterQueuedCancel(ctx, tx, task.ConversationID, canceledAt)
		}
	case subagent.TaskRunning:
		_, err = tx.ExecContext(ctx, `
			UPDATE subagent_tasks
			SET cancellation_generation = cancellation_generation + 1,
			    cancellation_requested_at = ?, cancellation_reason = NULLIF(?, '')
			WHERE id = ? AND state = 'running' AND cancellation_generation = ?`,
			formatTimestamp(canceledAt), boundedSubagentSummary(reason), taskID, expectedGeneration)
	default:
		return subagent.Task{}, subagent.ErrNotCancelable
	}
	if err != nil {
		return subagent.Task{}, err
	}
	if err := insertSubagentEvent(ctx, tx, task.OwnerSessionID, task.ConversationID, task.ID, "task.cancel_requested", canceledAt); err != nil {
		return subagent.Task{}, err
	}
	task, err = loadSubagentTask(ctx, tx, taskID)
	if err != nil {
		return subagent.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return subagent.Task{}, err
	}
	return task, nil
}

func updateConversationAfterQueuedCancel(ctx context.Context, tx *sql.Tx, conversationID subagent.ConversationID, at time.Time) error {
	var active, queued int
	if err := tx.QueryRowContext(ctx, `SELECT
		COUNT(*) FILTER (WHERE state = 'running'), COUNT(*) FILTER (WHERE state = 'queued')
		FROM subagent_tasks WHERE conversation_id = ?`, conversationID).Scan(&active, &queued); err != nil {
		return err
	}
	state := subagent.ConversationAborted
	if active > 0 || queued > 0 {
		state = subagent.ConversationRunning
	}
	_, err := tx.ExecContext(ctx, `UPDATE subagent_conversations SET state = ?, updated_at = ? WHERE id = ? AND dismissed_at IS NULL`, state, formatTimestamp(at), conversationID)
	return err
}

// Dismiss durably tombstones a conversation and aborts all queued/running tasks.
func (s *Store) Dismiss(ctx context.Context, conversationID subagent.ConversationID, expectedGeneration uint64, reason string, dismissedAt time.Time) ([]subagent.Task, error) {
	dismissedAt = dismissedAt.UTC()
	if dismissedAt.IsZero() {
		dismissedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	conversation, err := loadSubagentConversation(ctx, tx, conversationID, true)
	if err != nil {
		return nil, err
	}
	if conversation.DismissedAt != nil {
		return nil, nil
	}
	if conversation.Generation != expectedGeneration {
		return nil, subagent.ErrConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+taskColumns+` FROM subagent_tasks t
		WHERE t.conversation_id = ? AND t.state IN ('queued', 'running') ORDER BY t.sequence`, conversationID)
	if err != nil {
		return nil, err
	}
	var before []subagent.Task
	for rows.Next() {
		task, scanErr := scanSubagentTask(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		before = append(before, task)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE subagent_conversations
		SET dismissed_at = ?, updated_at = ?, state = 'aborted', active_task_id = NULL,
		    generation = generation + 1
		WHERE id = ? AND dismissed_at IS NULL AND generation = ?`,
		formatTimestamp(dismissedAt), formatTimestamp(dismissedAt), conversationID, expectedGeneration); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE subagent_tasks
		SET state = 'aborted', cancellation_generation = cancellation_generation + 1,
		    cancellation_requested_at = ?, cancellation_reason = NULLIF(?, ''),
		    finished_at = ?, terminal_error = NULLIF(?, '')
		WHERE conversation_id = ? AND state IN ('queued', 'running')`,
		formatTimestamp(dismissedAt), boundedSubagentSummary(reason), formatTimestamp(dismissedAt),
		boundedSubagentSummary(reason), conversationID); err != nil {
		return nil, err
	}
	changed := make([]subagent.Task, 0, len(before))
	for _, previous := range before {
		task, err := loadSubagentTask(ctx, tx, previous.ID)
		if err != nil {
			return nil, err
		}
		changed = append(changed, task)
		if previous.State == subagent.TaskRunning {
			if _, err := insertMailbox(ctx, tx, task, subagent.TaskAborted, "", reason, dismissedAt); err != nil {
				return nil, err
			}
		}
		if err := insertSubagentEvent(ctx, tx, task.OwnerSessionID, conversationID, task.ID, "task.aborted", dismissedAt); err != nil {
			return nil, err
		}
	}
	if err := insertSubagentEvent(ctx, tx, conversation.OwnerSessionID, conversationID, "", "conversation.dismissed", dismissedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return changed, nil
}

// Conversation loads one conversation including a dismissal tombstone.
func (s *Store) Conversation(ctx context.Context, id subagent.ConversationID) (subagent.Conversation, error) {
	return loadSubagentConversation(ctx, s.db, id, true)
}

// ConversationByAgent loads the one non-dismissed conversation for an owner and agent.
func (s *Store) ConversationByAgent(ctx context.Context, ownerSessionID, agentName string) (subagent.Conversation, error) {
	conversation, err := scanSubagentConversation(s.db.QueryRowContext(ctx, `SELECT `+conversationColumns+`
		FROM subagent_conversations c
		WHERE c.owner_session_id = ? AND c.agent_name = ? AND c.dismissed_at IS NULL`, ownerSessionID, agentName))
	return conversation, mapSubagentNotFound(err)
}

// Task loads one task by exact identity.
func (s *Store) Task(ctx context.Context, id subagent.TaskID) (subagent.Task, error) {
	return loadSubagentTask(ctx, s.db, id)
}

// ListConversations returns an owner's non-dismissed roster ordered by activity.
func (s *Store) ListConversations(ctx context.Context, ownerSessionID string) ([]subagent.Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+conversationColumns+`
		FROM subagent_conversations c
		WHERE c.owner_session_id = ? AND c.dismissed_at IS NULL
		ORDER BY c.updated_at DESC, c.id`, ownerSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var conversations []subagent.Conversation
	for rows.Next() {
		conversation, err := scanSubagentConversation(rows)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

// ListDismissedConversations returns owner tombstones for asynchronous artifact cleanup.
func (s *Store) ListDismissedConversations(ctx context.Context, ownerSessionID string) ([]subagent.Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+conversationColumns+`
		FROM subagent_conversations c
		WHERE c.owner_session_id = ? AND c.dismissed_at IS NOT NULL
		ORDER BY c.updated_at, c.id`, ownerSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var conversations []subagent.Conversation
	for rows.Next() {
		conversation, err := scanSubagentConversation(rows)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

// AllDismissedConversations returns every tombstone for startup artifact cleanup.
func (s *Store) AllDismissedConversations(ctx context.Context) ([]subagent.Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+conversationColumns+`
		FROM subagent_conversations c WHERE c.dismissed_at IS NOT NULL ORDER BY c.updated_at, c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var conversations []subagent.Conversation
	for rows.Next() {
		conversation, err := scanSubagentConversation(rows)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

// ListTasks returns complete task history in conversation sequence order.
func (s *Store) ListTasks(ctx context.Context, conversationID subagent.ConversationID) ([]subagent.Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns+` FROM subagent_tasks t WHERE t.conversation_id = ? ORDER BY t.sequence`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []subagent.Task
	for rows.Next() {
		task, err := scanSubagentTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// RecoverRunning interrupts work left running by a previous daemon while preserving queued tasks.
func (s *Store) RecoverRunning(ctx context.Context, recoveredAt time.Time) (subagent.Recovery, error) {
	recoveredAt = recoveredAt.UTC()
	if recoveredAt.IsZero() {
		recoveredAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subagent.Recovery{}, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT `+taskColumns+` FROM subagent_tasks t WHERE t.state = 'running' ORDER BY t.started_at, t.id`)
	if err != nil {
		return subagent.Recovery{}, err
	}
	var running []subagent.Task
	for rows.Next() {
		task, scanErr := scanSubagentTask(rows)
		if scanErr != nil {
			_ = rows.Close()
			return subagent.Recovery{}, scanErr
		}
		running = append(running, task)
	}
	if err := rows.Close(); err != nil {
		return subagent.Recovery{}, err
	}
	for _, previous := range running {
		if _, err := tx.ExecContext(ctx, `
			UPDATE subagent_tasks
			SET state = 'interrupted', cancellation_generation = cancellation_generation + 1,
			    finished_at = ?, terminal_error = 'daemon restarted while task was running'
			WHERE id = ? AND state = 'running'`, formatTimestamp(recoveredAt), previous.ID); err != nil {
			return subagent.Recovery{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE subagent_conversations
			SET state = 'interrupted', active_task_id = NULL, last_completed_task_id = ?,
			    last_result_summary = NULL, updated_at = ?
			WHERE id = ? AND dismissed_at IS NULL`, previous.ID, formatTimestamp(recoveredAt), previous.ConversationID); err != nil {
			return subagent.Recovery{}, err
		}
		task, err := loadSubagentTask(ctx, tx, previous.ID)
		if err != nil {
			return subagent.Recovery{}, err
		}
		if _, err := insertMailbox(ctx, tx, task, subagent.TaskInterrupted, "", task.Error, recoveredAt); err != nil {
			return subagent.Recovery{}, err
		}
		if err := insertSubagentEvent(ctx, tx, task.OwnerSessionID, task.ConversationID, task.ID, "task.interrupted", recoveredAt); err != nil {
			return subagent.Recovery{}, err
		}
	}
	var queued int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subagent_tasks WHERE state = 'queued'`).Scan(&queued); err != nil {
		return subagent.Recovery{}, err
	}
	if err := tx.Commit(); err != nil {
		return subagent.Recovery{}, err
	}
	interrupted := make([]subagent.Task, 0, len(running))
	for _, task := range running {
		task.State = subagent.TaskInterrupted
		task.CancellationGeneration++
		task.Error = "daemon restarted while task was running"
		finished := recoveredAt
		task.FinishedAt = &finished
		interrupted = append(interrupted, task)
	}
	return subagent.Recovery{InterruptedTasks: interrupted, QueuedTasks: queued}, nil
}

// PendingMailbox returns a bounded deterministic batch for safe-boundary delivery.
func (s *Store) PendingMailbox(ctx context.Context, ownerSessionID string, limit int) ([]subagent.MailboxItem, error) {
	if limit < 1 || limit > 256 {
		return nil, fmt.Errorf("mailbox limit must be 1-256")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_session_id, conversation_id, task_id, agent_name,
		       task_state, COALESCE(summary, ''), COALESCE(terminal_error, ''),
		       created_at, delivered_at, generation
		FROM parent_mailbox
		WHERE owner_session_id = ? AND delivered_at IS NULL
		ORDER BY created_at, id LIMIT ?`, ownerSessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []subagent.MailboxItem
	for rows.Next() {
		item, err := scanMailbox(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// PendingMailboxOwners returns owners with undelivered results in oldest-first order.
func (s *Store) PendingMailboxOwners(ctx context.Context, afterOwner string, limit int) ([]string, error) {
	if limit < 1 || limit > 256 {
		return nil, fmt.Errorf("mailbox owner limit must be 1-256")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.owner_session_id
		FROM parent_mailbox p
		JOIN sessions s ON s.id = p.owner_session_id
		WHERE p.delivered_at IS NULL AND s.archived_at IS NULL
		  AND p.owner_session_id > ?
		GROUP BY p.owner_session_id
		ORDER BY p.owner_session_id
		LIMIT ?`, afterOwner, limit)
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

// MarkMailboxDelivered generation-safely marks a complete batch delivered.
func (s *Store) MarkMailboxDelivered(ctx context.Context, ids []string, expectedGeneration uint64, deliveredAt time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	deliveredAt = deliveredAt.UTC()
	if deliveredAt.IsZero() {
		deliveredAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		result, err := tx.ExecContext(ctx, `
			UPDATE parent_mailbox SET delivered_at = ?
			WHERE id = ? AND generation = ? AND delivered_at IS NULL`, formatTimestamp(deliveredAt), id, expectedGeneration)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			var existing sql.NullString
			var generation uint64
			if err := tx.QueryRowContext(ctx, `SELECT delivered_at, generation FROM parent_mailbox WHERE id = ?`, id).Scan(&existing, &generation); err != nil {
				return mapSubagentNotFound(err)
			}
			if !existing.Valid || generation != expectedGeneration {
				return subagent.ErrConflict
			}
		}
	}
	return tx.Commit()
}

func insertMailbox(ctx context.Context, tx *sql.Tx, task subagent.Task, state subagent.TaskState, summary, terminalError string, at time.Time) (subagent.MailboxItem, error) {
	conversation, err := loadSubagentConversation(ctx, tx, task.ConversationID, true)
	if err != nil {
		return subagent.MailboxItem{}, err
	}
	id, err := identifier.New("mail_")
	if err != nil {
		return subagent.MailboxItem{}, err
	}
	item := subagent.MailboxItem{
		ID: id, OwnerSessionID: task.OwnerSessionID, ConversationID: task.ConversationID,
		TaskID: task.ID, AgentName: conversation.Agent.Name, State: state,
		Summary: boundedSubagentSummary(summary), Error: boundedSubagentSummary(terminalError),
		CreatedAt: at, Generation: 1,
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO parent_mailbox(
			id, owner_session_id, conversation_id, task_id, agent_name, task_state,
			summary, terminal_error, generation, created_at
		) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), 1, ?)
		ON CONFLICT(task_id) DO NOTHING`, item.ID, item.OwnerSessionID, item.ConversationID,
		item.TaskID, item.AgentName, item.State, item.Summary, item.Error, formatTimestamp(at))
	if err != nil {
		return subagent.MailboxItem{}, err
	}
	return loadMailboxByTask(ctx, tx, task.ID)
}

func loadMailboxByTask(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, taskID subagent.TaskID) (subagent.MailboxItem, error) {
	return scanMailbox(queryer.QueryRowContext(ctx, `
		SELECT id, owner_session_id, conversation_id, task_id, agent_name,
		       task_state, COALESCE(summary, ''), COALESCE(terminal_error, ''),
		       created_at, delivered_at, generation
		FROM parent_mailbox WHERE task_id = ?`, taskID))
}

func scanMailbox(scanner rowScanner) (subagent.MailboxItem, error) {
	var item subagent.MailboxItem
	var created string
	var delivered sql.NullString
	if err := scanner.Scan(&item.ID, &item.OwnerSessionID, &item.ConversationID, &item.TaskID,
		&item.AgentName, &item.State, &item.Summary, &item.Error, &created, &delivered, &item.Generation); err != nil {
		return subagent.MailboxItem{}, err
	}
	var err error
	item.CreatedAt, err = parseTimestamp(created)
	if err != nil {
		return subagent.MailboxItem{}, err
	}
	if delivered.Valid {
		value, err := parseTimestamp(delivered.String)
		if err != nil {
			return subagent.MailboxItem{}, err
		}
		item.DeliveredAt = &value
	}
	return item, nil
}

func insertSubagentEvent(ctx context.Context, tx *sql.Tx, owner string, conversationID subagent.ConversationID, taskID subagent.TaskID, kind string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO subagent_events(owner_session_id, conversation_id, task_id, kind, occurred_at)
		VALUES (?, ?, NULLIF(?, ''), ?, ?)`, owner, conversationID, taskID, kind, formatTimestamp(at))
	return err
}

func loadSubagentConversation(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id subagent.ConversationID, includeDismissed bool) (subagent.Conversation, error) {
	query := `SELECT ` + conversationColumns + ` FROM subagent_conversations c WHERE c.id = ?`
	if !includeDismissed {
		query += ` AND c.dismissed_at IS NULL`
	}
	conversation, err := scanSubagentConversation(queryer.QueryRowContext(ctx, query, id))
	return conversation, mapSubagentNotFound(err)
}

func scanSubagentConversation(scanner rowScanner) (subagent.Conversation, error) {
	var conversation subagent.Conversation
	var sourceKind string
	var created, updated string
	var initialized, dismissed sql.NullString
	if err := scanner.Scan(
		&conversation.ID, &conversation.OwnerSessionID, &conversation.Agent.Name,
		&conversation.Agent.Description, &conversation.Agent.Model, &conversation.Agent.Instructions,
		&sourceKind, &conversation.Agent.Source.Path, &conversation.Agent.Source.PluginID,
		&conversation.CWD, &conversation.Model, &conversation.ThinkingLevel, &initialized, &conversation.State,
		&conversation.Generation, &conversation.ActiveTaskID, &conversation.LastCompletedTaskID,
		&conversation.LastResultSummary, &created, &updated, &dismissed, &conversation.QueuedTasks,
	); err != nil {
		return subagent.Conversation{}, err
	}
	conversation.Agent.Source.Kind = subagent.SourceKind(sourceKind)
	var err error
	conversation.CreatedAt, err = parseTimestamp(created)
	if err != nil {
		return subagent.Conversation{}, err
	}
	conversation.UpdatedAt, err = parseTimestamp(updated)
	if err != nil {
		return subagent.Conversation{}, err
	}
	if initialized.Valid {
		value, err := parseTimestamp(initialized.String)
		if err != nil {
			return subagent.Conversation{}, err
		}
		conversation.DroidInitializedAt = &value
	}
	if dismissed.Valid {
		value, err := parseTimestamp(dismissed.String)
		if err != nil {
			return subagent.Conversation{}, err
		}
		conversation.DismissedAt = &value
	}
	return conversation, nil
}

func loadSubagentTask(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id subagent.TaskID) (subagent.Task, error) {
	task, err := scanSubagentTask(queryer.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM subagent_tasks t WHERE t.id = ?`, id))
	return task, mapSubagentNotFound(err)
}

func scanSubagentTask(scanner rowScanner) (subagent.Task, error) {
	var task subagent.Task
	var queued string
	var started, finished sql.NullString
	if err := scanner.Scan(&task.ID, &task.ConversationID, &task.OwnerSessionID, &task.Sequence,
		&task.Message, &task.State, &task.RetryOf, &task.ChildTurnID,
		&task.CancellationGeneration, &queued, &started, &finished,
		&task.ResultSummary, &task.Error); err != nil {
		return subagent.Task{}, err
	}
	var err error
	task.QueuedAt, err = parseTimestamp(queued)
	if err != nil {
		return subagent.Task{}, err
	}
	if started.Valid {
		value, err := parseTimestamp(started.String)
		if err != nil {
			return subagent.Task{}, err
		}
		task.StartedAt = &value
	}
	if finished.Valid {
		value, err := parseTimestamp(finished.String)
		if err != nil {
			return subagent.Task{}, err
		}
		task.FinishedAt = &value
	}
	return task, nil
}

func mapSubagentNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return subagent.ErrNotFound
	}
	return err
}

func boundedSubagentSummary(value string) string {
	value = strings.ReplaceAll(strings.ToValidUTF8(value, "�"), "\x00", "�")
	const limit = 16 << 10
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func archiveOwnedSubagents(ctx context.Context, tx *sql.Tx, ownerSessionID string, archivedAt time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+taskColumns+` FROM subagent_tasks t
		WHERE t.owner_session_id = ? AND t.state = 'running' ORDER BY t.id`, ownerSessionID)
	if err != nil {
		return err
	}
	var running []subagent.Task
	for rows.Next() {
		task, scanErr := scanSubagentTask(rows)
		if scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		running = append(running, task)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	const reason = "owner session was archived"
	if _, err := tx.ExecContext(ctx, `
		UPDATE subagent_tasks
		SET state = 'aborted', cancellation_generation = cancellation_generation + 1,
		    cancellation_requested_at = ?, cancellation_reason = ?, finished_at = ?, terminal_error = ?
		WHERE owner_session_id = ? AND state IN ('queued', 'running')`,
		formatTimestamp(archivedAt), reason, formatTimestamp(archivedAt), reason, ownerSessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE subagent_conversations
		SET dismissed_at = COALESCE(dismissed_at, ?), updated_at = ?, state = 'aborted',
		    active_task_id = NULL, generation = generation + CASE WHEN dismissed_at IS NULL THEN 1 ELSE 0 END
		WHERE owner_session_id = ?`, formatTimestamp(archivedAt), formatTimestamp(archivedAt), ownerSessionID); err != nil {
		return err
	}
	for _, previous := range running {
		task, err := loadSubagentTask(ctx, tx, previous.ID)
		if err != nil {
			return err
		}
		if _, err := insertMailbox(ctx, tx, task, subagent.TaskAborted, "", reason, archivedAt); err != nil {
			return err
		}
		if err := insertSubagentEvent(ctx, tx, ownerSessionID, task.ConversationID, task.ID, "task.aborted", archivedAt); err != nil {
			return err
		}
	}
	return nil
}

// SubagentOwner projects authoritative parent configuration for task admission.
func (s *Store) SubagentOwner(ctx context.Context, sessionID string) (subagent.Owner, error) {
	var owner subagent.Owner
	var provider, model, thinking sql.NullString
	var persistent int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, cwd, model_provider, model_id, thinking_level, persistent
		FROM sessions WHERE id = ? AND archived_at IS NULL`, sessionID).Scan(
		&owner.SessionID, &owner.CWD, &provider, &model, &thinking, &persistent,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return subagent.Owner{}, subagent.ErrNotFound
	}
	if err != nil {
		return subagent.Owner{}, err
	}
	owner.Model = provider.String + "/" + model.String
	owner.ThinkingLevel = thinking.String
	owner.Persistent = persistent == 1
	return owner, nil
}

var _ subagent.OwnerResolver = (*Store)(nil)
