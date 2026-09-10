package session

import (
	"errors"
	"testing"
	"time"
)

func TestManagerRenameRejectsTemporaryDisposalAndKeepsActivityMonotonic(t *testing.T) {
	t.Parallel()

	const sessionID = "session_0123456789abcdef0123456789abcdef"
	future := time.Now().UTC().Add(time.Hour)
	manager := &Manager{
		deleting:  make(map[string]bool),
		temporary: map[string]SessionRecord{sessionID: {ID: sessionID, Name: "Before", UpdatedAt: future}},
	}
	renamed, err := manager.Rename(t.Context(), sessionID, "After")
	if err != nil || renamed.Name != "After" || !renamed.UpdatedAt.After(future) {
		t.Fatalf("Rename(temporary) = %+v, %v", renamed, err)
	}
	manager.deleting[sessionID] = true
	if _, err := manager.Rename(t.Context(), sessionID, "Too late"); !errors.Is(err, ErrDeleteBusy) {
		t.Fatalf("Rename(disposed temporary) error = %v, want ErrDeleteBusy", err)
	}
}
