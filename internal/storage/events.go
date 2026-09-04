package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/akonwi/kit/internal/identifier"
	kitsession "github.com/akonwi/kit/internal/session"
)

const (
	retainedSessionEvents     = 4096
	retainedSessionEventBytes = 16 << 20
)

type sessionEventPayload struct {
	TurnID             string                         `json:"turnId"`
	RunID              string                         `json:"runId"`
	MessageID          string                         `json:"messageId,omitempty"`
	ContentIndex       int                            `json:"contentIndex,omitempty"`
	Delta              string                         `json:"delta,omitempty"`
	Text               string                         `json:"text,omitempty"`
	Thinking           string                         `json:"thinking,omitempty"`
	ToolCallID         string                         `json:"toolCallId,omitempty"`
	ToolName           string                         `json:"toolName,omitempty"`
	Arguments          string                         `json:"arguments,omitempty"`
	ArgumentsTruncated bool                           `json:"argumentsTruncated,omitempty"`
	Content            []kitsession.TranscriptContent `json:"content,omitempty"`
	ContentTruncated   bool                           `json:"contentTruncated,omitempty"`
	Details            json.RawMessage                `json:"details,omitempty"`
	DetailsOmitted     bool                           `json:"detailsOmitted,omitempty"`
	IsError            bool                           `json:"isError,omitempty"`
	Status             kitsession.RunStatus           `json:"status,omitempty"`
	ErrorKind          kitsession.ProviderErrorKind   `json:"errorKind,omitempty"`
	ErrorMessage       string                         `json:"errorMessage,omitempty"`
}

