package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/scratchpad"
)

// Scratchpad reads the authoritative shared scratchpad through one bound
// persistent session identity.
func (m *Manager) Scratchpad(ctx context.Context, sessionID string) (scratchpad.Record, error) {
	if err := m.beginOperation(); err != nil {
		return scratchpad.Record{}, err
	}
	defer m.ops.Done()
	if err := m.requireScratchpadSession(ctx, sessionID); err != nil {
		return scratchpad.Record{}, err
	}
	record, err := m.scratchpads.Get(ctx, sessionID)
	if err != nil {
		return scratchpad.Record{}, normalizeScratchpadReadError(err)
	}
	return record, nil
}

// UpdateScratchpad applies one revision-guarded content replacement and
// publishes committed changes to every currently loaded family runtime.
func (m *Manager) UpdateScratchpad(ctx context.Context, sessionID string, expectedRevision int64, content string) (scratchpad.Record, error) {
	if err := m.beginOperation(); err != nil {
		return scratchpad.Record{}, err
	}
	defer m.ops.Done()
	if err := m.requireScratchpadSession(ctx, sessionID); err != nil {
		return scratchpad.Record{}, err
	}
	record, err := m.scratchpads.Update(ctx, sessionID, expectedRevision, content)
	if err != nil {
		return scratchpad.Record{}, err
	}
	if record.Revision == expectedRevision {
		return record, nil
	}
	m.publishScratchpadChanged(record)
	return record, nil
}

func normalizeScratchpadReadError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, scratchpad.ErrMigrationRequired):
		return err
	default:
		return scratchpad.ErrUnavailable
	}
}

// EditScratchpad applies exact replacements to the latest committed shared
// scratchpad and publishes a change only when content changed.
func (m *Manager) EditScratchpad(ctx context.Context, sessionID string, edits []scratchpad.Edit) (scratchpad.Record, int, error) {
	if err := m.beginOperation(); err != nil {
		return scratchpad.Record{}, 0, err
	}
	defer m.ops.Done()
	if err := m.requireScratchpadSession(ctx, sessionID); err != nil {
		return scratchpad.Record{}, 0, err
	}
	record, applied, changed, err := m.scratchpads.Edit(ctx, sessionID, edits)
	if err != nil {
		return scratchpad.Record{}, 0, err
	}
	if changed {
		m.publishScratchpadChanged(record)
	}
	return record, applied, nil
}

func (m *Manager) requireScratchpadSession(ctx context.Context, sessionID string) error {
	if m.scratchpads == nil {
		return scratchpad.ErrUnavailable
	}
	if !identifier.Valid(sessionID, "session_") {
		return fmt.Errorf("%w: invalid session id", ErrInvalidInput)
	}
	m.mu.Lock()
	deleting := m.deleting[sessionID]
	m.mu.Unlock()
	if deleting {
		return ErrDeleteBusy
	}
	record, err := m.sessionRecord(ctx, sessionID)
	if err != nil {
		return err
	}
	if record.ArchivedAt != nil {
		return fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	if !record.Persistent {
		return scratchpad.ErrUnsupported
	}
	if record.ScratchpadOwnerID == "" {
		return fmt.Errorf("%w: session has no scratchpad owner", scratchpad.ErrUnavailable)
	}
	return nil
}

func (m *Manager) publishScratchpadChanged(record scratchpad.Record) {
	type target struct {
		sessionID string
		runtime   *runtime
	}
	m.mu.Lock()
	targets := make([]target, 0)
	for sessionID, loaded := range m.runtimes {
		if loaded.scratchpadOwnerID == record.OwnerSessionID {
			targets = append(targets, target{sessionID: sessionID, runtime: loaded})
		}
	}
	m.mu.Unlock()

	for _, target := range targets {
		copy := record
		if err := target.runtime.events.append([]NewEvent{{
			SessionID:  target.sessionID,
			Kind:       EventScratchpadChanged,
			Scratchpad: &copy,
		}}); err != nil {
			target.runtime.events.invalidate()
			slog.Error("Could not publish committed scratchpad change", "session_id", target.sessionID, "owner_session_id", record.OwnerSessionID, "error", err)
		}
	}
}
