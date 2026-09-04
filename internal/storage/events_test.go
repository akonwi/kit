package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	kitsession "github.com/akonwi/kit/internal/session"
)

func TestSessionEventRetentionReportsAnExpiredCursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	created, err := store.CreateSession(ctx, NewSession{
		ID: "session-retention", CWD: "/workspace", Persistent: true,
		ModelProvider: "test", ModelID: "echo",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	events := make([]kitsession.NewEvent, retainedSessionEvents+2)
	for index := range events {
		events[index] = kitsession.NewEvent{
			SessionID: created.ID, TurnID: "turn-1", RunID: "run-1",
			Kind: kitsession.EventUserMessage, Text: fmt.Sprintf("message %d", index),
		}
	}
	if _, err := store.AppendSessionEvents(ctx, events); err != nil {
		t.Fatalf("AppendSessionEvents() error = %v", err)
	}
	page, err := store.ListSessionEvents(ctx, created.ID, 1, 32)
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if !page.ResyncRequired || page.FirstSequence != 3 || page.LastSequence != int64(len(events)) || len(page.Events) != 0 {
		t.Fatalf("expired cursor page = %+v", page)
	}
}

func TestSessionEventsReceiveContiguousDurableSequences(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	created, err := store.CreateSession(ctx, NewSession{
		ID: "session-events", CWD: "/workspace", Persistent: true,
		ModelProvider: "test", ModelID: "echo",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	first, err := store.AppendSessionEvents(ctx, []kitsession.NewEvent{
		{SessionID: created.ID, TurnID: "turn-1", RunID: "run-1", Kind: kitsession.EventRunStarted, Status: kitsession.RunStatusRunning},
		{SessionID: created.ID, TurnID: "turn-1", RunID: "run-1", Kind: kitsession.EventUserMessage, Text: "hello"},
	})
	if err != nil {
		t.Fatalf("first AppendSessionEvents() error = %v", err)
	}
	second, err := store.AppendSessionEvents(ctx, []kitsession.NewEvent{
		{SessionID: created.ID, TurnID: "turn-1", RunID: "run-1", MessageID: "message-1", Kind: kitsession.EventAssistantTextDelta, ContentIndex: 0, Delta: "hi"},
		{
			SessionID: created.ID, TurnID: "turn-1", RunID: "run-1",
			Kind: kitsession.EventToolCompleted, ToolCallID: "call-1", ToolName: "read",
			Content: []kitsession.TranscriptContent{{Kind: kitsession.TranscriptContentText, Text: "contents"}},
			Details: json.RawMessage(`{"lines":1}`),
		},
	})
	if err != nil {
		t.Fatalf("second AppendSessionEvents() error = %v", err)
	}
	if first[0].StreamID == "" || first[0].StreamID != second[0].StreamID {
		t.Fatalf("stream ids = %q and %q", first[0].StreamID, second[0].StreamID)
	}
	if first[0].Sequence != 1 || first[1].Sequence != 2 || second[0].Sequence != 3 || second[1].Sequence != 4 {
		t.Fatalf("event sequences = %d, %d, %d, %d", first[0].Sequence, first[1].Sequence, second[0].Sequence, second[1].Sequence)
	}

	page, err := store.ListSessionEvents(ctx, created.ID, 1, 32)
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if page.StreamID != first[0].StreamID || page.FirstSequence != 1 || page.LastSequence != 4 || len(page.Events) != 3 {
		t.Fatalf("event page = %+v", page)
	}
	if page.Events[0].Kind != kitsession.EventUserMessage || page.Events[0].Text != "hello" || page.Events[1].MessageID != "message-1" || page.Events[1].Delta != "hi" {
		t.Fatalf("event page content = %+v", page.Events)
	}
	completed := page.Events[2]
	if completed.Kind != kitsession.EventToolCompleted || len(completed.Content) != 1 || completed.Content[0].Text != "contents" || string(completed.Details) != `{"lines":1}` {
		t.Fatalf("stored tool completion = %+v", completed)
	}
}
