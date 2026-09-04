package droids

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCompactionRunsBeforeFirstProviderRequest(t *testing.T) {
	prompt := strings.Repeat("long context ", 80)
	var providerRequest Request
	var hookRequest CompactionRequest
	providers, err := NewProviders(fauxProvider{
		model: Model{ID: "m", ContextWindow: 200, MaxOutputTokens: 20},
		reply: func(req Request) AssistantMessage {
			providerRequest = req
			return AssistantMessage{Content: []Content{TextContent{Text: "done"}}, StopReason: StopReasonStop}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStorage()
	d, err := New(Options{
		Providers: providers,
		Model:     "m",
		Storage:   store,
		Session:   "session",
		Compact: func(_ context.Context, req CompactionRequest) (CompactionResult, error) {
			hookRequest = req
			return CompactionResult{Applied: true, Messages: []Message{
				UserMessage{Content: []Content{TextContent{Text: "compact state"}}},
			}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	run, err := d.Stream(context.Background(), prompt)
	if err != nil {
		t.Fatal(err)
	}
	var starts, ends int
	for event := range run.Events() {
		switch event.(type) {
		case CompactionStart:
			starts++
		case CompactionEnd:
			ends++
		}
	}
	if _, err := run.Result(); err != nil {
		t.Fatal(err)
	}

	if hookRequest.Reason != CompactionThreshold {
		t.Fatalf("compaction reason = %q", hookRequest.Reason)
	}
	if hookRequest.Usage.ContextWindow != 200 || hookRequest.Usage.EstimatedInput == 0 {
		t.Fatalf("context usage = %#v", hookRequest.Usage)
	}
	if starts != 1 || ends != 1 {
		t.Fatalf("compaction events = %d starts, %d ends", starts, ends)
	}
	if len(providerRequest.Messages) != 1 || messageText(providerRequest.Messages[0]) != "compact state" {
		t.Fatalf("provider messages = %#v", providerRequest.Messages)
	}

	// Compaction replaces only active context. The original user message and
	// successful assistant response remain the durable transcript.
	history, err := store.Load(context.Background(), "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || messageText(history[0]) != prompt || messageText(history[1]) != "done" {
		t.Fatalf("durable history = %#v", history)
	}
}

func TestCompactionDoesNotRunBelowThreshold(t *testing.T) {
	hookCalls := 0
	providers, _ := NewProviders(fauxProvider{
		model: Model{ID: "m", ContextWindow: 10_000, MaxOutputTokens: 100},
		reply: func(Request) AssistantMessage {
			return AssistantMessage{StopReason: StopReasonStop}
		},
	})
	d, err := New(Options{
		Providers: providers,
		Model:     "m",
		Compact: func(context.Context, CompactionRequest) (CompactionResult, error) {
			hookCalls++
			return CompactionResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.Execute(context.Background(), "short"); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 0 {
		t.Fatalf("hook calls = %d", hookCalls)
	}
}

func TestCompactionUsesStricterInputLimit(t *testing.T) {
	hookCalls := 0
	providers, _ := NewProviders(fauxProvider{
		model: Model{
			ID:              "m",
			ContextWindow:   100_000,
			MaxInputTokens:  500,
			MaxOutputTokens: 10_000,
		},
		reply: func(Request) AssistantMessage {
			return AssistantMessage{StopReason: StopReasonStop}
		},
	})
	d, err := New(Options{
		Providers: providers,
		Model:     "m",
		Compact: func(_ context.Context, req CompactionRequest) (CompactionResult, error) {
			hookCalls++
			if req.Usage.MaxInputTokens != 500 {
				t.Fatalf("max input = %d", req.Usage.MaxInputTokens)
			}
			return CompactionResult{Applied: true, Messages: []Message{
				UserMessage{Content: []Content{TextContent{Text: "short"}}},
			}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.Execute(context.Background(), strings.Repeat("x", 900)); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 1 {
		t.Fatalf("hook calls = %d", hookCalls)
	}
}

func TestCompactionSkipsProactiveDetectionWithoutContextWindow(t *testing.T) {
	hookCalls := 0
	providers, _ := NewProviders(fauxProvider{
		model: Model{ID: "m", MaxOutputTokens: 100},
		reply: func(Request) AssistantMessage {
			return AssistantMessage{StopReason: StopReasonStop}
		},
	})
	d, err := New(Options{
		Providers: providers,
		Model:     "m",
		Compact: func(context.Context, CompactionRequest) (CompactionResult, error) {
			hookCalls++
			return CompactionResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.Execute(context.Background(), strings.Repeat("x", 20_000)); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 0 {
		t.Fatalf("hook calls = %d", hookCalls)
	}
}

func TestCompactionIncludesPendingSteeringInUsageAndSnapshot(t *testing.T) {
	steering := strings.Repeat("steer ", 100)
	var hookRequest CompactionRequest
	providers, _ := NewProviders(fauxProvider{
		model: Model{ID: "m", ContextWindow: 300, MaxOutputTokens: 20},
		reply: func(Request) AssistantMessage {
			return AssistantMessage{StopReason: StopReasonStop}
		},
	})
	d, err := New(Options{
		Providers: providers,
		Model:     "m",
		Compact: func(_ context.Context, req CompactionRequest) (CompactionResult, error) {
			hookRequest = req
			return CompactionResult{Applied: true, Messages: []Message{
				UserMessage{Content: []Content{TextContent{Text: "short"}}},
			}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Steer(steering)

	if _, err := d.Execute(context.Background(), "prompt"); err != nil {
		t.Fatal(err)
	}
	if len(hookRequest.Messages) != 2 || messageText(hookRequest.Messages[1]) != steering {
		t.Fatalf("hook messages = %#v", hookRequest.Messages)
	}
}

func TestCompactionHookCannotMutateDroidModel(t *testing.T) {
	providers, _ := NewProviders(fauxProvider{
		model: Model{
			ID:              "m",
			Input:           []string{"text"},
			ReasoningLevels: []string{"low"},
			ContextWindow:   100,
			MaxOutputTokens: 10,
		},
		reply: func(Request) AssistantMessage { return AssistantMessage{StopReason: StopReasonStop} },
	})
	d, err := New(Options{
		Providers: providers,
		Model:     "m",
		Compact: func(_ context.Context, req CompactionRequest) (CompactionResult, error) {
			req.Model.Input[0] = "mutated"
			req.Usage.Model.ReasoningLevels[0] = "mutated"
			return CompactionResult{Applied: true, Messages: []Message{
				UserMessage{Content: []Content{TextContent{Text: "short"}}},
			}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Execute(context.Background(), strings.Repeat("x", 500)); err != nil {
		t.Fatal(err)
	}
	if d.model.Input[0] != "text" || d.model.ReasoningLevels[0] != "low" {
		t.Fatalf("Droid model mutated: %#v", d.model)
	}
}

func TestCompactionHookErrorFailsRunBeforeProvider(t *testing.T) {
	providerCalls := 0
	providers, _ := NewProviders(fauxProvider{
		model: Model{ID: "m", ContextWindow: 100, MaxOutputTokens: 10},
		reply: func(Request) AssistantMessage {
			providerCalls++
			return AssistantMessage{StopReason: StopReasonStop}
		},
	})
	d, err := New(Options{
		Providers: providers,
		Model:     "m",
		Compact: func(context.Context, CompactionRequest) (CompactionResult, error) {
			return CompactionResult{}, errors.New("summary unavailable")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	message, err := d.Execute(context.Background(), strings.Repeat("x", 500))
	if err == nil || !strings.Contains(err.Error(), "summary unavailable") {
		t.Fatalf("error = %v", err)
	}
	if message.StopReason != StopReasonError {
		t.Fatalf("message = %#v", message)
	}
	if providerCalls != 0 {
		t.Fatalf("provider calls = %d", providerCalls)
	}
}

func TestCompactionRejectsInvalidOrOversizedReplacement(t *testing.T) {
	tests := []struct {
		name        string
		replacement []Message
		wantError   string
	}{
		{
			name: "dangling tool result",
			replacement: []Message{ToolResultMessage{
				ToolCallID: "call", Content: []Content{TextContent{Text: "result"}},
			}},
			wantError: "no preceding tool call",
		},
		{
			name: "incomplete tool call",
			replacement: []Message{AssistantMessage{
				Content:    []Content{ToolCall{ID: "call", Name: "tool", Arguments: []byte(`{}`)}},
				StopReason: StopReasonLength,
			}},
			wantError: "contains incomplete tool calls",
		},
		{
			name: "tool use without calls",
			replacement: []Message{AssistantMessage{
				Content:    []Content{TextContent{Text: "calling"}},
				StopReason: StopReasonToolUse,
			}},
			wantError: "without tool calls",
		},
		{
			name: "does not reduce",
			replacement: []Message{UserMessage{Content: []Content{
				TextContent{Text: strings.Repeat("y", 600)},
			}}},
			wantError: "did not reduce",
		},
		{
			name: "still above trigger",
			replacement: []Message{UserMessage{Content: []Content{
				TextContent{Text: strings.Repeat("y", 300)},
			}}},
			wantError: "remains above the compaction trigger",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers, _ := NewProviders(fauxProvider{
				model: Model{ID: "m", ContextWindow: 100, MaxOutputTokens: 10},
				reply: func(Request) AssistantMessage {
					t.Fatal("provider should not be called")
					return AssistantMessage{}
				},
			})
			d, err := New(Options{
				Providers: providers,
				Model:     "m",
				Compact: func(context.Context, CompactionRequest) (CompactionResult, error) {
					return CompactionResult{Applied: true, Messages: tt.replacement}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()

			_, err = d.Execute(context.Background(), strings.Repeat("x", 500))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestProviderContextOverflowCompactsAndRetriesOnce(t *testing.T) {
	providerCalls := 0
	hookCalls := 0
	store := NewMemoryStorage()
	providers, _ := NewProviders(fauxProvider{
		model: Model{ID: "m", ContextWindow: 10_000, MaxOutputTokens: 100},
		reply: func(Request) AssistantMessage {
			providerCalls++
			if providerCalls == 1 {
				return AssistantMessage{StopReason: StopReasonContextWindow, ErrorMessage: "prompt is too long"}
			}
			return AssistantMessage{Content: []Content{TextContent{Text: "recovered"}}, StopReason: StopReasonStop}
		},
	})
	d, err := New(Options{
		Providers: providers,
		Model:     "m",
		Storage:   store,
		Session:   "s",
		Compact: func(_ context.Context, req CompactionRequest) (CompactionResult, error) {
			hookCalls++
			if req.Reason != CompactionOverflow {
				t.Fatalf("reason = %q", req.Reason)
			}
			return CompactionResult{Applied: true, Messages: []Message{
				UserMessage{Content: []Content{TextContent{Text: "short"}}},
			}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	message, err := d.Execute(context.Background(), strings.Repeat("original ", 30))
	if err != nil {
		t.Fatal(err)
	}
	if message.Text() != "recovered" || providerCalls != 2 || hookCalls != 1 {
		t.Fatalf("message=%q providerCalls=%d hookCalls=%d", message.Text(), providerCalls, hookCalls)
	}
	history, _ := store.Load(context.Background(), "s")
	if len(history) != 2 || messageText(history[0]) == "short" || messageText(history[1]) != "recovered" {
		t.Fatalf("durable history = %#v", history)
	}
}

func TestProviderContextOverflowRetriesOnlyOnce(t *testing.T) {
	providerCalls := 0
	hookCalls := 0
	providers, _ := NewProviders(fauxProvider{
		model: Model{ID: "m", ContextWindow: 10_000, MaxOutputTokens: 100},
		reply: func(Request) AssistantMessage {
			providerCalls++
			return AssistantMessage{StopReason: StopReasonContextWindow, ErrorMessage: "prompt is too long"}
		},
	})
	d, err := New(Options{
		Providers: providers,
		Model:     "m",
		Compact: func(context.Context, CompactionRequest) (CompactionResult, error) {
			hookCalls++
			return CompactionResult{Applied: true, Messages: []Message{
				UserMessage{Content: []Content{TextContent{Text: "short"}}},
			}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	_, err = d.Execute(context.Background(), strings.Repeat("original ", 30))
	if err == nil || !strings.Contains(err.Error(), "prompt is too long") {
		t.Fatalf("error = %v", err)
	}
	if providerCalls != 2 || hookCalls != 1 {
		t.Fatalf("providerCalls=%d hookCalls=%d", providerCalls, hookCalls)
	}
}

func TestContinuationDoesNotInvokeCompaction(t *testing.T) {
	hookCalls := 0
	providerCalls := 0
	store := NewMemoryStorage()
	assistant := AssistantMessage{
		Content:    []Content{ToolCall{ID: "c1", Name: "tool", Arguments: []byte(`{}`)}},
		StopReason: StopReasonToolUse,
	}
	result := ToolResultMessage{ToolCallID: "c1", ToolName: "tool", Content: []Content{TextContent{Text: strings.Repeat("x", 500)}}}
	_ = store.Append(context.Background(), "s", assistant, result)
	providers, _ := NewProviders(fauxProvider{
		model: Model{ID: "m", ContextWindow: 100, MaxOutputTokens: 10},
		reply: func(Request) AssistantMessage {
			providerCalls++
			return AssistantMessage{StopReason: StopReasonContextWindow, ErrorMessage: "prompt is too long"}
		},
	})
	d, err := New(Options{
		Providers: providers,
		Model:     "m",
		Storage:   store,
		Session:   "s",
		Compact: func(context.Context, CompactionRequest) (CompactionResult, error) {
			hookCalls++
			return CompactionResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.Continue(context.Background()); err == nil || !strings.Contains(err.Error(), "prompt is too long") {
		t.Fatalf("error = %v", err)
	}
	if hookCalls != 0 || providerCalls != 1 {
		t.Fatalf("hook calls = %d, provider calls = %d", hookCalls, providerCalls)
	}
}

func TestCompactionEventLifecycle(t *testing.T) {
	tests := []struct {
		name        string
		model       Model
		prompt      string
		replies     []AssistantMessage
		hook        CompactionHook
		wantEvents  []string
		wantRunFail bool
	}{
		{
			name:   "proactive hook failure has no unmatched turn",
			model:  Model{ID: "m", ContextWindow: 100, MaxOutputTokens: 10},
			prompt: strings.Repeat("x", 500),
			hook: func(context.Context, CompactionRequest) (CompactionResult, error) {
				return CompactionResult{}, errors.New("compact failed")
			},
			wantEvents: []string{
				"AgentStart", "MessageStart", "MessageEnd",
				"CompactionStart", "CompactionEnd", "ErrorEvent", "AgentEnd",
			},
			wantRunFail: true,
		},
		{
			name:   "declined overflow closes rejected turn",
			model:  Model{ID: "m", ContextWindow: 10_000, MaxOutputTokens: 100},
			prompt: strings.Repeat("x", 200),
			replies: []AssistantMessage{
				{StopReason: StopReasonContextWindow, ErrorMessage: "prompt is too long"},
			},
			hook: func(context.Context, CompactionRequest) (CompactionResult, error) {
				return CompactionResult{}, nil
			},
			wantEvents: []string{
				"AgentStart", "MessageStart", "MessageEnd", "TurnStart",
				"MessageStart", "MessageEnd", "TurnEnd",
				"CompactionStart", "CompactionEnd", "ErrorEvent", "AgentEnd",
			},
			wantRunFail: true,
		},
		{
			name:   "successful overflow retry uses a second turn",
			model:  Model{ID: "m", ContextWindow: 10_000, MaxOutputTokens: 100},
			prompt: strings.Repeat("x", 200),
			replies: []AssistantMessage{
				{StopReason: StopReasonContextWindow, ErrorMessage: "prompt is too long"},
				{Content: []Content{TextContent{Text: "done"}}, StopReason: StopReasonStop},
			},
			hook: func(context.Context, CompactionRequest) (CompactionResult, error) {
				return CompactionResult{Applied: true, Messages: []Message{
					UserMessage{Content: []Content{TextContent{Text: "short"}}},
				}}, nil
			},
			wantEvents: []string{
				"AgentStart", "MessageStart", "MessageEnd", "TurnStart",
				"MessageStart", "MessageEnd", "TurnEnd",
				"CompactionStart", "CompactionEnd", "TurnStart",
				"MessageStart", "MessageEnd", "TurnEnd", "AgentEnd",
			},
		},
		{
			name:   "failed retry closes both turns",
			model:  Model{ID: "m", ContextWindow: 10_000, MaxOutputTokens: 100},
			prompt: strings.Repeat("x", 200),
			replies: []AssistantMessage{
				{StopReason: StopReasonContextWindow, ErrorMessage: "first overflow"},
				{StopReason: StopReasonContextWindow, ErrorMessage: "second overflow"},
			},
			hook: func(context.Context, CompactionRequest) (CompactionResult, error) {
				return CompactionResult{Applied: true, Messages: []Message{
					UserMessage{Content: []Content{TextContent{Text: "short"}}},
				}}, nil
			},
			wantEvents: []string{
				"AgentStart", "MessageStart", "MessageEnd", "TurnStart",
				"MessageStart", "MessageEnd", "TurnEnd",
				"CompactionStart", "CompactionEnd", "TurnStart",
				"MessageStart", "MessageEnd", "TurnEnd", "ErrorEvent", "AgentEnd",
			},
			wantRunFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := 0
			providers, _ := NewProviders(fauxProvider{
				model: tt.model,
				reply: func(Request) AssistantMessage {
					if call >= len(tt.replies) {
						return AssistantMessage{StopReason: StopReasonStop}
					}
					message := tt.replies[call]
					call++
					return message
				},
			})
			d, err := New(Options{Providers: providers, Model: "m", Compact: tt.hook})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()

			run, err := d.Stream(context.Background(), tt.prompt)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for event := range run.Events() {
				got = append(got, eventName(event))
			}
			_, runErr := run.Result()
			if (runErr != nil) != tt.wantRunFail {
				t.Fatalf("run error = %v", runErr)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantEvents, ",") {
				t.Fatalf("events:\n got %v\nwant %v", got, tt.wantEvents)
			}
		})
	}
}

func eventName(event Event) string {
	switch event.(type) {
	case AgentStart:
		return "AgentStart"
	case AgentEnd:
		return "AgentEnd"
	case CompactionStart:
		return "CompactionStart"
	case CompactionEnd:
		return "CompactionEnd"
	case TurnStart:
		return "TurnStart"
	case TurnEnd:
		return "TurnEnd"
	case MessageStart:
		return "MessageStart"
	case MessageDelta:
		return "MessageDelta"
	case MessageEnd:
		return "MessageEnd"
	case ErrorEvent:
		return "ErrorEvent"
	default:
		return "other"
	}
}

func TestContextUsageRemainingUsesExactMinimum(t *testing.T) {
	d := &Droid{
		model:             Model{ContextWindow: 100, MaxInputTokens: 200},
		compactionReserve: 20,
		toolsByName:       map[string]AnyTool{},
	}
	usage := d.contextUsage([]Message{UserMessage{Content: []Content{
		TextContent{Text: strings.Repeat("x", 92)},
	}}})
	if usage.EstimatedInput != 80 {
		t.Fatalf("estimated input = %d, test requires 80", usage.EstimatedInput)
	}
	if usage.Remaining != 0 {
		t.Fatalf("remaining = %d, want 0", usage.Remaining)
	}
}

func TestApproximateTokenEstimateBiasesHighForNonASCII(t *testing.T) {
	text := strings.Repeat("🙂漢字", 50)
	messages := []Message{
		UserMessage{Content: []Content{TextContent{Text: text}}},
	}
	tokens := estimateRequestTokens(Request{Messages: messages})
	if tokens < len([]rune(text)) {
		t.Fatalf("estimate = %d, runes = %d", tokens, len([]rune(text)))
	}
	if got, want := EstimateMessagesTokens(messages), tokens-estimateRequestTokens(Request{}); got != want {
		t.Fatalf("message estimate = %d, want request contribution %d", got, want)
	}
}

func TestReasoningBudgetAffectsAnthropicAllowanceOnly(t *testing.T) {
	anthropic := Model{
		ID:              "claude",
		API:             ModelAPIAnthropicMessages,
		MaxOutputTokens: 128_000,
		Reasoning:       true,
		ReasoningLevels: []string{"high"},
	}
	if _, err := resolveRequestMaxTokens(anthropic, 0, "high"); err == nil {
		t.Fatal("expected default Anthropic allowance below reasoning budget to fail")
	}
	anthropicTokens, err := resolveRequestMaxTokens(anthropic, 17_408, "high")
	if err != nil || anthropicTokens != 17_408 {
		t.Fatalf("Anthropic allowance = %d, %v", anthropicTokens, err)
	}
	if _, err := resolveRequestMaxTokens(anthropic, 4096, "high"); err == nil {
		t.Fatal("expected explicit Anthropic allowance below reasoning budget to fail")
	}

	openAITokens, err := resolveRequestMaxTokens(Model{
		ID:              "gpt",
		API:             ModelAPIOpenAIResponses,
		MaxOutputTokens: 128_000,
		Reasoning:       true,
		ReasoningLevels: []string{"high"},
	}, 0, "high")
	if err != nil || openAITokens != defaultRequestMaxTokens {
		t.Fatalf("OpenAI allowance = %d, %v", openAITokens, err)
	}
}

func TestContextWindowErrorClassification(t *testing.T) {
	for _, test := range []struct {
		code    string
		message string
	}{
		{code: "context_length_exceeded"},
		{message: "This model's maximum context length is 128000 tokens"},
		{message: "prompt is too long"},
		{message: "model_context_window_exceeded"},
	} {
		if !isContextWindowError(test.code, test.message) {
			t.Fatalf("did not classify code=%q message=%q", test.code, test.message)
		}
	}
	for _, unrelated := range []string{
		"too many requests",
		"image input is too long for this field",
		"tool output exceeds maximum size",
	} {
		if isContextWindowError("invalid_request_error", unrelated) {
			t.Fatalf("classified unrelated provider error %q as context overflow", unrelated)
		}
	}
	message := responseErrorMessageWithCode(Model{ID: "m"}, context.Background(), "context_length_exceeded", "too long")
	if message.StopReason != StopReasonContextWindow {
		t.Fatalf("stop reason = %q", message.StopReason)
	}
}

func messageText(message Message) string {
	switch msg := message.(type) {
	case UserMessage:
		return textOfContent(msg.Content)
	case AssistantMessage:
		return msg.Text()
	case ToolResultMessage:
		return textOfContent(msg.Content)
	default:
		return ""
	}
}
