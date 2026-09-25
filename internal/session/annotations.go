package session

import (
	"context"

	kitannotation "github.com/akonwi/kit/internal/annotation"
)

// AnnotationCreated publishes the committed record while its annotation
// mutation lock is still held.
func (m *Manager) AnnotationCreated(record kitannotation.Record) {
	m.publishAnnotationEvent(NewEvent{SessionID: record.SessionID, Kind: EventAnnotationCreated, AnnotationID: record.ID, Annotation: &record})
}

// AnnotationUpdated publishes the committed record while its annotation
// mutation lock is still held.
func (m *Manager) AnnotationUpdated(record kitannotation.Record) {
	m.publishAnnotationEvent(NewEvent{SessionID: record.SessionID, Kind: EventAnnotationUpdated, AnnotationID: record.ID, Annotation: &record})
}

// AnnotationDeleted publishes deletion while its annotation mutation lock is held.
func (m *Manager) AnnotationDeleted(sessionID string, annotationID uint64) {
	m.publishAnnotationEvent(NewEvent{SessionID: sessionID, Kind: EventAnnotationDeleted, AnnotationID: annotationID})
}

// AnnotationsSubmitted publishes one ordered consumption event tied to the
// accepted message. Immutable message snapshots retain the submitted records.
func (m *Manager) AnnotationsSubmitted(sessionID, messageID string, annotationIDs []uint64) {
	if len(annotationIDs) == 0 {
		return
	}
	m.publishAnnotationEvent(NewEvent{
		SessionID: sessionID, Kind: EventAnnotationSubmitted,
		AnnotationIDs: append([]uint64(nil), annotationIDs...), AcceptedMessageID: messageID,
	})
}

// ReconcileAnnotationSubmissions resolves crash-interrupted cross-store
// reservations against the authoritative droid history.
func (m *Manager) ReconcileAnnotationSubmissions(ctx context.Context, sessionID string) error {
	if m.annotations == nil {
		return nil
	}
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return err
	}
	return m.annotations.RecoverSubmissions(ctx, sessionID, loaded.droid.HasAnnotationSubmission)
}

func (m *Manager) publishAnnotationEvent(event NewEvent) {
	if m == nil {
		return
	}
	if err := m.beginOperation(); err != nil {
		return
	}
	defer m.ops.Done()
	m.mu.Lock()
	loaded := m.runtimes[event.SessionID]
	deleting := m.deleting[event.SessionID]
	m.mu.Unlock()
	if loaded == nil || deleting {
		return
	}
	if err := loaded.events.append([]NewEvent{event}); err != nil {
		loaded.events.invalidate()
	}
}

// AnnotationChanged remains a small compatibility seam for submitted steering
// paths that already hold authoritative IDs.
func (m *Manager) AnnotationChanged(_ context.Context, sessionID string, kind EventKind, annotationID uint64) {
	if kind == EventAnnotationDeleted {
		m.AnnotationDeleted(sessionID, annotationID)
	}
}
