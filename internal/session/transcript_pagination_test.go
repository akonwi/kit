package session_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

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
