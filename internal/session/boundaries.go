package session

import (
	"context"
	"time"

	"github.com/akonwi/kit/droids"
)

// informBoundary durably records message in droid's conversation and, once it
// is recorded, announces it to live clients as a boundary.created event.
// Callers hold the runtime's mu, so a snapshot or a turn's stream reset cannot
// fall between the recording and the announcement. To a
// client a boundary is a context record from the moment it is created; whether
// the model has seen it yet is not part of the record.
//
// A redelivered boundary whose receipts droid already holds is not announced
// again. Clients still reconcile records by boundary ID, because a snapshot
// can list a record whose announcement follows it.
func informBoundary(ctx context.Context, droid *droids.Droid, events *eventLog, sessionID string, message droids.BoundaryMessage) error {
	redelivered := boundaryReceived(ctx, droid, message)
	err := droid.Inform(ctx, message)
	// An Inform that reports failure may still have recorded the boundary;
	// callers reconcile that through its receipts, and so does the announcement.
	if !redelivered && (err == nil || boundaryReceived(context.WithoutCancel(ctx), droid, message)) {
		publishBoundary(events, sessionID, message, time.Now())
	}
	return err
}

func boundaryReceived(ctx context.Context, droid *droids.Droid, message droids.BoundaryMessage) bool {
	receipts := message.ReceiptIDs
	if len(receipts) == 0 {
		receipts = []string{message.ID}
	}
	for _, receipt := range receipts {
		if receipt == "" {
			return false
		}
		received, err := droid.BoundaryReceived(ctx, receipt)
		if err != nil || !received {
			return false
		}
	}
	return true
}

// publishBoundary appends the boundary.created event for message. A record
// that cannot be announced invalidates the stream so clients resynchronize
// from a snapshot, which lists it.
func publishBoundary(events *eventLog, sessionID string, message droids.BoundaryMessage, createdAt time.Time) {
	if events == nil || message.ID == "" {
		return
	}
	event, err := boundaryCreatedEvent(sessionID, message, createdAt)
	if err == nil {
		err = events.append([]NewEvent{event})
	}
	if err != nil {
		events.invalidate()
	}
}

func boundaryCreatedEvent(sessionID string, message droids.BoundaryMessage, createdAt time.Time) (NewEvent, error) {
	record, err := boundaryRecord(message, createdAt)
	if err != nil {
		return NewEvent{}, err
	}
	event := NewEvent{
		SessionID: sessionID, Kind: EventBoundaryCreated,
		BoundaryID: record.BoundaryID, BoundaryKind: record.BoundaryKind, BoundarySource: record.BoundarySource,
		CreatedAt: record.CreatedAt,
	}
	event.Content, event.ContentTruncated = boundLiveContent(record.Content)
	event.Details, event.DetailsOmitted = boundedLiveToolDetails(record.Details)
	return event, nil
}
