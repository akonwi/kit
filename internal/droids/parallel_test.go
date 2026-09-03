package droids

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// multiToolThenStop calls all of toolNames in one assistant message, then stops.
func multiToolThenStop(t *testing.T, toolNames ...string) Providers {
	t.Helper()
	calls := 0
	prov, err := NewProviders(fauxProvider{
		model: Model{ID: "m"},
		reply: func(req Request) AssistantMessage {
			calls++
			if calls == 1 {
				var content []Content
				for i, name := range toolNames {
					content = append(content, ToolCall{
						ID:        fmt.Sprintf("c%d", i),
						Name:      name,
						Arguments: []byte(`{}`),
					})
				}
				return AssistantMessage{Content: content, StopReason: StopReasonToolUse}
			}
			return AssistantMessage{Content: []Content{TextContent{Text: "final"}}, StopReason: StopReasonStop}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return prov
}

func TestParallelToolsRunConcurrently(t *testing.T) {
	var inFlight, maxInFlight int32
	slow := func(name string) AnyTool {
		return NewTool(Tool[struct{}]{
			Name: name,
			Execute: func(ctx context.Context, _ struct{}) (ToolResult, error) {
				n := atomic.AddInt32(&inFlight, 1)
				for {
					m := atomic.LoadInt32(&maxInFlight)
					if n <= m || atomic.CompareAndSwapInt32(&maxInFlight, m, n) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&inFlight, -1)
				return ToolText(name), nil
			},
		})
	}

	d, _ := New(Options{
		Providers: multiToolThenStop(t, "a", "b", "c"),
		Model:     "m",
		Tools:     []AnyTool{slow("a"), slow("b"), slow("c")},
	})
	defer d.Close()

	if _, err := d.Execute(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&maxInFlight); got < 2 {
		t.Fatalf("expected concurrent execution, max in-flight was %d", got)
	}
}

func TestParallelTranscriptIsSourceOrder(t *testing.T) {
	// Tool "a" is slow, "b" is fast: completion order is b,a but the transcript
	// must stay in source order a,b.
	mk := func(name string, delay time.Duration) AnyTool {
		return NewTool(Tool[struct{}]{
			Name: name,
			Execute: func(context.Context, struct{}) (ToolResult, error) {
				time.Sleep(delay)
				return ToolText(name), nil
			},
		})
	}
	d, _ := New(Options{
		Providers: multiToolThenStop(t, "a", "b"),
		Model:     "m",
		Tools:     []AnyTool{mk("a", 30*time.Millisecond), mk("b", 0)},
	})
	defer d.Close()

	if _, err := d.Execute(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}

	var order []string
	for _, m := range d.snapshot() {
		if trm, ok := m.(ToolResultMessage); ok {
			order = append(order, textOfContent(trm.Content))
		}
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("transcript order = %v, want [a b]", order)
	}
}

func TestSequentialToolMForcesSerialBatch(t *testing.T) {
	var inFlight, maxInFlight int32
	mk := func(name string, mode ExecutionMode) AnyTool {
		return NewTool(Tool[struct{}]{
			Name: name,
			Mode: mode,
			Execute: func(context.Context, struct{}) (ToolResult, error) {
				n := atomic.AddInt32(&inFlight, 1)
				for {
					m := atomic.LoadInt32(&maxInFlight)
					if n <= m || atomic.CompareAndSwapInt32(&maxInFlight, m, n) {
						break
					}
				}
				time.Sleep(15 * time.Millisecond)
				atomic.AddInt32(&inFlight, -1)
				return ToolText(name), nil
			},
		})
	}
	d, _ := New(Options{
		Providers: multiToolThenStop(t, "x", "y"),
		Model:     "m",
		// y is sequential, so the whole batch must serialize.
		Tools: []AnyTool{mk("x", ModeParallel), mk("y", ModeSequential)},
	})
	defer d.Close()

	if _, err := d.Execute(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&maxInFlight); got != 1 {
		t.Fatalf("expected serialized batch, max in-flight was %d", got)
	}
}
