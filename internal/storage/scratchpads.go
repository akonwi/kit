package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/akonwi/kit/internal/scratchpad"
)

// Get loads the scratchpad shared by the bound persistent session's family.
func (s *Store) Get(ctx context.Context, sessionID string) (scratchpad.Record, error) {
	if s == nil || s.db == nil {
		return scratchpad.Record{}, fmt.Errorf("store is closed")
	}
	return scanScratchpad(s.db.QueryRowContext(ctx, `
		SELECT p.owner_session_id, p.content, p.revision, p.updated_at, p.migration_required
		FROM sessions AS bound
		JOIN scratchpads AS p ON p.owner_session_id = bound.scratchpad_owner_id
		WHERE bound.id = ? AND bound.archived_at IS NULL
	`, sessionID))
}

// Update atomically replaces family scratchpad content behind a revision guard.
func (s *Store) Update(ctx context.Context, sessionID string, expectedRevision int64, content string) (scratchpad.Record, error) {
	if s == nil || s.db == nil {
		return scratchpad.Record{}, fmt.Errorf("store is closed")
	}
	if err := scratchpad.ValidateRevision(expectedRevision); err != nil {
		return scratchpad.Record{}, err
	}
	if err := scratchpad.ValidateContent(content); err != nil {
		return scratchpad.Record{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return scratchpad.Record{}, fmt.Errorf("begin scratchpad update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := scanScratchpad(tx.QueryRowContext(ctx, `
		SELECT p.owner_session_id, p.content, p.revision, p.updated_at, p.migration_required
		FROM sessions AS bound
		JOIN scratchpads AS p ON p.owner_session_id = bound.scratchpad_owner_id
		WHERE bound.id = ? AND bound.archived_at IS NULL
	`, sessionID))
	if err != nil {
		return scratchpad.Record{}, err
	}
	if current.Revision != expectedRevision {
		return scratchpad.Record{}, &scratchpad.ConflictError{Expected: expectedRevision, Current: current}
	}
	if current.Content == content {
		return current, nil
	}
	if current.Revision == math.MaxInt64 {
		return scratchpad.Record{}, scratchpad.ErrRevisionExhausted
	}
	updatedAt := time.Now().UTC()
	if current.UpdatedAt.After(updatedAt) {
		updatedAt = current.UpdatedAt
	}
	updated, err := scanScratchpad(tx.QueryRowContext(ctx, `
		UPDATE scratchpads
		SET content = ?, revision = revision + 1, updated_at = ?
		WHERE owner_session_id = ? AND revision = ? AND migration_required = 0
		RETURNING owner_session_id, content, revision, updated_at, migration_required
	`, content, formatTimestamp(updatedAt), current.OwnerSessionID, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		latest, loadErr := scanScratchpad(tx.QueryRowContext(ctx, `
			SELECT owner_session_id, content, revision, updated_at, migration_required
			FROM scratchpads WHERE owner_session_id = ?
		`, current.OwnerSessionID))
		if loadErr != nil {
			return scratchpad.Record{}, loadErr
		}
		return scratchpad.Record{}, &scratchpad.ConflictError{Expected: expectedRevision, Current: latest}
	}
	if err != nil {
		return scratchpad.Record{}, fmt.Errorf("update scratchpad %q: %w", current.OwnerSessionID, err)
	}
	if err := tx.Commit(); err != nil {
		return scratchpad.Record{}, fmt.Errorf("commit scratchpad %q update: %w", current.OwnerSessionID, err)
	}
	return updated, nil
}

// Edit applies simultaneous exact replacements to the latest committed family scratchpad.
func (s *Store) Edit(ctx context.Context, sessionID string, edits []scratchpad.Edit) (scratchpad.Record, int, bool, error) {
	if s == nil || s.db == nil {
		return scratchpad.Record{}, 0, false, fmt.Errorf("store is closed")
	}
	if err := scratchpad.ValidateEdits(edits); err != nil {
		return scratchpad.Record{}, 0, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return scratchpad.Record{}, 0, false, fmt.Errorf("begin scratchpad edit: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := scanScratchpad(tx.QueryRowContext(ctx, `
		SELECT p.owner_session_id, p.content, p.revision, p.updated_at, p.migration_required
		FROM sessions AS bound
		JOIN scratchpads AS p ON p.owner_session_id = bound.scratchpad_owner_id
		WHERE bound.id = ? AND bound.archived_at IS NULL
	`, sessionID))
	if err != nil {
		return scratchpad.Record{}, 0, false, err
	}
	updatedContent, applied, err := scratchpad.ApplyExactEdits(current.Content, edits)
	if err != nil {
		return scratchpad.Record{}, 0, false, err
	}
	if updatedContent == current.Content {
		return current, applied, false, nil
	}
	if current.Revision == math.MaxInt64 {
		return scratchpad.Record{}, 0, false, scratchpad.ErrRevisionExhausted
	}
	updatedAt := time.Now().UTC()
	if current.UpdatedAt.After(updatedAt) {
		updatedAt = current.UpdatedAt
	}
	updated, err := scanScratchpad(tx.QueryRowContext(ctx, `
		UPDATE scratchpads
		SET content = ?, revision = revision + 1, updated_at = ?
		WHERE owner_session_id = ? AND revision = ? AND migration_required = 0
		RETURNING owner_session_id, content, revision, updated_at, migration_required
	`, updatedContent, formatTimestamp(updatedAt), current.OwnerSessionID, current.Revision))
	if errors.Is(err, sql.ErrNoRows) {
		return scratchpad.Record{}, 0, false, scratchpad.ErrUnavailable
	}
	if err != nil {
		return scratchpad.Record{}, 0, false, fmt.Errorf("edit scratchpad %q: %w", current.OwnerSessionID, err)
	}
	if err := tx.Commit(); err != nil {
		return scratchpad.Record{}, 0, false, fmt.Errorf("commit scratchpad %q edit: %w", current.OwnerSessionID, err)
	}
	return updated, applied, true, nil
}

func scanScratchpad(scanner rowScanner) (scratchpad.Record, error) {
	var record scratchpad.Record
	var updatedAt string
	var migrationRequired int
	if err := scanner.Scan(&record.OwnerSessionID, &record.Content, &record.Revision, &updatedAt, &migrationRequired); errors.Is(err, sql.ErrNoRows) {
		return scratchpad.Record{}, scratchpad.ErrNotFound
	} else if err != nil {
		return scratchpad.Record{}, err
	}
	if migrationRequired == 1 {
		return scratchpad.Record{}, scratchpad.ErrMigrationRequired
	}
	var err error
	record.UpdatedAt, err = parseTimestamp(updatedAt)
	if err != nil {
		return scratchpad.Record{}, fmt.Errorf("parse scratchpad updated_at: %w", err)
	}
	if err := scratchpad.ValidateRevision(record.Revision); err != nil {
		return scratchpad.Record{}, fmt.Errorf("invalid persisted scratchpad revision: %w", err)
	}
	if err := scratchpad.ValidateContent(record.Content); err != nil {
		return scratchpad.Record{}, fmt.Errorf("invalid persisted scratchpad content: %w", err)
	}
	return record, nil
}

var _ scratchpad.Repository = (*Store)(nil)
