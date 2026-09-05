package session

import (
	"context"
	"errors"
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

func TestOrderReplayMessagesPlacesClaimedBashBeforeTargetTurn(t *testing.T) {
	t.Parallel()
	now := "2026-01-01T00:00:00Z"
	payload, err := encodePersistedBashExecution(persistedBashExecution{
		Version: 1, Type: "bash", Command: "pwd", CWD: "/workspace",
		Status: BashExecutionCompleted, StartedAt: now, CompletedAt: now,
		ContextBeforeTurnID: "turn_2",
	})
	if err != nil {
		t.Fatalf("encodePersistedBashExecution() error = %v", err)
	}
	records := []MessageRecord{
		{ID: "user-1", TurnID: "turn_1", Sequence: 0, Role: "user"},
		{ID: "assistant-1", TurnID: "turn_1", Sequence: 1, Role: "assistant"},
		{ID: "bash", Sequence: 2, Role: "bash", PayloadJSON: payload},
		{ID: "user-2", TurnID: "turn_2", Sequence: 3, Role: "user"},
		{ID: "assistant-2", TurnID: "turn_2", Sequence: 4, Role: "assistant"},
	}
	ordered := orderReplayMessages(records)
	want := []string{"user-1", "assistant-1", "bash", "user-2", "assistant-2"}
	for index, id := range want {
		if ordered[index].ID != id {
			t.Fatalf("ordered[%d] = %q, want %q", index, ordered[index].ID, id)
		}
	}
}

func TestOrderReplayMessagesDoesNotSplitParentTurn(t *testing.T) {
	t.Parallel()
	records := []MessageRecord{
		{ID: "assistant", TurnID: "turn_1", Sequence: 0, Role: "assistant"},
		{ID: "bash", Sequence: 1, Role: "bash"},
		{ID: "tool", TurnID: "turn_1", Sequence: 2, Role: "tool"},
		{ID: "user", TurnID: "turn_2", Sequence: 3, Role: "user"},
	}
	ordered := orderReplayMessages(records)
	want := []string{"assistant", "tool", "bash", "user"}
	for index, id := range want {
		if ordered[index].ID != id {
			t.Fatalf("ordered[%d] = %q, want %q", index, ordered[index].ID, id)
		}
	}
}

func TestDroidStorageStopsWritingAfterFirstAppendFailure(t *testing.T) {
	t.Parallel()

	repository := &failingAppendRepository{}
	adapter := newDroidStorage(repository, "session")
	if err := adapter.beginTurn("turn"); err != nil {
		t.Fatalf("beginTurn() error = %v", err)
	}
	message := droids.UserMessage{
		Content: []droids.Content{droids.TextContent{Text: "hello"}}, Timestamp: 1,
	}
	if err := adapter.Append(context.Background(), "session", message); err == nil {
		t.Fatal("first Append() succeeded")
	}
	if err := adapter.Append(context.Background(), "session", message); err == nil {
		t.Fatal("poisoned Append() succeeded")
	}
	if repository.appendCalls != 1 {
		t.Fatalf("repository append calls = %d, want 1", repository.appendCalls)
	}
	if err := adapter.endTurn(); err == nil {
		t.Fatal("endTurn() did not report persistence failure")
	}
}

type failingAppendRepository struct {
	Repository
	appendCalls int
}

func (r *failingAppendRepository) AppendMessages(
	context.Context,
	string,
	string,
	[]NewMessageRecord,
) ([]MessageRecord, error) {
	r.appendCalls++
	if r.appendCalls == 1 {
		return nil, errors.New("disk full")
	}
	return nil, nil
}
