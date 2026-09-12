package session

import (
	"errors"
	"testing"
	"time"
)

func TestManagerSerializesLoadedRenameMutationAndBroadcast(t *testing.T) {
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	events, err := newEventLog()
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{
		deleting:  make(map[string]bool),
		temporary: map[string]SessionRecord{sessionID: {ID: sessionID, Name: "Before", UpdatedAt: time.Now().UTC()}},
		runtimes:  map[string]*runtime{sessionID: {events: events}},
	}
	events.mu.Lock()
	firstDone := make(chan error, 1)
	go func() {
		_, renameErr := manager.Rename(t.Context(), sessionID, "First")
		firstDone <- renameErr
	}()
	deadline := time.Now().Add(time.Second)
	for {
		manager.mu.Lock()
		name := manager.temporary[sessionID].Name
		manager.mu.Unlock()
		if name == "First" {
			break
		}
		if time.Now().After(deadline) {
			events.mu.Unlock()
			t.Fatal("first rename did not reach event publication")
		}
		time.Sleep(time.Millisecond)
	}
	secondDone := make(chan error, 1)
	go func() {
		_, renameErr := manager.Rename(t.Context(), sessionID, "Second")
		secondDone <- renameErr
	}()
	time.Sleep(10 * time.Millisecond)
	manager.mu.Lock()
	nameWhileBlocked := manager.temporary[sessionID].Name
	manager.mu.Unlock()
	if nameWhileBlocked != "First" {
		events.mu.Unlock()
		t.Fatalf("second rename overtook first publication: %q", nameWhileBlocked)
	}
	events.mu.Unlock()
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	page := events.page(events.streamID, 0)
	if len(page.Events) != 2 || page.Events[0].SessionName != "First" || page.Events[1].SessionName != "Second" {
		t.Fatalf("ordered rename events = %+v", page.Events)
	}
}

func TestManagerRenameRejectsTemporaryDisposalAndKeepsActivityMonotonic(t *testing.T) {
	t.Parallel()

	const sessionID = "session_0123456789abcdef0123456789abcdef"
	future := time.Now().UTC().Add(time.Hour)
	events, err := newEventLog()
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{
		deleting:  make(map[string]bool),
		temporary: map[string]SessionRecord{sessionID: {ID: sessionID, Name: "Before", UpdatedAt: future}},
		runtimes:  map[string]*runtime{sessionID: {events: events}},
	}
	renamed, err := manager.Rename(t.Context(), sessionID, "After")
	if err != nil || renamed.Name != "After" || !renamed.UpdatedAt.After(future) {
		t.Fatalf("Rename(temporary) = %+v, %v", renamed, err)
	}
	page := events.page(events.streamID, 0)
	if len(page.Events) != 1 || page.Events[0].Kind != EventSessionRenamed || page.Events[0].SessionName != "After" {
		t.Fatalf("rename events = %+v", page.Events)
	}
	manager.deleting[sessionID] = true
	if _, err := manager.Rename(t.Context(), sessionID, "Too late"); !errors.Is(err, ErrDeleteBusy) {
		t.Fatalf("Rename(disposed temporary) error = %v, want ErrDeleteBusy", err)
	}
}
