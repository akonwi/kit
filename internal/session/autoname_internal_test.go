package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
)

type autoNameTestStream struct{ message droids.AssistantMessage }

func (s autoNameTestStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 2)
	events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: "test", Model: "echo"}}
	events <- droids.StreamDone{Message: s.message}
	close(events)
	return events
}
func (s autoNameTestStream) Result() droids.AssistantMessage { return s.message }

// TestAutoNameWaitsForTheForkedBoundaryToMeetTheThreshold covers a parent
// whose active turn makes it look eligible while its last settled boundary,
// which the naming fork copies, has only one user turn.
func TestAutoNameWaitsForTheForkedBoundaryToMeetTheThreshold(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	baseModel := droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
	model, err := droids.BindModel(droids.AdaptProvider("test", []droids.Model{baseModel}, func(ctx context.Context, _ droids.Model, _ droids.Request) droids.Stream {
		if calls.Add(1) == 2 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return autoNameTestStream{message: droids.AssistantMessage{
			Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
			Content: []droids.AssistantContent{droids.TextContent{Text: "reply"}},
		}}
	}), baseModel)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := droids.Spawn(t.Context(), "conversation_autoname_parent", droids.Config{Model: model})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	prompt := func(text string) droids.ExecutionHandle {
		handle, err := parent.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: text}}}, droids.PromptOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return handle
	}
	if _, err := prompt("inspect the parser").Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	active := prompt("then add a test")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("second turn did not start")
	}
	defer func() {
		close(release)
		_, _ = active.Wait(context.Background())
	}()
	if turns, ready, err := autoNameEligibility(t.Context(), parent); err != nil || !ready || turns != 2 {
		t.Fatalf("parent eligibility = %d, %t, %v; want the active turn counted", turns, ready, err)
	}

	manager := &Manager{mailboxContext: t.Context()}
	if err := manager.nameFromMemoryFork("session_autoname", parent); !errors.Is(err, errAutoNameNotReady) {
		t.Fatalf("nameFromMemoryFork() error = %v, want errAutoNameNotReady", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want no naming request", got)
	}
}
