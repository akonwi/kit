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

func TestSessionEventReplayNormalizesLegacyAssistantAndToolFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	created, err := store.CreateSession(ctx, NewSession{
		ID: "session-legacy-events", CWD: "/workspace", Persistent: true,
		ModelProvider: "test", ModelID: "echo",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	started, err := store.AppendSessionEvents(ctx, []kitsession.NewEvent{{
		SessionID: created.ID, TurnID: "turn-1", RunID: "run-1",
		Kind: kitsession.EventRunStarted, Status: kitsession.RunStatusRunning,
	}})
	if err != nil {
		t.Fatalf("AppendSessionEvents() error = %v", err)
	}
	legacy := []struct {
		sequence int
		kind     kitsession.EventKind
		payload  string
	}{
		{2, kitsession.EventAssistantStarted, `{"turnId":"turn-1","runId":"run-1"}`},
		{3, kitsession.EventToolPlanned, `{"turnId":"turn-1","runId":"run-1","toolCallId":"call-1","toolName":"read"}`},
		{4, kitsession.EventAssistantCompleted, `{"turnId":"turn-1","runId":"run-1"}`},
		{5, kitsession.EventToolStarted, `{"turnId":"turn-1","runId":"run-1","toolCallId":"call-1","toolName":"read"}`},
		{6, kitsession.EventToolCompleted, `{"turnId":"turn-1","runId":"run-1","toolCallId":"call-1","toolName":"read","text":"contents"}`},
	}
	for _, event := range legacy {
		if _, err := store.db.ExecContext(ctx, `
			INSERT INTO session_events(session_id, stream_id, sequence, kind, payload_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, created.ID, started[0].StreamID, event.sequence, event.kind, event.payload, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatalf("insert legacy event %d: %v", event.sequence, err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE session_streams SET next_sequence = 7 WHERE session_id = ?`, created.ID); err != nil {
		t.Fatalf("advance legacy stream: %v", err)
	}

	page, err := store.ListSessionEvents(ctx, created.ID, 0, 32)
	if err != nil {
		t.Fatalf("ListSessionEvents() legacy replay error = %v", err)
	}
	if len(page.Events) != 6 {
		t.Fatalf("legacy replay events = %+v", page.Events)
	}
	for _, index := range []int{1, 2, 3} {
		if page.Events[index].MessageID != "legacy-assistant:run-1" {
			t.Errorf("legacy event %d message id = %q", index, page.Events[index].MessageID)
		}
	}
	for _, index := range []int{2, 4} {
		if !page.Events[index].ArgumentsTruncated || page.Events[index].Arguments != "" {
			t.Errorf("legacy tool event %d arguments = %q truncated %v", index, page.Events[index].Arguments, page.Events[index].ArgumentsTruncated)
		}
	}
	if completed := page.Events[5]; completed.Text != "" || len(completed.Content) != 1 || completed.Content[0].Text != "contents" {
		t.Fatalf("legacy tool completion = %+v", completed)
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
