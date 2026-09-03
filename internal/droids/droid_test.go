package droids

import (
	"context"
	"strings"
	"testing"
)

// fauxProvider is a Provider config backed by a scripted stream, for testing
// the loop without real network calls.
type fauxProvider struct {
	model Model
	reply func(req Request) AssistantMessage
}

func (f fauxProvider) build() (providerEntry, error) {
	m := f.model
	m.Provider = "faux"
	impl := f
	return providerEntry{
		id:     "faux",
		models: map[string]Model{m.ID: m},
		stream: func(ctx context.Context, model Model, req Request, opts callOptions) Stream {
			msg := impl.reply(req)
			msg.Provider = "faux"
			msg.Model = model.ID
			ch := make(chan StreamEvent, 2)
			ch <- StreamStart{Partial: AssistantMessage{Provider: "faux", Model: model.ID}}
			if msg.StopReason == StopReasonError || msg.StopReason == StopReasonContextWindow || msg.StopReason == StopReasonAborted {
				ch <- StreamError{Message: msg}
			} else {
				ch <- StreamDone{Message: msg}
			}
			close(ch)
			return &staticStream{events: ch, final: msg}
		},
	}, nil
}

func TestRunSingleTurn(t *testing.T) {
	prov, err := NewProviders(fauxProvider{
		model: Model{ID: "test-model", MaxOutputTokens: 100},
		reply: func(req Request) AssistantMessage {
			return AssistantMessage{
				Content:    []Content{TextContent{Text: "hello back"}},
				StopReason: StopReasonStop,
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStorage()
	d, err := New(Options{
		Providers: prov,
		Model:     "test-model",
		Storage:   store,
		Session:   "s1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	msg, err := d.Execute(context.Background(), "hi")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := msg.Text(); got != "hello back" {
		t.Fatalf("got %q", got)
	}

	history, _ := store.Load(context.Background(), "s1")
	if len(history) != 2 { // user + assistant
		t.Fatalf("expected 2 persisted messages, got %d", len(history))
	}
}

func TestOptionsMaxTokensIsPerRequestAllowance(t *testing.T) {
	var request Request
	providers, err := NewProviders(fauxProvider{
		model: Model{ID: "m", MaxOutputTokens: 10_000},
		reply: func(req Request) AssistantMessage {
			request = req
			return AssistantMessage{StopReason: StopReasonStop}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(Options{Providers: providers, Model: "m", MaxTokens: 1234})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Execute(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if request.MaxTokens != 1234 {
		t.Fatalf("request max tokens = %d", request.MaxTokens)
	}
}

func TestCloneUserMessageCopiesContentSlice(t *testing.T) {
	message := UserMessage{Content: []Content{TextContent{Text: "original"}}}
	cloned := cloneUserMessage(message)
	message.Content[0] = TextContent{Text: "mutated"}
	if got := cloned.Content[0].(TextContent).Text; got != "original" {
		t.Fatalf("cloned content = %q", got)
	}
}

func TestStreamMessagePreservesStructuredUserContent(t *testing.T) {
	var received UserMessage
	providers, err := NewProviders(fauxProvider{
		model: Model{ID: "m"},
		reply: func(req Request) AssistantMessage {
			received = req.Messages[len(req.Messages)-1].(UserMessage)
			return AssistantMessage{Content: []Content{TextContent{Text: "done"}}, StopReason: StopReasonStop}
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

	file := NewFileData("notes.txt", "text/plain", []byte("hello"))
	run, err := d.StreamMessage(context.Background(), UserMessage{Content: []Content{
		TextContent{Text: "Read this"}, file,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for range run.Events() {
	}
	if _, err := run.Result(); err != nil {
		t.Fatal(err)
	}
	if len(received.Content) != 2 || received.Content[1] != file {
		t.Fatalf("provider received %#v", received.Content)
	}
}

func TestRunDoesNotExecuteToolCallsWithoutToolUseStop(t *testing.T) {
	providerCalls := 0
	toolCalls := 0
	providers, err := NewProviders(fauxProvider{
		model: Model{ID: "m"},
		reply: func(Request) AssistantMessage {
			providerCalls++
			return AssistantMessage{
				Content: []Content{ToolCall{
					ID:        "partial",
					Name:      "dangerous",
					Arguments: []byte(`{"path":`),
				}},
				StopReason: StopReasonLength,
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := NewTool(Tool[struct{}]{
		Name: "dangerous",
		Execute: func(context.Context, struct{}) (ToolResult, error) {
			toolCalls++
			return ToolText("ran"), nil
		},
	})
	d, err := New(Options{Providers: providers, Model: "m", Tools: []AnyTool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	message, err := d.Execute(context.Background(), "do it")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if message.StopReason != StopReasonLength {
		t.Fatalf("stop reason = %q", message.StopReason)
	}
	if providerCalls != 1 || toolCalls != 0 {
		t.Fatalf("provider calls = %d, tool calls = %d", providerCalls, toolCalls)
	}
}

func TestNewRejectsDuplicateToolNames(t *testing.T) {
	providers, err := NewProviders(fauxProvider{
		model: Model{ID: "m"},
		reply: func(Request) AssistantMessage {
			return AssistantMessage{StopReason: StopReasonStop}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := func() AnyTool {
		return NewTool(Tool[struct{}]{
			Name: "same",
			Execute: func(context.Context, struct{}) (ToolResult, error) {
				return ToolText("ok"), nil
			},
		})
	}

	_, err = New(Options{
		Providers: providers,
		Model:     "m",
		Tools:     []AnyTool{tool(), tool()},
	})
	if err == nil || !strings.Contains(err.Error(), `duplicate tool name "same"`) {
		t.Fatalf("New error = %v", err)
	}
}

func TestRunWithToolCall(t *testing.T) {
	calls := 0
	prov, _ := NewProviders(fauxProvider{
		model: Model{ID: "m"},
		reply: func(req Request) AssistantMessage {
			calls++
			if calls == 1 {
				return AssistantMessage{
					Content: []Content{ToolCall{
						ID:        "c1",
						Name:      "echo",
						Arguments: []byte(`{"text":"world"}`),
					}},
					StopReason: StopReasonToolUse,
				}
			}
			return AssistantMessage{
				Content:    []Content{TextContent{Text: "done"}},
				StopReason: StopReasonStop,
			}
		},
	})

	type echoArgs struct {
		Text string `json:"text"`
	}
	echo := NewTool(Tool[echoArgs]{
		Name: "echo",
		Execute: func(_ context.Context, a echoArgs) (ToolResult, error) {
			return ToolText("echoed: " + a.Text), nil
		},
	})

	d, _ := New(Options{Providers: prov, Model: "m", Tools: []AnyTool{echo}})
	defer d.Close()

	msg, err := d.Execute(context.Background(), "call echo")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if msg.Text() != "done" {
		t.Fatalf("got %q", msg.Text())
	}
	if calls != 2 {
		t.Fatalf("expected 2 model calls, got %d", calls)
	}
}
