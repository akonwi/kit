package session

import (
	"context"
	"errors"
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

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
