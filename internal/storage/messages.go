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

// ListMessages returns every message, including diagnostics from incomplete runs.
func (s *Store) ListMessages(ctx context.Context, sessionID string) ([]MessageRecord, error) {
	return s.listMessages(ctx, sessionID, false)
}

// ListReplayMessages returns only messages from completed turns, ensuring a
// restarted droids runtime never rehydrates a partial or failed transcript.
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
		JOIN turns ON turns.id = messages.turn_id
		          AND turns.session_id = messages.session_id
		          AND turns.status = 'completed'`
	}
	query += `
		WHERE messages.session_id = ?
		ORDER BY messages.sequence`
	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}
	defer rows.Close()

	var records []MessageRecord
	for rows.Next() {
		var record MessageRecord
		var createdAt string
		if err := rows.Scan(
			&record.ID,
			&record.SessionID,
			&record.TurnID,
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
