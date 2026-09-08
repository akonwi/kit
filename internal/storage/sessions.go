package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

const timestampLayout = "2006-01-02T15:04:05.000000000Z"

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(timestampLayout)
}

func parseTimestamp(value string) (time.Time, error) {
	parsed, err := time.Parse(timestampLayout, value)
	if err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}

// CreateSession inserts session metadata.
func (s *Store) CreateSession(ctx context.Context, input NewSession) (SessionRecord, error) {
	if s == nil || s.db == nil {
		return SessionRecord{}, fmt.Errorf("store is closed")
	}
	if input.ID == "" || input.CWD == "" || input.ModelProvider == "" || input.ModelID == "" {
		return SessionRecord{}, fmt.Errorf("session id, cwd, model provider, and model id are required")
	}
	now := time.Now().UTC()
	persistent := 0
	if input.Persistent {
		persistent = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions(
			id, cwd, name, persistent, parent_session_id,
			model_provider, model_id, thinking_level, created_at, updated_at
		) VALUES (?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?, ?)
	`,
		input.ID,
		input.CWD,
		input.Name,
		persistent,
		input.ParentSessionID,
		input.ModelProvider,
		input.ModelID,
		input.ThinkingLevel,
		now.Format(timestampLayout),
		now.Format(timestampLayout),
	)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("create session %q: %w", input.ID, err)
	}
	return s.GetSession(ctx, input.ID)
}

// GetSessionCWDMutation loads one durable cwd-mutation receipt.
func (s *Store) GetSessionCWDMutation(ctx context.Context, sessionID, mutationID string) (CWDMutation, error) {
	if s == nil || s.db == nil {
		return CWDMutation{}, fmt.Errorf("store is closed")
	}
	mutation, err := scanCWDMutation(s.db.QueryRowContext(ctx, `
		SELECT mutation_id, session_id, target_path, previous_cwd, cwd, changed
		FROM session_cwd_mutations WHERE session_id = ? AND mutation_id = ?
	`, sessionID, mutationID))
	if errors.Is(err, sql.ErrNoRows) {
		return CWDMutation{}, fmt.Errorf("cwd mutation %q for session %q: %w", mutationID, sessionID, ErrNotFound)
	}
	if err != nil {
		return CWDMutation{}, fmt.Errorf("load cwd mutation %q: %w", mutationID, err)
	}
	return mutation, nil
}

// ApplySessionCWDMutation atomically records an idempotency receipt and updates
// the session cwd. Reusing an id returns the original receipt without applying
// the relative target a second time.
func (s *Store) ApplySessionCWDMutation(ctx context.Context, mutation CWDMutation) (SessionRecord, CWDMutation, error) {
	if s == nil || s.db == nil {
		return SessionRecord{}, CWDMutation{}, fmt.Errorf("store is closed")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionRecord{}, CWDMutation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	existing, err := scanCWDMutation(tx.QueryRowContext(ctx, `
		SELECT mutation_id, session_id, target_path, previous_cwd, cwd, changed
		FROM session_cwd_mutations WHERE session_id = ? AND mutation_id = ?
	`, mutation.SessionID, mutation.ID))
	if err == nil {
		if existing.TargetPath != mutation.TargetPath {
			return SessionRecord{}, CWDMutation{}, fmt.Errorf("cwd mutation id %q was reused", mutation.ID)
		}
		record, loadErr := scanSession(tx.QueryRowContext(ctx, `
			SELECT id, cwd, name, persistent, parent_session_id,
			       model_provider, model_id, thinking_level, droid_initialized_at,
			       created_at, updated_at, archived_at
			FROM sessions WHERE id = ? AND archived_at IS NULL
		`, mutation.SessionID))
		if errors.Is(loadErr, sql.ErrNoRows) {
			loadErr = fmt.Errorf("session %q: %w", mutation.SessionID, ErrNotFound)
		}
		return record, existing, loadErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, CWDMutation{}, fmt.Errorf("check cwd mutation %q: %w", mutation.ID, err)
	}

	var record SessionRecord
	if mutation.Changed {
		record, err = scanSession(tx.QueryRowContext(ctx, `
			UPDATE sessions SET cwd = ?, updated_at = ?
			WHERE id = ? AND archived_at IS NULL
			RETURNING id, cwd, name, persistent, parent_session_id,
			          model_provider, model_id, thinking_level, droid_initialized_at,
			          created_at, updated_at, archived_at
		`, mutation.CWD, formatTimestamp(time.Now()), mutation.SessionID))
	} else {
		record, err = scanSession(tx.QueryRowContext(ctx, `
			SELECT id, cwd, name, persistent, parent_session_id,
			       model_provider, model_id, thinking_level, droid_initialized_at,
			       created_at, updated_at, archived_at
			FROM sessions WHERE id = ? AND archived_at IS NULL
		`, mutation.SessionID))
	}
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, CWDMutation{}, fmt.Errorf("session %q: %w", mutation.SessionID, ErrNotFound)
	}
	if err != nil {
		return SessionRecord{}, CWDMutation{}, fmt.Errorf("change session %q cwd: %w", mutation.SessionID, err)
	}
	changed := 0
	if mutation.Changed {
		changed = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_cwd_mutations(
			session_id, mutation_id, target_path, previous_cwd, cwd, changed, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, mutation.SessionID, mutation.ID, mutation.TargetPath, mutation.PreviousCWD, mutation.CWD, changed, formatTimestamp(time.Now())); err != nil {
		return SessionRecord{}, CWDMutation{}, fmt.Errorf("record cwd mutation %q: %w", mutation.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return SessionRecord{}, CWDMutation{}, fmt.Errorf("commit cwd mutation %q: %w", mutation.ID, err)
	}
	return record, mutation, nil
}

func scanCWDMutation(scanner rowScanner) (CWDMutation, error) {
	var mutation CWDMutation
	var changed int
	err := scanner.Scan(&mutation.ID, &mutation.SessionID, &mutation.TargetPath, &mutation.PreviousCWD, &mutation.CWD, &changed)
	mutation.Changed = changed == 1
	return mutation, err
}

// RenameSession replaces a non-archived session's display name.
func (s *Store) RenameSession(ctx context.Context, id, name string) (SessionRecord, error) {
	if s == nil || s.db == nil {
		return SessionRecord{}, fmt.Errorf("store is closed")
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE sessions
		SET name = ?, updated_at = ?
		WHERE id = ? AND archived_at IS NULL
		RETURNING id, cwd, name, persistent, parent_session_id,
		          model_provider, model_id, thinking_level, droid_initialized_at,
		          created_at, updated_at, archived_at
	`, name, formatTimestamp(time.Now()), id)
	record, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, fmt.Errorf("session %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return SessionRecord{}, fmt.Errorf("rename session %q: %w", id, err)
	}
	return record, nil
}

