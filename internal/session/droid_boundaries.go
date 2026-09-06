package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/akonwi/kit/internal/droids"
)

// boundaryTracker tracks which Kit-owned bash transcript records have been
// delivered through the droid boundary API. Droids owns agent persistence in
// its dedicated Store; this type is not an agent Storage implementation.
type boundaryTracker struct {
	store     Repository
	sessionID string

	mu              sync.Mutex
	contextSequence int64
}

func newBoundaryTracker(store Repository, sessionID string) *boundaryTracker {
	return &boundaryTracker{store: store, sessionID: sessionID, contextSequence: -1}
}

type sequencedBoundary struct {
	sequence int64
	message  droids.BoundaryMessage
}

func (s *boundaryTracker) bashBoundaries(ctx context.Context, after, through int64) ([]sequencedBoundary, error) {
	records, err := s.store.ListMessages(ctx, s.sessionID)
	if err != nil {
		return nil, err
	}
	boundaries := make([]sequencedBoundary, 0)
	for _, record := range records {
		if record.Role != "bash" || record.Sequence <= after || (through >= 0 && record.Sequence > through) {
			continue
		}
		execution, err := decodeBashExecution(record)
		if err != nil {
			return nil, fmt.Errorf("decode message %q: %w", record.ID, err)
		}
		if execution.Status == BashExecutionRunning || execution.Status == BashExecutionInterrupted || execution.ExcludeFromContext {
			continue
		}
		message := bashContextMessage(execution)
		boundaries = append(boundaries, sequencedBoundary{
			sequence: record.Sequence,
			message: droids.BoundaryMessage{
				ID: execution.ID, Kind: "bash", Source: "composer", Content: message.Content,
			},
		})
	}
	return boundaries, nil
}

func (s *boundaryTracker) markBashContextSequence(sequence int64) {
	s.mu.Lock()
	s.contextSequence = max(s.contextSequence, sequence)
	s.mu.Unlock()
}

func (s *boundaryTracker) bashContextSequence() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contextSequence
}
