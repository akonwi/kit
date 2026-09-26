package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
	kitsession "github.com/akonwi/kit/internal/session"
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
	if input.ID == "" || input.CWD == "" || input.ModelProvider == "" || input.ModelID == "" || input.ScratchpadOwnerID == "" {
		return SessionRecord{}, fmt.Errorf("session id, cwd, scratchpad owner, model provider, and model id are required")
	}
	if !input.Persistent {
		return SessionRecord{}, fmt.Errorf("durable session repository does not accept temporary sessions")
	}
	now := time.Now().UTC()
	persistent := 0
	if input.Persistent {
		persistent = 1
	}
	var initializedAt any
	if input.DroidInitializedAt != nil {
		initializedAt = input.DroidInitializedAt.UTC().Format(timestampLayout)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("begin session %q creation: %w", input.ID, err)
	}
	defer func() { _ = tx.Rollback() }()
	var deleted int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM deleted_session_ids WHERE id = ?`, input.ID).Scan(&deleted); err == nil {
		return SessionRecord{}, fmt.Errorf("session id %q was permanently deleted: %w", input.ID, kitsession.ErrInvalidInput)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, fmt.Errorf("check deleted session id %q: %w", input.ID, err)
	}

	ownerID := input.ScratchpadOwnerID
	if ownerID != input.ID {
		if input.ParentSessionID == "" {
			return SessionRecord{}, fmt.Errorf("inherited scratchpad owner requires a parent session")
		}
		var parentOwnerID string
		if err := tx.QueryRowContext(ctx,
			`SELECT scratchpad_owner_id FROM sessions WHERE id = ?`,
			input.ParentSessionID,
		).Scan(&parentOwnerID); errors.Is(err, sql.ErrNoRows) {
			return SessionRecord{}, fmt.Errorf("parent session %q: %w", input.ParentSessionID, ErrNotFound)
		} else if err != nil {
			return SessionRecord{}, fmt.Errorf("load parent session %q scratchpad owner: %w", input.ParentSessionID, err)
		}
		if parentOwnerID != ownerID {
			return SessionRecord{}, fmt.Errorf("scratchpad owner %q does not match parent session family", ownerID)
		}
	}
	formattedNow := now.Format(timestampLayout)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sessions(
			id, cwd, name, persistent, parent_session_id, scratchpad_owner_id,
			model_provider, model_id, thinking_level, droid_initialized_at, created_at, updated_at
		) VALUES (?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
	`,
		input.ID,
		input.CWD,
		input.Name,
		persistent,
		input.ParentSessionID,
		ownerID,
		input.ModelProvider,
		input.ModelID,
		input.ThinkingLevel,
		initializedAt,
		formattedNow,
		formattedNow,
	)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("create session %q: %w", input.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return SessionRecord{}, fmt.Errorf("commit session %q creation: %w", input.ID, err)
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
			SELECT id, cwd, name, persistent, parent_session_id, scratchpad_owner_id,
			       model_provider, model_id, thinking_level, configuration_revision, droid_initialized_at,
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

	changed := 0
	if mutation.Changed {
		changed = 1
	}
	activityAt := formatTimestamp(time.Now())
	record, err := scanSession(tx.QueryRowContext(ctx, `
		UPDATE sessions
		SET cwd = CASE WHEN ? = 1 THEN ? ELSE cwd END,
		    updated_at = CASE WHEN updated_at < ? THEN ? ELSE updated_at END
		WHERE id = ? AND archived_at IS NULL
		RETURNING id, cwd, name, persistent, parent_session_id, scratchpad_owner_id,
		          model_provider, model_id, thinking_level, configuration_revision, droid_initialized_at,
		          created_at, updated_at, archived_at
	`, changed, mutation.CWD, activityAt, activityAt, mutation.SessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, CWDMutation{}, fmt.Errorf("session %q: %w", mutation.SessionID, ErrNotFound)
	}
	if err != nil {
		return SessionRecord{}, CWDMutation{}, fmt.Errorf("change session %q cwd: %w", mutation.SessionID, err)
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

// UpdateSessionConfiguration atomically changes the exact model and thinking
// configuration behind an expected-revision guard and advances activity
// without allowing a delayed clock value to move it backward.
func (s *Store) UpdateSessionConfiguration(ctx context.Context, update ConfigurationUpdate) (SessionRecord, error) {
	if s == nil || s.db == nil {
		return SessionRecord{}, fmt.Errorf("store is closed")
	}
	if err := validateConfigurationUpdate(update); err != nil {
		return SessionRecord{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var provider, modelID, thinking sql.NullString
	var revision uint64
	var updatedAtText string
	err = tx.QueryRowContext(ctx, `
		SELECT model_provider, model_id, thinking_level, configuration_revision, updated_at
		FROM sessions WHERE id = ? AND archived_at IS NULL
	`, update.SessionID).Scan(&provider, &modelID, &thinking, &revision, &updatedAtText)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, fmt.Errorf("session %q: %w", update.SessionID, ErrNotFound)
	}
	if err != nil {
		return SessionRecord{}, fmt.Errorf("load session %q configuration: %w", update.SessionID, err)
	}
	if revision != update.ExpectedRevision {
		return SessionRecord{}, &ConfigurationConflictError{Expected: update.ExpectedRevision, Actual: revision}
	}

	changed := provider.String != update.ModelProvider || modelID.String != update.ModelID || thinking.String != update.ThinkingLevel
	resultRevision := revision
	if changed {
		if revision >= math.MaxInt64 {
			return SessionRecord{}, fmt.Errorf("session configuration revision overflow")
		}
		resultRevision++
	}
	updatedAt, err := parseTimestamp(updatedAtText)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("parse session updated_at: %w", err)
	}
	activityAt := time.Now().UTC()
	if updatedAt.After(activityAt) {
		activityAt = updatedAt
	}
	record, err := scanSession(tx.QueryRowContext(ctx, `
		UPDATE sessions
		SET model_provider = ?, model_id = ?, thinking_level = NULLIF(?, ''),
		    configuration_revision = ?, updated_at = ?
		WHERE id = ? AND archived_at IS NULL AND configuration_revision = ?
		RETURNING id, cwd, name, persistent, parent_session_id, scratchpad_owner_id,
		          model_provider, model_id, thinking_level, configuration_revision, droid_initialized_at,
		          created_at, updated_at, archived_at
	`, update.ModelProvider, update.ModelID, update.ThinkingLevel,
		resultRevision, formatTimestamp(activityAt), update.SessionID, update.ExpectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, &ConfigurationConflictError{Expected: update.ExpectedRevision, Actual: revision}
	}
	if err != nil {
		return SessionRecord{}, fmt.Errorf("configure session %q: %w", update.SessionID, err)
	}
	if err := tx.Commit(); err != nil {
		return SessionRecord{}, fmt.Errorf("commit session %q configuration: %w", update.SessionID, err)
	}
	return record, nil
}

func validateConfigurationUpdate(update ConfigurationUpdate) error {
	if update.SessionID == "" || update.ModelProvider == "" || update.ModelID == "" {
		return fmt.Errorf("session id, model provider, and model id are required")
	}
	if update.ExpectedRevision == 0 || update.ExpectedRevision > math.MaxInt64 {
		return fmt.Errorf("expected configuration revision is invalid")
	}
	for name, value := range map[string]string{
		"session id": update.SessionID, "model provider": update.ModelProvider,
		"model id": update.ModelID, "thinking level": update.ThinkingLevel,
	} {
		if len(value) > 256 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return fmt.Errorf("%s must be valid UTF-8 without NUL and at most 256 bytes", name)
		}
	}
	return nil
}

// RenameSession replaces a non-archived session's display name.
func (s *Store) RenameSession(ctx context.Context, id, name string) (SessionRecord, error) {
	if s == nil || s.db == nil {
		return SessionRecord{}, fmt.Errorf("store is closed")
	}
	activityAt := formatTimestamp(time.Now())
	row := s.db.QueryRowContext(ctx, `
		UPDATE sessions
		SET name = ?,
		    updated_at = CASE WHEN updated_at < ? THEN ? ELSE updated_at END
		WHERE id = ? AND archived_at IS NULL
		RETURNING id, cwd, name, persistent, parent_session_id, scratchpad_owner_id,
		          model_provider, model_id, thinking_level, configuration_revision, droid_initialized_at,
		          created_at, updated_at, archived_at
	`, name, activityAt, activityAt, id)
	record, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, fmt.Errorf("session %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return SessionRecord{}, fmt.Errorf("rename session %q: %w", id, err)
	}
	return record, nil
}

// TouchSession advances a non-archived session's activity time without allowing
// a delayed operation to move it backward.
func (s *Store) TouchSession(ctx context.Context, id string, activityAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	if activityAt.IsZero() {
		return fmt.Errorf("session activity time is required")
	}
	formatted := formatTimestamp(activityAt)
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions
		SET updated_at = CASE WHEN updated_at < ? THEN ? ELSE updated_at END
		WHERE id = ? AND archived_at IS NULL
	`, formatted, formatted, id)
	if err != nil {
		return fmt.Errorf("touch session %q: %w", id, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("touch session %q: %w", id, err)
	}
	if count == 1 {
		return nil
	}
	return fmt.Errorf("session %q: %w", id, ErrNotFound)
}

// CompleteSessionDeletion records that external stores and attachments were removed.
func (s *Store) CompleteSessionDeletion(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE deleted_session_ids SET cleanup_pending = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("complete session deletion %q: %w", id, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM deleted_session_ids WHERE id = ?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("session deletion %q: %w", id, ErrNotFound)
		} else if err != nil {
			return fmt.Errorf("check session deletion %q: %w", id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_deletion_artifacts WHERE session_id = ?`, id); err != nil {
		return fmt.Errorf("complete deletion artifacts for %q: %w", id, err)
	}
	return tx.Commit()
}

// ListSessionDeletionArtifacts returns the durable external-store inventory for a deletion job.
func (s *Store) ListSessionDeletionArtifacts(ctx context.Context, id string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is closed")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT artifact_id FROM session_deletion_artifacts
		WHERE session_id = ? AND artifact_kind = 'subagent_store' ORDER BY artifact_id
	`, id)
	if err != nil {
		return nil, fmt.Errorf("list deletion artifacts for %q: %w", id, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var artifactID string
		if err := rows.Scan(&artifactID); err != nil {
			return nil, err
		}
		ids = append(ids, artifactID)
	}
	return ids, rows.Err()
}

// DeleteSession permanently deletes a session and all registry data owned by it.
// When semantic forks share its scratchpad, ownership transfers to a surviving
// direct child so the remaining sessions keep the same shared contents.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.DeleteSessionWithArtifacts(ctx, id)
}

// DeleteSessionWithArtifacts permanently deletes a session and records its exact
// external child-store inventory in the durable cleanup job.
func (s *Store) DeleteSessionWithArtifacts(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	if id == "" {
		return fmt.Errorf("session id is empty")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			var deleted int
			if deletedErr := tx.QueryRowContext(ctx, `SELECT 1 FROM deleted_session_ids WHERE id = ?`, id).Scan(&deleted); deletedErr == nil {
				return nil
			} else if !errors.Is(deletedErr, sql.ErrNoRows) {
				return fmt.Errorf("check deleted session %q: %w", id, deletedErr)
			}
			return fmt.Errorf("session %q: %w", id, ErrNotFound)
		}
		return fmt.Errorf("find session %q for deletion: %w", id, err)
	}

	var dependentCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sessions WHERE scratchpad_owner_id = ? AND id <> ?
	`, id, id).Scan(&dependentCount); err != nil {
		return fmt.Errorf("count sessions sharing scratchpad %q: %w", id, err)
	}
	if dependentCount > 0 {
		var successor string
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM sessions
			WHERE scratchpad_owner_id = ? AND id <> ?
			ORDER BY created_at, id LIMIT 1
		`, id, id).Scan(&successor)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("cannot delete session %q: shared scratchpad has no surviving owner", id)
		}
		if err != nil {
			return fmt.Errorf("find successor scratchpad owner for %q: %w", id, err)
		}

		var content string
		var revision int64
		var updatedAt string
		var migrationRequired int
		if err := tx.QueryRowContext(ctx, `
			SELECT content, revision, updated_at, migration_required
			FROM scratchpads WHERE owner_session_id = ?
		`, id).Scan(&content, &revision, &updatedAt, &migrationRequired); err != nil {
			return fmt.Errorf("load shared scratchpad %q for transfer: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO scratchpads(owner_session_id, content, revision, updated_at, migration_required)
			VALUES (?, ?, ?, ?, ?)
		`, successor, content, revision, updatedAt, migrationRequired); err != nil {
			return fmt.Errorf("copy shared scratchpad %q to successor %q: %w", id, successor, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO scratchpad_owner_transfers(source_owner_id, target_owner_id)
			VALUES (?, ?)
		`, id, successor); err != nil {
			return fmt.Errorf("authorize shared scratchpad transfer from %q to %q: %w", id, successor, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE sessions SET scratchpad_owner_id = ?
			WHERE scratchpad_owner_id = ? AND id <> ?
		`, successor, id, id); err != nil {
			return fmt.Errorf("transfer shared scratchpad %q to successor %q: %w", id, successor, err)
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM scratchpad_owner_transfers WHERE source_owner_id = ? AND target_owner_id = ?
		`, id, successor); err != nil {
			return fmt.Errorf("finish shared scratchpad transfer from %q to %q: %w", id, successor, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO deleted_session_ids(id, deleted_at, cleanup_pending)
		VALUES (?, ?, 1)
	`, id, formatTimestamp(time.Now().UTC())); err != nil {
		return fmt.Errorf("reserve deleted session id %q: %w", id, err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM subagent_conversations WHERE owner_session_id = ?`, id)
	if err != nil {
		return fmt.Errorf("list child stores for session %q: %w", id, err)
	}
	var childStoreIDs []string
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read child store for session %q: %w", id, err)
		}
		if !identifier.Valid(childID, "subagent_") {
			_ = rows.Close()
			return fmt.Errorf("invalid subagent store id %q", childID)
		}
		childStoreIDs = append(childStoreIDs, childID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate child stores for session %q: %w", id, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close child store inventory for session %q: %w", id, err)
	}
	for _, childID := range childStoreIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_deletion_artifacts(session_id, artifact_kind, artifact_id)
			VALUES (?, 'subagent_store', ?)
		`, id, childID); err != nil {
			return fmt.Errorf("record deletion artifact %q for session %q: %w", childID, id, err)
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session %q: %w", id, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted session %q: %w", id, err)
	}
	if count != 1 {
		return fmt.Errorf("session %q: %w", id, ErrNotFound)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deletion of session %q: %w", id, err)
	}
	return nil
}

// ArchiveSession hides a session from future loads while retaining its durable data.
// Owned subagent conversations and queued/running work are tombstoned in the
// same transaction, since normal archival does not activate foreign-key cascades.
func (s *Store) ArchiveSession(ctx context.Context, id string, archivedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	archivedAt = archivedAt.UTC()
	formatted := formatTimestamp(archivedAt)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET archived_at = ?,
		    updated_at = CASE WHEN updated_at < ? THEN ? ELSE updated_at END
		WHERE id = ? AND archived_at IS NULL
	`, formatted, formatted, formatted, id)
	if err != nil {
		return fmt.Errorf("archive session %q: %w", id, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		var existingArchivedAt sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT archived_at FROM sessions WHERE id = ?`, id).Scan(&existingArchivedAt)
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
	if err := archiveOwnedSubagents(ctx, tx, id, archivedAt); err != nil {
		return fmt.Errorf("archive session %q subagents: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE peer_session_queries
		SET state = 'recipient_archived', terminal_error = 'recipient session was archived',
		    completed_at = ?, generation = generation + 1
		WHERE recipient_session_id = ? AND state = 'queued'
	`, formatted, id); err != nil {
		return fmt.Errorf("archive session %q peer queries: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit archived session %q: %w", id, err)
	}
	return nil
}

// GetSession loads one session by exact id, including archived tombstones.
func (s *Store) GetSession(ctx context.Context, id string) (SessionRecord, error) {
	if s == nil || s.db == nil {
		return SessionRecord{}, fmt.Errorf("store is closed")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, cwd, name, persistent, parent_session_id, scratchpad_owner_id,
		       model_provider, model_id, thinking_level, configuration_revision, droid_initialized_at,
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
		SELECT id, cwd, name, persistent, parent_session_id, scratchpad_owner_id,
		       model_provider, model_id, thinking_level, configuration_revision, droid_initialized_at,
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
		&record.ScratchpadOwnerID,
		&modelProvider,
		&modelID,
		&thinking,
		&record.ConfigurationRevision,
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
		SET droid_initialized_at = COALESCE(droid_initialized_at, ?)
		WHERE id = ? AND archived_at IS NULL
	`, formatTimestamp(initializedAt), sessionID)
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
