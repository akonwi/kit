package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestProjectDroidMessagesRepairsTerminalTurnIdempotently(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.CreateSession(ctx, NewSession{
		ID: "session", CWD: "/workspace", Persistent: true,
		ModelProvider: "test", ModelID: "echo",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReserveParentRun(ctx, "session", "turn", "run"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.StartReservedParentRun(ctx, "session", "run", "droid-turn"); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishParentRun(ctx, "session", "turn", "run", RunStatusInterrupted, "restart"); err != nil {
		t.Fatal(err)
	}
	message := NewMessageRecord{ID: "message_0123456789abcdef0123456789abcdef", Role: "user", PayloadJSON: []byte(`{"version":1}`)}
	for range 2 {
		if _, err := store.ProjectDroidMessages(ctx, "session", "turn", []NewMessageRecord{message}); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := store.ListMessages(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != message.ID || messages[0].TurnID != "turn" {
		t.Fatalf("projected messages = %+v", messages)
	}
	runs, err := store.ListParentRuns(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].DroidTurnID != "droid-turn" {
		t.Fatalf("parent run projections = %+v", runs)
	}
}
