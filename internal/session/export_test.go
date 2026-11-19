package session

import (
	"context"
	"testing"
)

// HoldQueueTurnFollower makes sessionID's queue follower look busy until the
// returned function releases it, as a follower still adopting a turn does.
func HoldQueueTurnFollower(t testing.TB, m *Manager, sessionID string) func() {
	t.Helper()
	loaded, err := m.runtime(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	feed := loaded.queueTurns
	feed.mu.Lock()
	feed.draining = true
	feed.mu.Unlock()
	return func() {
		feed.mu.Lock()
		feed.draining = false
		feed.signalCaughtUpLocked()
		feed.mu.Unlock()
	}
}