// AppendSessionEvents assigns contiguous stream sequences and durably appends a batch.
func (s *Store) AppendSessionEvents(ctx context.Context, events []kitsession.NewEvent) ([]kitsession.Event, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is closed")
	}
	if len(events) == 0 {
		return nil, nil
	}
	sessionID := events[0].SessionID
	payloads := make([][]byte, len(events))
	for index, event := range events {
		if event.SessionID != sessionID {
			return nil, fmt.Errorf("event %d belongs to a different session", index)
		}
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("event %d: %w", index, err)
		}
		payload, err := json.Marshal(sessionEventPayload{
			TurnID: event.TurnID, RunID: event.RunID, MessageID: event.MessageID, ContentIndex: event.ContentIndex,
			Delta: event.Delta, Text: event.Text, Thinking: event.Thinking,
			ToolCallID: event.ToolCallID, ToolName: event.ToolName,
			Arguments: event.Arguments, ArgumentsTruncated: event.ArgumentsTruncated,
			Content: event.Content, ContentTruncated: event.ContentTruncated,
			Details: event.Details, DetailsOmitted: event.DetailsOmitted,
			IsError: event.IsError, Status: event.Status, ErrorKind: event.ErrorKind,
			ErrorMessage: event.ErrorMessage,
		})
		if err != nil {
			return nil, fmt.Errorf("encode event %d: %w", index, err)
		}
		payloads[index] = payload
	}
	streamID, err := identifier.New("stream_")
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin session event append: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_streams(session_id, stream_id, next_sequence, updated_at)
		VALUES (?, ?, 1, ?)
		ON CONFLICT(session_id) DO NOTHING
	`, sessionID, streamID, formatTimestamp(now)); err != nil {
		return nil, fmt.Errorf("ensure session stream: %w", err)
	}
	var storedStreamID string
	var firstSequence int64
	err = tx.QueryRowContext(ctx, `
		UPDATE session_streams
		SET next_sequence = next_sequence + ?, updated_at = ?
		WHERE session_id = ?
		RETURNING stream_id, next_sequence - ?
	`, len(events), formatTimestamp(now), sessionID, len(events)).Scan(&storedStreamID, &firstSequence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("allocate session event sequence: %w", err)
	}

	stored := make([]kitsession.Event, 0, len(events))
	for index, event := range events {
		sequence := firstSequence + int64(index)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_events(session_id, stream_id, sequence, kind, payload_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, sessionID, storedStreamID, sequence, event.Kind, payloads[index], formatTimestamp(now)); err != nil {
			return nil, fmt.Errorf("append session event %d: %w", index, err)
		}
		stored = append(stored, kitsession.Event{NewEvent: event, StreamID: storedStreamID, Sequence: sequence})
	}
	lastSequence := firstSequence + int64(len(events)) - 1
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM session_events
		WHERE session_id = ? AND stream_id = ? AND sequence <= ?
	`, sessionID, storedStreamID, lastSequence-retainedSessionEvents); err != nil {
		return nil, fmt.Errorf("prune session events by count: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM session_events
		WHERE session_id = ? AND stream_id = ? AND sequence IN (
			SELECT sequence FROM (
				SELECT sequence,
				       SUM(length(payload_json) + length(kind) + 64)
				       OVER (ORDER BY sequence DESC) AS retained_bytes
				FROM session_events
				WHERE session_id = ? AND stream_id = ?
			)
			WHERE retained_bytes > ?
		)
	`, sessionID, storedStreamID, sessionID, storedStreamID, retainedSessionEventBytes); err != nil {
		return nil, fmt.Errorf("prune session events by size: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit session event append: %w", err)
	}
	return stored, nil
}

// ListSessionEvents returns one bounded ordered page after sequence.
func (s *Store) ListSessionEvents(ctx context.Context, sessionID string, after int64, limit int) (kitsession.EventPage, error) {
	if s == nil || s.db == nil {
		return kitsession.EventPage{}, fmt.Errorf("store is closed")
	}
	if sessionID == "" || after < 0 {
		return kitsession.EventPage{}, fmt.Errorf("session id and non-negative sequence are required")
	}
	if limit <= 0 || limit > 64 {
		limit = 32
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return kitsession.EventPage{}, fmt.Errorf("begin session event read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var streamID string
	var nextSequence int64
	err = tx.QueryRowContext(ctx, `
		SELECT stream_id, next_sequence
		FROM session_streams
		WHERE session_id = ?
	`, sessionID).Scan(&streamID, &nextSequence)
	if errors.Is(err, sql.ErrNoRows) {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id = ?)`, sessionID).Scan(&exists); err != nil {
			return kitsession.EventPage{}, fmt.Errorf("verify session for event list: %w", err)
		}
		if exists == 0 {
			return kitsession.EventPage{}, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
		}
		if err := tx.Commit(); err != nil {
			return kitsession.EventPage{}, fmt.Errorf("commit empty session event read: %w", err)
		}
		return kitsession.EventPage{}, nil
	}
	if err != nil {
		return kitsession.EventPage{}, fmt.Errorf("load session stream: %w", err)
	}
	page := kitsession.EventPage{StreamID: streamID, LastSequence: nextSequence - 1}
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MIN(sequence), 0)
		FROM session_events
		WHERE session_id = ? AND stream_id = ?
	`, sessionID, streamID).Scan(&page.FirstSequence); err != nil {
		return kitsession.EventPage{}, fmt.Errorf("load session event retention cursor: %w", err)
	}
	if after > 0 && (page.FirstSequence > after+1 || after > page.LastSequence) {
		page.ResyncRequired = true
		if err := tx.Commit(); err != nil {
			return kitsession.EventPage{}, fmt.Errorf("commit expired session event read: %w", err)
		}
		return page, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT sequence, kind, payload_json
		FROM session_events
		WHERE session_id = ? AND stream_id = ? AND sequence > ?
		ORDER BY sequence
		LIMIT ?
	`, sessionID, streamID, after, limit)
	if err != nil {
		return kitsession.EventPage{}, fmt.Errorf("list session events: %w", err)
	}
	for rows.Next() {
		var sequence int64
		var kind kitsession.EventKind
		var encoded []byte
		if err := rows.Scan(&sequence, &kind, &encoded); err != nil {
			return kitsession.EventPage{}, fmt.Errorf("scan session event: %w", err)
		}
		var payload sessionEventPayload
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			return kitsession.EventPage{}, fmt.Errorf("decode session event %d: %w", sequence, err)
		}
		event := kitsession.Event{NewEvent: kitsession.NewEvent{
			SessionID: sessionID, TurnID: payload.TurnID, RunID: payload.RunID, MessageID: payload.MessageID, Kind: kind,
			ContentIndex: payload.ContentIndex, Delta: payload.Delta, Text: payload.Text,
			Thinking: payload.Thinking, ToolCallID: payload.ToolCallID, ToolName: payload.ToolName,
			Arguments: payload.Arguments, ArgumentsTruncated: payload.ArgumentsTruncated,
			Content: payload.Content, ContentTruncated: payload.ContentTruncated,
			Details: append(json.RawMessage(nil), payload.Details...), DetailsOmitted: payload.DetailsOmitted,
			IsError: payload.IsError, Status: payload.Status,
			ErrorKind: payload.ErrorKind, ErrorMessage: payload.ErrorMessage,
		}, StreamID: streamID, Sequence: sequence}
		if err := event.NewEvent.Validate(); err != nil {
			return kitsession.EventPage{}, fmt.Errorf("validate session event %d: %w", sequence, err)
		}
		page.Events = append(page.Events, event)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return kitsession.EventPage{}, fmt.Errorf("list session events: %w", err)
	}
	if err := rows.Close(); err != nil {
		return kitsession.EventPage{}, fmt.Errorf("close session events: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return kitsession.EventPage{}, fmt.Errorf("commit session event read: %w", err)
	}
	return page, nil
}
