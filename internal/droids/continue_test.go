package droids

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

func continuationTranscript(calls ...ToolCall) []Message {
	content := make([]Content, len(calls))
	results := make([]Message, len(calls))
	for i, call := range calls {
		content[i] = call
		results[i] = ToolResultMessage{
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Content:    []Content{TextContent{Text: "result " + call.ID}},
		}
	}
	messages := []Message{
		UserMessage{Content: []Content{TextContent{Text: "original prompt"}}},
		AssistantMessage{Content: content, StopReason: StopReasonToolUse},
	}
	return append(messages, results...)
}

func newContinuationDroid(
	t *testing.T,
	messages []Message,
	reply func(Request) AssistantMessage,
	opts func(*Options),
) *Droid {
	t.Helper()
	providers, err := NewProviders(fauxProvider{model: Model{ID: "m"}, reply: reply})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStorage()
	if err := store.Append(context.Background(), "s", messages...); err != nil {
		t.Fatal(err)
	}
	options := Options{Providers: providers, Model: "m", Storage: store, Session: "s"}
	if opts != nil {
		opts(&options)
	}
	d, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestContinueDoesNotInjectUserMessage(t *testing.T) {
	requests := make(chan Request, 1)
	seed := continuationTranscript(ToolCall{ID: "c1", Name: "lookup", Arguments: []byte(`{}`)})
	d := newContinuationDroid(t, seed, func(req Request) AssistantMessage {
		requests <- req
		return AssistantMessage{Content: []Content{TextContent{Text: "continued"}}, StopReason: StopReasonStop}
	}, nil)
	defer d.Close()

	message, err := d.Continue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.Text() != "continued" {
		t.Fatalf("message = %q, want continued", message.Text())
	}

	req := <-requests
	if len(req.Messages) != len(seed) {
		t.Fatalf("provider received %d messages, want %d", len(req.Messages), len(seed))
	}
	last, ok := req.Messages[len(req.Messages)-1].(ToolResultMessage)
	if !ok || last.ToolCallID != "c1" {
		t.Fatalf("last provider message = %#v, want tool result c1", req.Messages[len(req.Messages)-1])
	}

	if got := len(d.snapshot()); got != len(seed)+1 {
		t.Fatalf("transcript has %d messages, want %d", got, len(seed)+1)
	}
}

func TestContinueStreamClosesAfterRun(t *testing.T) {
	seed := continuationTranscript(ToolCall{ID: "c1", Name: "lookup", Arguments: []byte(`{}`)})
	d := newContinuationDroid(t, seed, func(Request) AssistantMessage {
		return AssistantMessage{Content: []Content{TextContent{Text: "continued"}}, StopReason: StopReasonStop}
	}, nil)
	defer d.Close()

	run, err := d.ContinueStream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	for event := range run.Events() {
		events = append(events, event)
		if start, ok := event.(MessageStart); ok {
			if _, isUser := start.Message.(UserMessage); isUser {
				t.Fatal("continuation emitted a new user message")
			}
		}
	}
	if len(events) == 0 {
		t.Fatal("continuation emitted no events")
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
	if message.Text() != "continued" {
		t.Fatalf("message = %q, want continued", message.Text())
	}
}

func TestContinueRejectsMalformedTranscript(t *testing.T) {
	c1 := ToolCall{ID: "c1", Name: "one", Arguments: []byte(`{}`)}
	c2 := ToolCall{ID: "c2", Name: "two", Arguments: []byte(`{}`)}
	result := func(id, name string) ToolResultMessage {
		return ToolResultMessage{ToolCallID: id, ToolName: name, Content: []Content{TextContent{Text: "ok"}}}
	}

	tests := []struct {
		name     string
		messages []Message
		want     string
	}{
		{name: "empty", want: "empty transcript"},
		{name: "last user", messages: []Message{UserMessage{}}, want: "must end with tool results"},
		{name: "result only", messages: []Message{result("c1", "one")}, want: "no preceding assistant"},
		{name: "result after user", messages: []Message{UserMessage{}, result("c1", "one")}, want: "must follow an assistant"},
		{name: "assistant without calls", messages: []Message{AssistantMessage{}, result("c1", "one")}, want: "has no tool calls"},
		{
			name: "missing result",
			messages: []Message{
				AssistantMessage{Content: []Content{c1, c2}}, result("c1", "one"),
			},
			want: "1 tool results for 2 tool calls",
		},
		{
			name: "out of order",
			messages: []Message{
				AssistantMessage{Content: []Content{c1, c2}}, result("c2", "two"), result("c1", "one"),
			},
			want: `has id "c2", want "c1"`,
		},
		{
			name: "name mismatch",
			messages: []Message{
				AssistantMessage{Content: []Content{c1}}, result("c1", "wrong"),
			},
			want: `has name "wrong", want "one"`,
		},
		{
			name: "empty call id",
			messages: []Message{
				AssistantMessage{Content: []Content{ToolCall{Name: "one"}}}, result("", "one"),
			},
			want: "tool call 0 has an empty id",
		},
		{
			name: "empty call name",
			messages: []Message{
				AssistantMessage{Content: []Content{ToolCall{ID: "c1"}}}, result("c1", ""),
			},
			want: "tool call 0 has an empty name",
		},
		{
			name: "duplicate call id",
			messages: []Message{
				AssistantMessage{Content: []Content{c1, ToolCall{ID: "c1", Name: "two"}}},
				result("c1", "one"), result("c1", "two"),
			},
			want: `tool call 1 has duplicate id "c1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerCalls := 0
			d := newContinuationDroid(t, tt.messages, func(Request) AssistantMessage {
				providerCalls++
				return AssistantMessage{StopReason: StopReasonStop}
			}, nil)
			defer d.Close()

			_, err := d.Continue(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Continue error = %v, want substring %q", err, tt.want)
			}
			if providerCalls != 0 {
				t.Fatalf("provider called %d times for malformed continuation", providerCalls)
			}
		})
	}
}

func TestContinueStreamValidationErrorClosesRun(t *testing.T) {
	providerCalls := 0
	d := newContinuationDroid(t, nil, func(Request) AssistantMessage {
		providerCalls++
		return AssistantMessage{StopReason: StopReasonStop}
	}, nil)
	defer d.Close()

	run, err := d.ContinueStream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for event := range run.Events() {
		t.Fatalf("validation failure emitted event %T", event)
	}
	if _, err := run.Result(); err == nil || !strings.Contains(err.Error(), "empty transcript") {
		t.Fatalf("Result error = %v, want empty transcript error", err)
	}
	if providerCalls != 0 {
		t.Fatalf("provider called %d times for malformed continuation", providerCalls)
	}
}

func TestContinueDefersSteeringUntilNextNormalRun(t *testing.T) {
	requests := make(chan Request, 2)
	seed := continuationTranscript(ToolCall{ID: "c1", Name: "lookup", Arguments: []byte(`{}`)})
	d := newContinuationDroid(t, seed, func(req Request) AssistantMessage {
		requests <- req
		return AssistantMessage{Content: []Content{TextContent{Text: "done"}}, StopReason: StopReasonStop}
	}, nil)
	defer d.Close()

	d.Steer("pending steering")
	if _, err := d.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	continued := <-requests
	for _, message := range continued.Messages {
		if user, ok := message.(UserMessage); ok && textOfContent(user.Content) == "pending steering" {
			t.Fatal("continuation injected pending steering")
		}
	}

	if _, err := d.Execute(context.Background(), "next prompt"); err != nil {
		t.Fatal(err)
	}
	next := <-requests
	found := false
	for _, message := range next.Messages {
		if user, ok := message.(UserMessage); ok && textOfContent(user.Content) == "pending steering" {
			found = true
		}
	}
	if !found {
		t.Fatal("pending steering was not retained for the next normal run")
	}
}

// Validation occurs when a queued continuation starts, after earlier runs have
// finished mutating the transcript—not when ContinueStream is called.
func TestContinuationValidationOccursAtExecutionTime(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	providerCalls := 0
	providers, _ := NewProviders(fauxProvider{
		model: Model{ID: "m"},
		reply: func(Request) AssistantMessage {
			providerCalls++
			if providerCalls == 1 {
				return AssistantMessage{
					Content:    []Content{ToolCall{ID: "c1", Name: "gate", Arguments: []byte(`{}`)}},
					StopReason: StopReasonToolUse,
				}
			}
			return AssistantMessage{Content: []Content{TextContent{Text: "continued"}}, StopReason: StopReasonStop}
		},
	})
	gate := NewTool(Tool[struct{}]{
		Name: "gate",
		Execute: func(context.Context, struct{}) (ToolResult, error) {
			close(entered)
			<-release
			return ToolText("approved"), nil
		},
	})
	d, _ := New(Options{Providers: providers, Model: "m", Tools: []AnyTool{gate}, MaxSteps: 1})
	defer d.Close()

	firstDone := make(chan error, 1)
	go func() {
		_, err := d.Execute(context.Background(), "first")
		firstDone <- err
	}()
	<-entered // transcript currently ends with an assistant tool call, no result

	run, err := d.ContinueStream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	close(release) // first run appends the result, then queued continuation starts

	for range run.Events() {
	}
	message, err := run.Result()
	if err != nil {
		t.Fatal(err)
	}
	if message.Text() != "continued" {
		t.Fatalf("message = %q, want continued", message.Text())
	}
	if err := <-firstDone; err == nil || !strings.Contains(err.Error(), "reached MaxSteps (1)") {
		t.Fatalf("first run error = %v, want MaxSteps error", err)
	}
}

func TestContinueGetsFreshMaxStepsBudget(t *testing.T) {
	calls := 0
	seed := continuationTranscript(ToolCall{ID: "c1", Name: "echo", Arguments: []byte(`{}`)})
	d := newContinuationDroid(t, seed, func(Request) AssistantMessage {
		calls++
		return AssistantMessage{
			Content: []Content{ToolCall{
				ID: "continued-" + strconv.Itoa(calls), Name: "echo", Arguments: []byte(`{}`),
			}},
			StopReason: StopReasonToolUse,
		}
	}, func(opts *Options) {
		opts.MaxSteps = 2
		opts.Tools = []AnyTool{NewTool(Tool[struct{}]{
			Name: "echo",
			Execute: func(context.Context, struct{}) (ToolResult, error) {
				return ToolText("ok"), nil
			},
		})}
	})
	defer d.Close()

	_, err := d.Continue(context.Background())
	if err == nil || !strings.Contains(err.Error(), "reached MaxSteps (2)") {
		t.Fatalf("Continue error = %v, want MaxSteps error", err)
	}
	if calls != 2 {
		t.Fatalf("provider calls = %d, want fresh budget of 2", calls)
	}
}
