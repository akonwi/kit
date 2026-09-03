package droids

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Run's event channel is scoped to one run and closes after AgentEnd, so
// the idiomatic range loop terminates without closing the Droid.
func TestRunRangeTerminates(t *testing.T) {
	providers, err := NewProviders(fauxProvider{
		model: Model{ID: "m"},
		reply: func(Request) AssistantMessage {
			return AssistantMessage{
				Content:    []Content{TextContent{Text: "done"}},
				StopReason: StopReasonStop,
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(Options{Providers: providers, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	run, err := d.Stream(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	var events []Event
	for event := range run.Events() {
		events = append(events, event)
	}

	if len(events) == 0 {
		t.Fatal("run emitted no events")
	}
	if _, ok := events[0].(AgentStart); !ok {
		t.Fatalf("first event = %T, want AgentStart", events[0])
	}
	if _, ok := events[len(events)-1].(AgentEnd); !ok {
		t.Fatalf("last event = %T, want AgentEnd", events[len(events)-1])
	}

	message, err := run.Result()
	if err != nil {
		t.Fatal(err)
	}
	if message.Text() != "done" {
		t.Fatalf("message = %q, want done", message.Text())
	}
}

// An existing, undrained session event channel must not block a Run;
// per-run events are delivered exclusively to the returned stream.
func TestRunDoesNotBlockOnSessionEvents(t *testing.T) {
	providers, _ := NewProviders(fauxProvider{
		model: Model{ID: "m"},
		reply: func(Request) AssistantMessage {
			return AssistantMessage{Content: []Content{TextContent{Text: "done"}}, StopReason: StopReasonStop}
		},
	})
	d, _ := New(Options{Providers: providers, Model: "m"})
	defer d.Close()

	_ = d.Events() // create the long-lived channel, but never drain it
	run, err := d.Stream(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	finished := make(chan struct{})
	go func() {
		for range run.Events() {
		}
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Run was blocked by the undrained session event channel")
	}
}

// Result is stable and may be called repeatedly after the run completes.
func TestRunResultIsReusable(t *testing.T) {
	providers, _ := NewProviders(fauxProvider{
		model: Model{ID: "m"},
		reply: func(Request) AssistantMessage {
			return AssistantMessage{Content: []Content{TextContent{Text: "done"}}, StopReason: StopReasonStop}
		},
	})
	d, _ := New(Options{Providers: providers, Model: "m"})
	defer d.Close()

	run, _ := d.Stream(context.Background(), "hi")
	for range run.Events() {
	}

	first, firstErr := run.Result()
	second, secondErr := run.Result()
	if firstErr != nil || secondErr != nil {
		t.Fatalf("Result errors: %v, %v", firstErr, secondErr)
	}
	if first.Text() != second.Text() || first.Text() != "done" {
		t.Fatalf("unstable results: %q, %q", first.Text(), second.Text())
	}
}

// Close completes a queued Run even if it never starts.
func TestCloseCompletesQueuedRun(t *testing.T) {
	entered := make(chan struct{})
	outcome := make(chan bool, 1)
	d, _ := New(Options{
		Providers: toolThenStop(t, "block"),
		Model:     "m",
		Tools:     []AnyTool{blockingTool("block", entered, outcome)},
	})

	go func() { _, _ = d.Execute(context.Background(), "first") }()
	<-entered

	run, err := d.Stream(context.Background(), "second")
	if err != nil {
		t.Fatal(err)
	}
	d.Close()

	select {
	case _, ok := <-run.Events():
		if ok {
			// Active-run events are not routed into the queued stream; any value
			// here would therefore be unexpected.
			t.Fatal("queued stream received an event")
		}
	case <-time.After(time.Second):
		t.Fatal("queued Run was not closed by Close")
	}

	if _, err := run.Result(); err == nil {
		t.Fatal("queued Run Result succeeded after Close")
	}
}

// Close aborting an active Run still delivers AgentEnd before closing
// the per-run channel.
func TestCloseActiveRunDeliversAgentEnd(t *testing.T) {
	entered := make(chan struct{})
	outcome := make(chan bool, 1)
	d, _ := New(Options{
		Providers: toolThenStop(t, "block"),
		Model:     "m",
		Tools:     []AnyTool{blockingTool("block", entered, outcome)},
	})

	run, err := d.Stream(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	d.Close()

	var events []Event
	for event := range run.Events() {
		events = append(events, event)
	}
	if len(events) == 0 {
		t.Fatal("active stream emitted no events")
	}
	if _, ok := events[len(events)-1].(AgentEnd); !ok {
		t.Fatalf("last event = %T, want AgentEnd", events[len(events)-1])
	}
}

// A Run racing Close either fails admission or is completed by the
// worker; it must never return successfully and remain orphaned.
func TestRunCloseRaceNeverOrphans(t *testing.T) {
	for i := 0; i < 200; i++ {
		providers, _ := NewProviders(fauxProvider{
			model: Model{ID: "m"},
			reply: func(Request) AssistantMessage {
				return AssistantMessage{Content: []Content{TextContent{Text: "done"}}, StopReason: StopReasonStop}
			},
		})
		d, _ := New(Options{Providers: providers, Model: "m"})

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var run Run
		var runErr error
		go func() {
			defer wg.Done()
			<-start
			run, runErr = d.Stream(context.Background(), "hi")
		}()
		go func() {
			defer wg.Done()
			<-start
			d.Close()
		}()
		close(start)
		wg.Wait()

		if runErr != nil {
			continue
		}
		result := make(chan struct{})
		go func() {
			_, _ = run.Result()
			close(result)
		}()
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: successful Run was orphaned", i)
		}
		for range run.Events() {
		}
	}
}
