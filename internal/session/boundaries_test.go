package session_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

// A boundary is a transcript record from the moment it is created. A client
// that attaches during a turn sees the boundaries that joined the turn at its
// start in the snapshot, and the turn's live stream announces those created
// during it, so each record is listed exactly once.
func TestBoundariesAreRecordsFromCreationAcrossARunningTurn(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	const before, during = "bash_11111111111111111111111111111111", "bash_22222222222222222222222222222222"

	idle, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	runBash(t, manager, record.ID, before, "printf first")
	created, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := boundaryRecordIDs(created); !reflectStringsEqual(got, []string{before}) {
		t.Fatalf("records before the turn = %v", got)
	}
	if got := announcedBoundaries(t, manager, record.ID, idle.EventStreamID, idle.EventCursor); !reflectStringsEqual(got, []string{before}) {
		t.Fatalf("announced before the turn = %v", got)
	}

	done := make(chan error, 1)
	go func() {
		_, err := manager.RunPrompt(context.Background(), record.ID, "blocked")
		done <- err
	}()
	<-providers.started
	runBash(t, manager, record.ID, during, "printf second")
	running, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.ActiveRunID == "" || !running.EventReplayAvailable {
		t.Fatalf("snapshot during the turn = active %q, replay %v", running.ActiveRunID, running.EventReplayAvailable)
	}
	if got := boundaryRecordIDs(running); !reflectStringsEqual(got, []string{before}) {
		t.Fatalf("records listed during the turn = %v, want only the one that joined at its start", got)
	}
	if got := announcedBoundaries(t, manager, record.ID, running.EventStreamID, running.EventReplayFrom); !reflectStringsEqual(got, []string{during}) {
		t.Fatalf("announced during the turn = %v", got)
	}

	close(providers.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	settled, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, message := range settled.Messages {
		if message.Role == "context" {
			counts[message.BoundaryID]++
		}
		if message.BoundaryID == before && message.TurnID == "" {
			t.Fatalf("boundary that joined the completed turn is listed without it: %+v", message)
		}
	}
	if counts[before] != 1 || counts[during] != 1 {
		t.Fatalf("records after the turn = %v", counts)
	}
}

func runBash(t *testing.T, manager *session.Manager, sessionID, executionID, command string) {
	t.Helper()
	if _, err := manager.StartBash(t.Context(), sessionID, executionID, command, false); err != nil {
		t.Fatal(err)
	}
	if settled, err := waitForBash(t, manager, sessionID, executionID); err != nil || settled.Status != session.BashExecutionCompleted {
		t.Fatalf("bash %s = %+v, %v", executionID, settled, err)
	}
	// GetBash reports the settled execution only after its boundary is recorded.
}

func boundaryRecordIDs(snapshot session.Snapshot) []string {
	var ids []string
	for _, record := range boundaryRecords(snapshot) {
		ids = append(ids, record.BoundaryID)
	}
	return ids
}

func announcedBoundaries(t *testing.T, manager *session.Manager, sessionID, streamID string, after int64) []string {
	t.Helper()
	page, err := manager.Events(t.Context(), sessionID, streamID, after)
	if err != nil {
		t.Fatal(err)
	}
	if page.ResyncRequired {
		t.Fatal("event page requires resynchronization")
	}
	var ids []string
	for _, event := range page.Events {
		if event.Kind == session.EventBoundaryCreated {
			ids = append(ids, event.BoundaryID)
		}
	}
	return ids
}
