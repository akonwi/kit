package droids_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
)

// batchReadProviders answers the first request with two read_fixture calls in
// one step and every later request with text.
type batchReadProviders struct {
	mu       sync.Mutex
	requests []droids.Request
}

func (p *batchReadProviders) ID() string             { return "test" }
func (p *batchReadProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *batchReadProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *batchReadProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, errors.New("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (p *batchReadProviders) Model(id string) (droids.Model, bool) {
	if id == "test/read" || id == "read" {
		return p.model(), true
	}
	return droids.Model{}, false
}
func (p *batchReadProviders) RefreshModels(context.Context) error { return nil }
func (p *batchReadProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	call := len(p.requests)
	p.mu.Unlock()
	if call == 1 {
		return sdkStaticStream(droids.AssistantMessage{
			Provider: "test", Model: "read", StopReason: droids.StopReasonToolUse,
			Content: []droids.AssistantContent{
				droids.ToolCall{ID: "call_first", Name: "read_fixture", Arguments: []byte(`{"path":"a.txt"}`)},
				droids.ToolCall{ID: "call_second", Name: "read_fixture", Arguments: []byte(`{"path":"b.txt"}`)},
			},
		})
	}
	return sdkStaticStream(droids.AssistantMessage{
		Provider: "test", Model: "read", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "Both read."}},
	})
}
func (p *batchReadProviders) model() droids.Model {
	return droids.Model{
		ID: "read", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
	}
}

// A before hook may park each call of a step in turn: the droid shuts down
// while the second call's hook is pending after the first call completed.
// Resuming must still deliver both results to the model and settle the turn.
func TestSDKResumeKeepsCallsCompletedBeforeALaterHookParked(t *testing.T) {
	store := droids.NewMemoryStore()
	providers := &batchReadProviders{}
	var toolRuns atomic.Int32

	// spawn opens the conversation with a hook that parks the calls park
	// selects and lets the rest proceed.
	spawn := func(park func(droids.ToolCallID) bool) (*droids.Droid, <-chan droids.ToolCallID) {
		parked := make(chan droids.ToolCallID, 1)
		droid, err := droids.Spawn(t.Context(), "conversation_batch_hook", droids.Config{
			Store: store, Model: resolvedTestModel(providers, "test/read"),
			Tools:     []droids.AnyTool{readOnlyTool(&toolRuns)},
			Execution: &droids.ExecutionPolicy{ToolExecution: droids.ModeSequential},
			BeforeToolCall: func(ctx context.Context, call droids.ToolContext, _ droids.ToolCall) (droids.BeforeToolResult, error) {
				if !park(call.ToolCallID) {
					return droids.BeforeToolResult{}, nil
				}
				parked <- call.ToolCallID
				<-ctx.Done()
				return droids.BeforeToolResult{}, ctx.Err()
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return droid, parked
	}
	waitParked := func(parked <-chan droids.ToolCallID) droids.ToolCallID {
		t.Helper()
		select {
		case id := <-parked:
			return id
		case <-time.After(5 * time.Second):
			t.Fatal("before hook did not park")
			return ""
		}
	}

	// Run 1 parks the first call. Droids assigns call ids, so the test learns
	// them from the parked hooks.
	first, firstParked := spawn(func(droids.ToolCallID) bool { return true })
	if _, err := first.Prompt(t.Context(), droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: "Read both fixtures."}},
	}, droids.PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	firstID := waitParked(firstParked)
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Run 2 lets the first call run and parks the second.
	second, secondParked := spawn(func(id droids.ToolCallID) bool { return id != firstID })
	if err := second.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	secondID := waitParked(secondParked)
	if err := second.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := toolRuns.Load(); got != 1 {
		t.Fatalf("tool runs after the second run = %d, want 1", got)
	}

	// Run 3 allows everything.
	third, _ := spawn(func(droids.ToolCallID) bool { return false })
	t.Cleanup(func() { _ = third.Close() })
	if err := third.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	state, err := third.WaitQuiescent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Kind != droids.QuiescentSettled || state.Execution == nil || state.Execution.Status != droids.ExecutionCompleted {
		t.Fatalf("quiescent state = %+v, want a completed turn", state)
	}
	if got := toolRuns.Load(); got != 2 {
		t.Fatalf("tool runs = %d, want each call run once", got)
	}

	providers.mu.Lock()
	defer providers.mu.Unlock()
	if len(providers.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(providers.requests))
	}
	results := map[droids.ToolCallID]bool{}
	for _, message := range providers.requests[1].Messages {
		if result, ok := message.(droids.ToolResultMessage); ok {
			results[result.ToolCallID] = true
		}
	}
	if !results[firstID] || !results[secondID] || len(results) != 2 {
		t.Fatalf("continuation results = %v, want both %s and %s", results, firstID, secondID)
	}
}
