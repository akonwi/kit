package droids

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// A tool that blocks until its context is cancelled. It signals when it starts
// (entered) and reports on outcome whether it observed cancellation.
func blockingTool(name string, entered chan struct{}, outcome chan bool) AnyTool {
	return NewTool(Tool[struct{}]{
		Name: name,
		Execute: func(ctx context.Context, _ struct{}) (ToolResult, error) {
			close(entered)
			select {
			case <-ctx.Done():
				outcome <- true
				return ToolResult{}, ctx.Err()
			case <-time.After(2 * time.Second):
				outcome <- false
				return ToolText("completed"), nil
			}
		},
	})
}

// Cancelling the ctx passed to Run must abort in-flight tool execution.
func TestRunContextCancelsToolExecution(t *testing.T) {
	entered := make(chan struct{})
	outcome := make(chan bool, 1)
	d, _ := New(Options{
		Providers: toolThenStop(t, "block"),
		Model:     "m",
		Tools:     []AnyTool{blockingTool("block", entered, outcome)},
	})
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, _ = d.Execute(ctx, "go") }()

	<-entered
	cancel()

	assertToolCancelled(t, outcome)
}

// Close must abort an in-flight run's tool execution.
func TestCloseAbortsActiveRun(t *testing.T) {
	entered := make(chan struct{})
	outcome := make(chan bool, 1)
	d, _ := New(Options{
		Providers: toolThenStop(t, "block"),
		Model:     "m",
		Tools:     []AnyTool{blockingTool("block", entered, outcome)},
	})

	go func() { _, _ = d.Execute(context.Background(), "go") }()

	<-entered
	d.Close()

	assertToolCancelled(t, outcome)
}

// Abort must abort an in-flight run's tool execution.
func TestAbortCancelsToolExecution(t *testing.T) {
	entered := make(chan struct{})
	outcome := make(chan bool, 1)
	d, _ := New(Options{
		Providers: toolThenStop(t, "block"),
		Model:     "m",
		Tools:     []AnyTool{blockingTool("block", entered, outcome)},
	})
	defer d.Close()

	go func() { _, _ = d.Execute(context.Background(), "go") }()

	<-entered
	d.Abort()

	assertToolCancelled(t, outcome)
}

// Enqueuing with an already-cancelled context fails fast.
func TestSendRejectsCancelledContext(t *testing.T) {
	d, _ := New(Options{Providers: toolThenStop(t, "noop"), Model: "m"})
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Send(ctx, "hi"); err == nil {
		t.Fatal("expected error enqueuing with cancelled context")
	}
}

// Cancelling Run's context preserves the error identity (errors.Is).
func TestRunCancelPreservesErrorIdentity(t *testing.T) {
	entered := make(chan struct{})
	outcome := make(chan bool, 1)
	d, _ := New(Options{
		Providers: toolThenStop(t, "block"),
		Model:     "m",
		Tools:     []AnyTool{blockingTool("block", entered, outcome)},
	})
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := d.Execute(ctx, "go"); errc <- err }()

	<-entered
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
	}
}

// abortOnCancelProvider streams nothing until its context is cancelled, then
// terminates with an aborted message — mimicking a real provider observing
// cancellation mid-stream. It signals when streaming begins.
type abortOnCancelProvider struct {
	id      string
	started chan struct{}
}

func (p abortOnCancelProvider) build() (providerEntry, error) {
	m := Model{ID: "m", Provider: "faux"}
	return providerEntry{
		id:     "faux",
		models: map[string]Model{m.ID: m},
		stream: func(ctx context.Context, model Model, _ Request, _ callOptions) Stream {
			s := newPipeStream()
			go func() {
				close(p.started)
				<-ctx.Done()
				s.final = AssistantMessage{
					Provider: "faux", Model: model.ID,
					StopReason: StopReasonAborted, ErrorMessage: ctx.Err().Error(),
				}
				s.emit(StreamError{Message: s.final})
				s.finish()
			}()
			return s
		},
	}, nil
}

// runPrompt maps a provider-reported abort back to the context error, so
// callers observing the run result keep errors.Is identity. Exercises the
// runPrompt branch directly (not Run's own ctx.Done select).
func TestRunPromptMapsAbortToContextError(t *testing.T) {
	started := make(chan struct{})
	prov, _ := NewProviders(abortOnCancelProvider{id: "faux", started: started})
	d, _ := New(Options{Providers: prov, Model: "m"})
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	resc := make(chan runResult, 1)
	go func() {
		resc <- d.runPrompt(queuedPrompt{ctx: ctx, messages: []Message{userText("go")}})
	}()

	<-started // provider is now streaming; cancel mid-turn
	cancel()

	select {
	case res := <-resc:
		if !errors.Is(res.err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", res.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runPrompt did not return")
	}
}

// A prompt cancelled while queued must be dropped without mutating the
// transcript or storage. Exercises runPrompt's pre-start guard directly.
func TestCancelledQueuedPromptIsDropped(t *testing.T) {
	store := NewMemoryStorage()
	d, _ := New(Options{Providers: toolThenStop(t, "noop"), Model: "m", Storage: store, Session: "s"})
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := d.runPrompt(queuedPrompt{ctx: ctx, messages: []Message{userText("second")}})

	if !errors.Is(res.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", res.err)
	}
	if got := len(d.snapshot()); got != 0 {
		t.Fatalf("transcript mutated: %d messages", got)
	}
	history, _ := store.Load(context.Background(), "s")
	if len(history) != 0 {
		t.Fatalf("storage mutated: %d messages", len(history))
	}
}

// cancelAwareStore rejects Append when its context is cancelled, and records
// whether that ever happened.
type cancelAwareStore struct {
	MemoryStorage
	mu           sync.Mutex
	rejectedOnce bool
}

func (s *cancelAwareStore) Append(ctx context.Context, session string, messages ...Message) error {
	if ctx.Err() != nil {
		s.mu.Lock()
		s.rejectedOnce = true
		s.mu.Unlock()
		return ctx.Err()
	}
	return s.MemoryStorage.Append(ctx, session, messages...)
}

// Persistence must not be cancelled along with the run: appending a completed
// message with a cancelled run context must still reach the store. Exercises
// appendMessage directly.
func TestPersistenceSurvivesCancellation(t *testing.T) {
	store := &cancelAwareStore{MemoryStorage: *NewMemoryStorage()}
	d, _ := New(Options{Providers: toolThenStop(t, "noop"), Model: "m", Storage: store, Session: "s"})
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.appendMessage(ctx, userText("x"))

	store.mu.Lock()
	rejected := store.rejectedOnce
	store.mu.Unlock()
	if rejected {
		t.Fatal("storage saw a cancelled context on Append")
	}
	if history, _ := store.Load(context.Background(), "s"); len(history) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(history))
	}
}

func assertToolCancelled(t *testing.T, outcome chan bool) {
	t.Helper()
	select {
	case cancelled := <-outcome:
		if !cancelled {
			t.Fatal("tool completed instead of observing cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tool did not return after cancellation")
	}
}