// ArchiveSession hides a session from future loads while retaining its durable data.
func (s *Store) ArchiveSession(ctx context.Context, id string, archivedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions
		SET archived_at = ?, updated_at = ?
		WHERE id = ? AND archived_at IS NULL
	`, formatTimestamp(archivedAt), formatTimestamp(archivedAt), id)
	if err != nil {
		return fmt.Errorf("archive session %q: %w", id, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	var existingArchivedAt sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT archived_at FROM sessions WHERE id = ?`, id).Scan(&existingArchivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("session %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("check archived session %q: %w", id, err)
	}
	if existingArchivedAt.Valid {
		return nil
	}
	return fmt.Errorf("session %q: %w", id, ErrNotFound)
}

// GetSession loads one session by exact id, including archived tombstones.
func (s *Store) GetSession(ctx context.Context, id string) (SessionRecord, error) {
	if s == nil || s.db == nil {
		return SessionRecord{}, fmt.Errorf("store is closed")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, cwd, name, persistent, parent_session_id,
		       model_provider, model_id, thinking_level, droid_initialized_at,
		       created_at, updated_at, archived_at
		FROM sessions
		WHERE id = ?
	`, id)
	record, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, fmt.Errorf("session %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return SessionRecord{}, fmt.Errorf("load session %q: %w", id, err)
	}
	return record, nil
}

// ListSessions returns non-archived sessions, optionally limited to one cwd.
func (s *Store) ListSessions(ctx context.Context, cwd string) ([]SessionRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is closed")
	}
	query := `
		SELECT id, cwd, name, persistent, parent_session_id,
		       model_provider, model_id, thinking_level, droid_initialized_at,
		       created_at, updated_at, archived_at
		FROM sessions
		WHERE archived_at IS NULL`
	arguments := []any{}
	if cwd != "" {
		query += " AND cwd = ?"
		arguments = append(arguments, cwd)
	}
	query += " ORDER BY id"

	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []SessionRecord
	for rows.Next() {
		record, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("decode session: %w", err)
		}
		sessions = append(sessions, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(scanner rowScanner) (SessionRecord, error) {
	var (
		record                                SessionRecord
		name, parent, thinking, initializedAt sql.NullString
		modelProvider, modelID                sql.NullString
		createdAt, updatedAt                  string
		archivedAt                            sql.NullString
		persistent                            int
	)
	if err := scanner.Scan(
		&record.ID,
		&record.CWD,
		&name,
		&persistent,
		&parent,
		&modelProvider,
		&modelID,
		&thinking,
		&initializedAt,
		&createdAt,
		&updatedAt,
		&archivedAt,
	); err != nil {
		return SessionRecord{}, err
	}
	var err error
	record.CreatedAt, err = parseTimestamp(createdAt)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("parse created_at: %w", err)
	}
	record.UpdatedAt, err = parseTimestamp(updatedAt)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("parse updated_at: %w", err)
	}
	if archivedAt.Valid {
		value, err := parseTimestamp(archivedAt.String)
		if err != nil {
			return SessionRecord{}, fmt.Errorf("parse archived_at: %w", err)
		}
		record.ArchivedAt = &value
	}
	record.Name = name.String
	record.Persistent = persistent == 1
	record.ParentSessionID = parent.String
	record.ModelProvider = modelProvider.String
	record.ModelID = modelID.String
	record.ThinkingLevel = thinking.String
	if initializedAt.Valid {
		value, err := parseTimestamp(initializedAt.String)
		if err != nil {
			return SessionRecord{}, fmt.Errorf("parse droid_initialized_at: %w", err)
		}
		record.DroidInitializedAt = &value
	}
	return record, nil
}

// MarkDroidInitialized records the one-time successful droid Store handshake.
func (s *Store) MarkDroidInitialized(ctx context.Context, sessionID string, initializedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions
		SET droid_initialized_at = COALESCE(droid_initialized_at, ?), updated_at = ?
		WHERE id = ? AND archived_at IS NULL
	`, formatTimestamp(initializedAt), formatTimestamp(time.Now()), sessionID)
	if err != nil {
		return fmt.Errorf("mark session droid initialized: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	return nil
}
