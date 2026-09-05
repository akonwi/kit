package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ReserveParentRun creates a durable queued run generation before a client can
// receive a cancellable handle for it.
func (s *Store) ReserveParentRun(
	ctx context.Context,
	sessionID, turnID, runID string,
) (TurnRecord, ParentRunRecord, error) {
	if s == nil || s.db == nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("store is closed")
	}
	if sessionID == "" || turnID == "" || runID == "" {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("session, turn, and run ids are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("begin parent run reservation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	var sequence int64
	err = tx.QueryRowContext(ctx, `
		UPDATE sessions
		SET next_turn_sequence = next_turn_sequence + 1,
		    updated_at = ?
		WHERE id = ? AND archived_at IS NULL
		RETURNING next_turn_sequence - 1
	`, formatTimestamp(now), sessionID).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	if err != nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("allocate reserved turn sequence: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO turns(id, session_id, sequence, status, created_at)
		VALUES (?, ?, ?, 'pending', ?)
	`, turnID, sessionID, sequence, formatTimestamp(now)); err != nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("reserve turn %q: %w", turnID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO parent_runs(id, session_id, turn_id, status, created_at)
		VALUES (?, ?, ?, 'queued', ?)
	`, runID, sessionID, turnID, formatTimestamp(now)); err != nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("reserve parent run %q: %w", runID, err)
	}
	if err := tx.Commit(); err != nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("commit parent run reservation: %w", err)
	}
	return TurnRecord{
			ID: turnID, SessionID: sessionID, Sequence: sequence,
			Status: RunStatusPending, CreatedAt: now,
		}, ParentRunRecord{
			ID: runID, SessionID: sessionID, TurnID: turnID,
			Status: RunStatusQueued, CreatedAt: now,
		}, nil
}

// GetParentRun returns one exact durable parent-run generation.
func (s *Store) GetParentRun(ctx context.Context, sessionID, runID string) (ParentRunRecord, error) {
	if s == nil || s.db == nil {
		return ParentRunRecord{}, fmt.Errorf("store is closed")
	}
	var record ParentRunRecord
	var createdAt string
	var runError, startedAt, completedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, turn_id, status, error, created_at, started_at, completed_at
		FROM parent_runs
		WHERE id = ? AND session_id = ?
	`, runID, sessionID).Scan(
		&record.ID, &record.SessionID, &record.TurnID, &record.Status,
		&runError, &createdAt, &startedAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ParentRunRecord{}, fmt.Errorf("parent run %q: %w", runID, ErrNotFound)
	}
	if err != nil {
		return ParentRunRecord{}, fmt.Errorf("get parent run %q: %w", runID, err)
	}
	record.Error = runError.String
	record.CreatedAt, err = parseTimestamp(createdAt)
	if err != nil {
		return ParentRunRecord{}, fmt.Errorf("parse parent run created_at: %w", err)
	}
	if startedAt.Valid {
		value, err := parseTimestamp(startedAt.String)
		if err != nil {
			return ParentRunRecord{}, fmt.Errorf("parse parent run started_at: %w", err)
		}
		record.StartedAt = &value
	}
	if completedAt.Valid {
		value, err := parseTimestamp(completedAt.String)
		if err != nil {
			return ParentRunRecord{}, fmt.Errorf("parse parent run completed_at: %w", err)
		}
		record.EndedAt = &value
	}
	return record, nil
}

// GetActiveParentRun returns the queued or running generation for a session.
func (s *Store) GetActiveParentRun(ctx context.Context, sessionID string) (ParentRunRecord, error) {
	if s == nil || s.db == nil {
		return ParentRunRecord{}, fmt.Errorf("store is closed")
	}
	var record ParentRunRecord
	var createdAt string
	var runError, startedAt, completedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, turn_id, status, error, created_at, started_at, completed_at
		FROM parent_runs
		WHERE session_id = ? AND status IN ('queued', 'running')
		ORDER BY CASE status WHEN 'running' THEN 0 ELSE 1 END,
		         created_at, id
		LIMIT 1
	`, sessionID).Scan(
		&record.ID, &record.SessionID, &record.TurnID, &record.Status,
		&runError, &createdAt, &startedAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ParentRunRecord{}, fmt.Errorf("active parent run for session %q: %w", sessionID, ErrNotFound)
	}
	if err != nil {
		return ParentRunRecord{}, fmt.Errorf("get active parent run for session %q: %w", sessionID, err)
	}
	record.Error = runError.String
	record.CreatedAt, err = parseTimestamp(createdAt)
	if err != nil {
		return ParentRunRecord{}, fmt.Errorf("parse active parent run created_at: %w", err)
	}
	if startedAt.Valid {
		value, err := parseTimestamp(startedAt.String)
		if err != nil {
			return ParentRunRecord{}, fmt.Errorf("parse active parent run started_at: %w", err)
		}
		record.StartedAt = &value
	}
	if completedAt.Valid {
		value, err := parseTimestamp(completedAt.String)
		if err != nil {
			return ParentRunRecord{}, fmt.Errorf("parse active parent run completed_at: %w", err)
		}
		record.EndedAt = &value
	}
	return record, nil
}

