package session_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func TestMessagePageFiltersRolesAcrossTranscriptPages(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	for turn := range 3 {
		if _, err := manager.RunPrompt(t.Context(), created.ID, fmt.Sprintf("turn %d", turn)); err != nil {
			t.Fatal(err)
		}
	}
	users, err := manager.MessagePage(t.Context(), created.ID, session.MessagePageQuery{Limit: 2, Roles: []string{"user"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(users.Messages) != 2 || !users.HasMore || users.NextCursor == 0 || users.Messages[0].Content[0].Text != "turn 2" || users.Messages[1].Content[0].Text != "turn 1" {
		t.Fatalf("user page = %+v", users)
	}
	if users.NextCursor != uint64(users.Messages[1].Sequence) {
		t.Fatalf("user page cursor = %d, oldest sequence = %d", users.NextCursor, users.Messages[1].Sequence)
	}
	older, err := manager.MessagePage(t.Context(), created.ID, session.MessagePageQuery{Before: users.NextCursor, Limit: 10, Roles: []string{"user"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Messages) != 1 || older.HasMore || older.NextCursor != 0 || older.Messages[0].Content[0].Text != "turn 0" {
		t.Fatalf("older user page = %+v", older)
	}
}

func TestSnapshotBoundsTranscriptAndPagesOlderCompleteTurns(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(
		store, &authorityProviders{}, staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	for turn := range 30 {
		if _, err := manager.RunPrompt(t.Context(), created.ID, fmt.Sprintf("turn %d", turn)); err != nil {
			t.Fatalf("RunPrompt(%d): %v", turn, err)
		}
	}

	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Messages) != 50 || !snapshot.HasMoreMessages || snapshot.PreviousMessageCursor == 0 {
		t.Fatalf("snapshot page = len %d cursor %d more %t", len(snapshot.Messages), snapshot.PreviousMessageCursor, snapshot.HasMoreMessages)
	}
	if snapshot.Messages[0].TurnID != snapshot.Messages[1].TurnID || snapshot.Messages[0].Role != "user" {
		t.Fatalf("snapshot starts with a split turn: %+v", snapshot.Messages[:2])
	}

	older, err := manager.TranscriptPage(t.Context(), created.ID, snapshot.PreviousMessageCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Messages) != 10 || older.HasMoreMessages || older.PreviousMessageCursor != 0 {
		t.Fatalf("older page = len %d cursor %d more %t", len(older.Messages), older.PreviousMessageCursor, older.HasMoreMessages)
	}
	if older.Messages[len(older.Messages)-1].Sequence >= snapshot.Messages[0].Sequence {
		t.Fatalf("older page overlaps snapshot: older sequence %d snapshot sequence %d", older.Messages[len(older.Messages)-1].Sequence, snapshot.Messages[0].Sequence)
	}
	if _, err := manager.TranscriptPage(t.Context(), created.ID, uint64(snapshot.Messages[1].Sequence)); !errors.Is(err, session.ErrTranscriptCursorUnavailable) {
		t.Fatalf("middle-of-turn TranscriptPage cursor error = %v", err)
	}
	if _, err := manager.TranscriptPage(t.Context(), created.ID, uint64(snapshot.Messages[len(snapshot.Messages)-1].Sequence)+1000); !errors.Is(err, session.ErrTranscriptCursorUnavailable) {
		t.Fatalf("stale TranscriptPage cursor error = %v", err)
	}
}

// TestSnapshotKeepsBashHistoryOutOfPaginatedTranscript pins that durable direct
// shell work never enters the paginated transcript. Bash executions carry
// sequences from a space separate from droid history, and the first execution in
// a session is sequence 0, so merging them reordered the page's first message and
// broke the previous-message cursor invariant on every paginated snapshot.
func TestSnapshotKeepsBashHistoryOutOfPaginatedTranscript(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(
		store, &authorityProviders{}, staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	for turn := range 30 {
		if _, err := manager.RunPrompt(t.Context(), created.ID, fmt.Sprintf("turn %d", turn)); err != nil {
			t.Fatalf("RunPrompt(%d): %v", turn, err)
		}
	}
	// An excluded execution never reaches droid history, so it exists only in
	// session history. It is also the one whose sequence would sort first.
	execution, err := manager.StartBash(t.Context(), created.ID, "bash_11111111111111111111111111111111", "printf hi", true)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for execution.Status == session.BashExecutionRunning {
		execution, err = manager.GetBash(t.Context(), created.ID, execution.ID)
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("bash %s did not settle", execution.ID)
		}
		time.Sleep(10 * time.Millisecond)
	}

	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.HasMoreMessages || snapshot.PreviousMessageCursor == 0 {
		t.Fatalf("snapshot page = len %d cursor %d more %t", len(snapshot.Messages), snapshot.PreviousMessageCursor, snapshot.HasMoreMessages)
	}
	if len(snapshot.Messages) != 50 {
		t.Fatalf("snapshot page = %d messages, want 50 transcript messages", len(snapshot.Messages))
	}
	if snapshot.PreviousMessageCursor != uint64(snapshot.Messages[0].Sequence) {
		t.Fatalf("cursor %d does not match first message sequence %d", snapshot.PreviousMessageCursor, snapshot.Messages[0].Sequence)
	}
	for _, message := range snapshot.Messages {
		if message.Role == "bash" {
			t.Fatalf("bash execution %q entered the transcript", message.ID)
		}
	}
	// The execution remains durable and recallable through its own projection.
	history, err := manager.BashHistory(t.Context(), created.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Entries) != 1 || history.Entries[0].ID != execution.ID || !history.Entries[0].ExcludeFromContext {
		t.Fatalf("bash history = %+v", history.Entries)
	}
	// The page still reads older messages from the same cursor.
	older, err := manager.TranscriptPage(t.Context(), created.ID, snapshot.PreviousMessageCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Messages) == 0 || older.HasMoreMessages || older.PreviousMessageCursor != 0 {
		t.Fatalf("older page = len %d cursor %d more %t", len(older.Messages), older.PreviousMessageCursor, older.HasMoreMessages)
	}
	if older.Messages[len(older.Messages)-1].Sequence >= snapshot.Messages[0].Sequence {
		t.Fatalf("older page overlaps snapshot: older %d first %d", older.Messages[len(older.Messages)-1].Sequence, snapshot.Messages[0].Sequence)
	}
}
