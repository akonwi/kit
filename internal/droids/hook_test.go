package droids

import (
	"context"
	"fmt"
	"testing"
)

// toolThenStop builds a provider that calls the named tool once, then stops.
func toolThenStop(t *testing.T, toolName string) Providers {
	t.Helper()
	calls := 0
	prov, err := NewProviders(fauxProvider{
		model: Model{ID: "m"},
		reply: func(req Request) AssistantMessage {
			calls++
			if calls == 1 {
				return AssistantMessage{
					Content:    []Content{ToolCall{ID: "c1", Name: toolName, Arguments: []byte(`{}`)}},
					StopReason: StopReasonToolUse,
				}
			}
			return AssistantMessage{Content: []Content{TextContent{Text: "final"}}, StopReason: StopReasonStop}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return prov
}

func echoTool(name string, ran *bool) AnyTool {
	return NewTool(Tool[struct{}]{
		Name: name,
		Execute: func(context.Context, struct{}) (ToolResult, error) {
			*ran = true
			return ToolText("executed"), nil
		},
	})
}

func TestBeforeToolCallRejectsAndContinues(t *testing.T) {
	var ran bool
	d, _ := New(Options{
		Providers: toolThenStop(t, "act"),
		Model:     "m",
		Tools:     []AnyTool{echoTool("act", &ran)},
		BeforeToolCall: func(_ context.Context, in BeforeToolContext) (BeforeToolResult, error) {
			return BeforeToolResult{Reject: true, Reason: "nope"}, nil
		},
	})
	defer d.Close()

	message, err := d.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if message.Text() != "final" {
		t.Fatalf("message = %q, want final (run should continue after rejection)", message.Text())
	}
	if ran {
		t.Fatal("tool executed despite rejection")
	}
	last := lastToolResult(t, d)
	if !last.IsError || textOfContent(last.Content) != "nope" {
		t.Fatalf("expected rejected error result, got %+v", last)
	}
}

func TestBeforeToolCallRejectsWithDefaultReason(t *testing.T) {
	var ran bool
	d, _ := New(Options{
		Providers: toolThenStop(t, "act"),
		Model:     "m",
		Tools:     []AnyTool{echoTool("act", &ran)},
		BeforeToolCall: func(_ context.Context, in BeforeToolContext) (BeforeToolResult, error) {
			return BeforeToolResult{Reject: true}, nil
		},
	})
	defer d.Close()

	if _, err := d.Execute(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("tool executed despite rejection")
	}
	last := lastToolResult(t, d)
	if !last.IsError || textOfContent(last.Content) != "Tool execution was rejected" {
		t.Fatalf("unexpected default rejection result: %+v", last)
	}
}

func TestToolResultCanMarkApplicationError(t *testing.T) {
	d, err := New(Options{
		Providers: toolThenStop(t, "act"),
		Model:     "m",
		Tools: []AnyTool{NewTool(Tool[struct{}]{
			Name: "act",
			Execute: func(context.Context, struct{}) (ToolResult, error) {
				result := ToolText("remote tool rejected the request")
				result.IsError = true
				result.Details = map[string]any{"code": "rejected"}
				return result, nil
			},
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.Execute(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	last := lastToolResult(t, d)
	if !last.IsError || textOfContent(last.Content) != "remote tool rejected the request" {
		t.Fatalf("application error result was not preserved: %+v", last)
	}
}

func TestBeforeToolCallShortCircuits(t *testing.T) {
	var ran bool
	cached := ToolText("cached result")
	d, _ := New(Options{
		Providers: toolThenStop(t, "act"),
		Model:     "m",
		Tools:     []AnyTool{echoTool("act", &ran)},
		BeforeToolCall: func(_ context.Context, in BeforeToolContext) (BeforeToolResult, error) {
			return BeforeToolResult{Result: &cached}, nil
		},
	})
	defer d.Close()

	if _, err := d.Execute(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("tool executed despite short-circuit")
	}
	if got := textOfContent(lastToolResult(t, d).Content); got != "cached result" {
		t.Fatalf("got %q", got)
	}
}

func TestAfterToolCallIdentityPreservesExecutionError(t *testing.T) {
	d, err := New(Options{
		Providers: toolThenStop(t, "act"),
		Model:     "m",
		Tools: []AnyTool{NewTool(Tool[struct{}]{
			Name: "act",
			Execute: func(context.Context, struct{}) (ToolResult, error) {
				return ToolResult{}, fmt.Errorf("failed")
			},
		})},
		AfterToolCall: func(_ context.Context, in AfterToolContext) (*ToolResult, error) {
			return &in.Result, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.Execute(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if result := lastToolResult(t, d); !result.IsError || textOfContent(result.Content) != "failed" {
		t.Fatalf("execution error was not preserved: %+v", result)
	}
}

func TestAfterToolCallRewrites(t *testing.T) {
	var ran bool
	d, _ := New(Options{
		Providers: toolThenStop(t, "act"),
		Model:     "m",
		Tools:     []AnyTool{echoTool("act", &ran)},
		AfterToolCall: func(_ context.Context, in AfterToolContext) (*ToolResult, error) {
			r := ToolText("rewritten from: " + textOfContent(in.Result.Content))
			return &r, nil
		},
	})
	defer d.Close()

	if _, err := d.Execute(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("tool should have executed")
	}
	if got := textOfContent(lastToolResult(t, d).Content); got != "rewritten from: executed" {
		t.Fatalf("got %q", got)
	}
}

func TestHookErrorDegradesToErrorResult(t *testing.T) {
	var ran bool
	d, _ := New(Options{
		Providers: toolThenStop(t, "act"),
		Model:     "m",
		Tools:     []AnyTool{echoTool("act", &ran)},
		BeforeToolCall: func(_ context.Context, in BeforeToolContext) (BeforeToolResult, error) {
			return BeforeToolResult{}, fmt.Errorf("hook boom")
		},
	})
	defer d.Close()

	if _, err := d.Execute(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	last := lastToolResult(t, d)
	if !last.IsError || textOfContent(last.Content) != "hook boom" {
		t.Fatalf("expected degraded error result, got %+v", last)
	}
}

func lastToolResult(t *testing.T, d *Droid) ToolResultMessage {
	t.Helper()
	msgs := d.snapshot()
	for i := len(msgs) - 1; i >= 0; i-- {
		if trm, ok := msgs[i].(ToolResultMessage); ok {
			return trm
		}
	}
	t.Fatal("no tool result message in transcript")
	return ToolResultMessage{}
}