// StartReservedParentRun atomically transitions one queued generation to
// running. A terminal status is returned unchanged so duplicate delivery does
// not execute it again.
func (s *Store) StartReservedParentRun(
	ctx context.Context,
	sessionID, runID string,
) (string, RunStatus, error) {
	if s == nil || s.db == nil {
		return "", "", fmt.Errorf("store is closed")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("begin reserved parent run: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := formatTimestamp(time.Now())
	var turnID string
	err = tx.QueryRowContext(ctx, `
		UPDATE parent_runs
		SET status = 'running', started_at = ?
		WHERE id = ? AND session_id = ? AND status = 'queued'
		RETURNING turn_id
	`, now, runID, sessionID).Scan(&turnID)
	if errors.Is(err, sql.ErrNoRows) {
		var status RunStatus
		err = tx.QueryRowContext(ctx, `
			SELECT turn_id, status
			FROM parent_runs
			WHERE id = ? AND session_id = ?
		`, runID, sessionID).Scan(&turnID, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("parent run %q: %w", runID, ErrNotFound)
		}
		if err != nil {
			return "", "", fmt.Errorf("load reserved parent run %q: %w", runID, err)
		}
		if err := tx.Commit(); err != nil {
			return "", "", fmt.Errorf("commit reserved parent run inspection: %w", err)
		}
		return turnID, status, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("start reserved parent run %q: %w", runID, err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE turns
		SET status = 'running', started_at = ?
		WHERE id = ? AND session_id = ? AND status = 'pending'
	`, now, turnID, sessionID)
	if err != nil {
		return "", "", fmt.Errorf("start reserved turn %q: %w", turnID, err)
	}
	if err := requireOneRow(result, "pending turn", turnID); err != nil {
		return "", "", err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sessions SET updated_at = ? WHERE id = ?", now, sessionID); err != nil {
		return "", "", fmt.Errorf("touch session %q: %w", sessionID, err)
	}
	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("commit reserved parent run start: %w", err)
	}
	return turnID, RunStatusRunning, nil
}

// AbortReservedParentRun atomically aborts a queued generation. Running and
// terminal statuses are returned unchanged for generation-safe coordination
// with the live runtime.
func (s *Store) AbortReservedParentRun(
	ctx context.Context,
	sessionID, runID, reason string,
) (RunStatus, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("store is closed")
	}
	if reason == "" {
		reason = "aborted before execution"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin reserved parent run abort: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := formatTimestamp(time.Now())
	var turnID string
	err = tx.QueryRowContext(ctx, `
		UPDATE parent_runs
		SET status = 'aborted', error = ?, completed_at = ?
		WHERE id = ? AND session_id = ? AND status = 'queued'
		RETURNING turn_id
	`, reason, now, runID, sessionID).Scan(&turnID)
	if errors.Is(err, sql.ErrNoRows) {
		var status RunStatus
		err = tx.QueryRowContext(ctx, `
			SELECT status
			FROM parent_runs
			WHERE id = ? AND session_id = ?
		`, runID, sessionID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("parent run %q: %w", runID, ErrNotFound)
		}
		if err != nil {
			return "", fmt.Errorf("load parent run for abort %q: %w", runID, err)
		}
		if err := tx.Commit(); err != nil {
			return "", fmt.Errorf("commit parent run abort inspection: %w", err)
		}
		return status, nil
	}
	if err != nil {
		return "", fmt.Errorf("abort queued parent run %q: %w", runID, err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE turns
		SET status = 'aborted', completed_at = ?
		WHERE id = ? AND session_id = ? AND status = 'pending'
	`, now, turnID, sessionID)
	if err != nil {
		return "", fmt.Errorf("abort pending turn %q: %w", turnID, err)
	}
	if err := requireOneRow(result, "pending turn", turnID); err != nil {
		return "", err
	}
	if err := releaseBashContextForIncompleteTurn(ctx, tx, sessionID, turnID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sessions SET updated_at = ? WHERE id = ?", now, sessionID); err != nil {
		return "", fmt.Errorf("touch session %q: %w", sessionID, err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit queued parent run abort: %w", err)
	}
	return RunStatusAborted, nil
}

// StartParentRun transactionally creates a running turn and its execution.
func (s *Store) StartParentRun(
	ctx context.Context,
	sessionID string,
	turnID string,
	runID string,
) (TurnRecord, ParentRunRecord, error) {
	if s == nil || s.db == nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("store is closed")
	}
	if sessionID == "" || turnID == "" || runID == "" {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("session, turn, and run ids are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("begin parent run: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	var sequence int64
	err = tx.QueryRowContext(ctx, `
		UPDATE sessions
		SET next_turn_sequence = next_turn_sequence + 1,
		    updated_at = ?
		WHERE id = ? AND archived_at IS NULL
		RETURNING next_turn_sequence - 1
	`, now.Format(timestampLayout), sessionID).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	if err != nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("allocate turn sequence: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO turns(id, session_id, sequence, status, started_at, created_at)
		VALUES (?, ?, ?, 'running', ?, ?)
	`, turnID, sessionID, sequence, now.Format(timestampLayout), now.Format(timestampLayout)); err != nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("create turn %q: %w", turnID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO parent_runs(id, session_id, turn_id, status, started_at, created_at)
		VALUES (?, ?, ?, 'running', ?, ?)
	`, runID, sessionID, turnID, now.Format(timestampLayout), now.Format(timestampLayout)); err != nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("create parent run %q: %w", runID, err)
	}
	if err := tx.Commit(); err != nil {
		return TurnRecord{}, ParentRunRecord{}, fmt.Errorf("commit parent run: %w", err)
	}
	started := now
	return TurnRecord{
			ID: turnID, SessionID: sessionID, Sequence: sequence,
			Status: RunStatusRunning, CreatedAt: now, StartedAt: &started,
		}, ParentRunRecord{
			ID: runID, SessionID: sessionID, TurnID: turnID,
			Status: RunStatusRunning, CreatedAt: now, StartedAt: &started,
		}, nil
}

// FinishParentRun transactionally records terminal turn and execution status.
func (s *Store) FinishParentRun(
	ctx context.Context,
	sessionID string,
	turnID string,
	runID string,
	status RunStatus,
	errorMessage string,
) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	if !terminalRunStatus(status) {
		return fmt.Errorf("parent run terminal status %q is invalid", status)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin parent run completion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(timestampLayout)
	result, err := tx.ExecContext(ctx, `
		UPDATE parent_runs
		SET status = ?, error = NULLIF(?, ''), completed_at = ?
		WHERE id = ? AND session_id = ? AND turn_id = ? AND status = 'running'
	`, status, errorMessage, now, runID, sessionID, turnID)
	if err != nil {
		return fmt.Errorf("finish parent run %q: %w", runID, err)
	}
	if err := requireOneRow(result, "running parent run", runID); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE turns
		SET status = ?, completed_at = ?
		WHERE id = ? AND session_id = ? AND status = 'running'
	`, status, now, turnID, sessionID)
	if err != nil {
		return fmt.Errorf("finish turn %q: %w", turnID, err)
	}
	if err := requireOneRow(result, "running turn", turnID); err != nil {
		return err
	}
	if status != RunStatusCompleted {
		if err := releaseBashContextForIncompleteTurn(ctx, tx, sessionID, turnID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sessions SET updated_at = ? WHERE id = ?", now, sessionID); err != nil {
		return fmt.Errorf("touch session %q: %w", sessionID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit parent run completion: %w", err)
	}
	return nil
}

// RecoverParentRun reconciles one exact execution after an ambiguous terminal
// write. An already-terminal run is returned unchanged; a still-running run and
// turn are atomically marked interrupted.
func (s *Store) RecoverParentRun(
	ctx context.Context,
	sessionID, turnID, runID, reason string,
) (RunStatus, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("store is closed")
	}
	if reason == "" {
		reason = "parent run settlement failed"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin parent run recovery: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var status RunStatus
	err = tx.QueryRowContext(ctx, `
		SELECT status
		FROM parent_runs
		WHERE id = ? AND session_id = ? AND turn_id = ?
	`, runID, sessionID, turnID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("parent run %q: %w", runID, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("inspect parent run %q: %w", runID, err)
	}
	if terminalRunStatus(status) {
		if status != RunStatusCompleted {
			if err := releaseBashContextForIncompleteTurn(ctx, tx, sessionID, turnID); err != nil {
				return "", err
			}
		}
		if err := tx.Commit(); err != nil {
			return "", fmt.Errorf("commit parent run inspection: %w", err)
		}
		return status, nil
	}
	if status != RunStatusRunning {
		return "", fmt.Errorf("parent run %q has unrecoverable status %q", runID, status)
	}
	now := formatTimestamp(time.Now())
	result, err := tx.ExecContext(ctx, `
		UPDATE parent_runs
		SET status = 'interrupted', error = ?, completed_at = ?
		WHERE id = ? AND session_id = ? AND turn_id = ? AND status = 'running'
	`, reason, now, runID, sessionID, turnID)
	if err != nil {
		return "", fmt.Errorf("interrupt parent run %q: %w", runID, err)
	}
	if err := requireOneRow(result, "running parent run", runID); err != nil {
		return "", err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE turns
		SET status = 'interrupted', completed_at = ?
		WHERE id = ? AND session_id = ? AND status = 'running'
	`, now, turnID, sessionID)
	if err != nil {
		return "", fmt.Errorf("interrupt turn %q: %w", turnID, err)
	}
	if err := requireOneRow(result, "running turn", turnID); err != nil {
		return "", err
	}
	if err := releaseBashContextForIncompleteTurn(ctx, tx, sessionID, turnID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit parent run recovery: %w", err)
	}
	return RunStatusInterrupted, nil
}

func releaseBashContextForIncompleteTurn(ctx context.Context, tx *sql.Tx, sessionID, turnID string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE messages
		SET payload_json = json_remove(payload_json, '$.contextBeforeTurnId')
		WHERE session_id = ? AND role = 'bash'
		  AND json_extract(payload_json, '$.contextBeforeTurnId') = ?
		  AND EXISTS (
			SELECT 1 FROM turns
			WHERE id = ? AND session_id = ? AND status <> 'completed'
		  )
	`, sessionID, turnID, turnID, sessionID); err != nil {
		return fmt.Errorf("release bash context for incomplete turn %q: %w", turnID, err)
	}
	return nil
}

func terminalRunStatus(status RunStatus) bool {
	switch status {
	case RunStatusCompleted, RunStatusFailed, RunStatusAborted, RunStatusInterrupted:
		return true
	default:
		return false
	}
}

func requireOneRow(result sql.Result, kind, id string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect %s %q update: %w", kind, id, err)
	}
	if count != 1 {
		return fmt.Errorf("%s %q: %w", kind, id, ErrNotFound)
	}
	return nil
}
