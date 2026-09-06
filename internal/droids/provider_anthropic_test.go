package droids

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestAnthropicResolvesAPIKeyForEveryRequest(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "unintended-environment-token")

	type requestAuth struct{ apiKey, authorization string }
	headers := make(chan requestAuth, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		headers <- requestAuth{apiKey: request.Header.Get("x-api-key"), authorization: request.Header.Get("Authorization")}
		http.Error(writer, "test response", http.StatusBadRequest)
	}))
	defer server.Close()
	apiKey := "first-key"
	providers, err := NewProviders(Anthropic{
		APIKeySource: func(context.Context) (string, error) { return apiKey, nil },
		BaseURL:      server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("claude-haiku-4-5")
	if !ok {
		t.Fatal("test model did not resolve")
	}
	for _, want := range []string{"first-key", "second-key"} {
		stream := providers.Stream(context.Background(), model, Request{Messages: []Message{UserMessage{Content: []InputContent{TextInput{Text: "hello"}}}}})
		for range stream.Events() {
		}
		if got := <-headers; got.apiKey != want || got.authorization != "" {
			t.Fatalf("request auth = %#v, want API key %q and no environment authorization", got, want)
		}
		apiKey = "second-key"
	}
}

func TestAnthropicKeyResolutionCancellationIsAnAbort(t *testing.T) {
	t.Parallel()
	providers, err := NewProviders(Anthropic{APIKeySource: func(ctx context.Context) (string, error) { return "", ctx.Err() }})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := providers.Model("claude-haiku-4-5")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := providers.Stream(ctx, model, Request{Messages: []Message{UserMessage{Content: []InputContent{TextInput{Text: "hello"}}}}})
	for range stream.Events() {
	}
	message := stream.Result()
	if message.StopReason != StopReasonAborted || message.ErrorKind == ErrorAuthentication {
		t.Fatalf("canceled key resolution = %#v", message)
	}
}

func TestAnthropicRejectsStaticAndDynamicAPIKeys(t *testing.T) {
	t.Parallel()
	_, err := NewProviders(Anthropic{APIKey: "static", APIKeySource: func(context.Context) (string, error) { return "dynamic", nil }})
	if err == nil {
		t.Fatal("Anthropic accepted both APIKey and APIKeySource")
	}
}

// Verify neutral messages/tools translate to the Anthropic wire shape.
func TestAnthropicMessageConversion(t *testing.T) {
	msgs := []Message{
		UserMessage{Content: []InputContent{TextInput{Text: "hi"}}},
		AssistantMessage{Content: []AssistantContent{
			TextContent{Text: "let me check"},
			ToolCall{ID: "t1", Name: "get_weather", Arguments: []byte(`{"city":"Paris"}`)},
		}},
		ToolResultMessage{ToolCallID: "t1", ToolName: "get_weather", Content: []ResultContent{TextContent{Text: "sunny"}}},
	}

	params := toAnthropicMessages(msgs)
	if len(params) != 3 {
		t.Fatalf("got %d messages, want 3", len(params))
	}

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		`"role":"user"`,
		`"role":"assistant"`,
		`"type":"tool_use"`,
		`"name":"get_weather"`,
		`"city":"Paris"`,
		`"type":"tool_result"`,
		`"tool_use_id":"t1"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("wire payload missing %q\n%s", want, s)
		}
	}
}

func TestAnthropicRejectsAttachmentsWithoutRequest(t *testing.T) {
	requested := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requested <- struct{}{}
	}))
	defer server.Close()

	providers, err := NewProviders(Anthropic{
		APIKey: "test-key", BaseURL: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("claude-haiku-4-5")
	if !ok {
		t.Fatal("test model did not resolve")
	}
	stream := providers.Stream(context.Background(), model, Request{Messages: []Message{
		UserMessage{Content: []InputContent{NewFileInputData("report.pdf", "application/pdf", []byte("pdf"))}},
	}})
	var events []StreamEvent
	for event := range stream.Events() {
		events = append(events, event)
	}
	if len(events) != 2 {
		t.Fatalf("events = %#v, want start + error", events)
	}
	if _, ok := events[0].(StreamStart); !ok {
		t.Fatalf("first event = %T, want StreamStart", events[0])
	}
	if _, ok := events[1].(StreamError); !ok {
		t.Fatalf("last event = %T, want StreamError", events[1])
	}
	message := stream.Result()
	if message.StopReason != StopReasonError || !strings.Contains(message.ErrorMessage, "unsupported user content") {
		t.Fatalf("message = %#v", message)
	}
	select {
	case <-requested:
		t.Fatal("unsupported attachment reached provider")
	default:
	}
}

func TestAnthropicDoesNotRaiseMaxTokensForReasoning(t *testing.T) {
	requested := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requested <- struct{}{}
	}))
	defer server.Close()

	providers, err := NewProviders(Anthropic{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("claude-haiku-4-5")
	if !ok {
		t.Fatal("test model did not resolve")
	}
	stream := providers.Stream(context.Background(), model, Request{
		Messages:  []Message{UserMessage{Content: []InputContent{TextInput{Text: "hi"}}}},
		Reasoning: "high",
		MaxTokens: 4096,
	})
	for range stream.Events() {
	}
	message := stream.Result()
	if message.StopReason != StopReasonError || !strings.Contains(message.ErrorMessage, "must exceed") || !strings.Contains(message.ErrorMessage, "reasoning budget") {
		t.Fatalf("message = %#v", message)
	}
	select {
	case <-requested:
		t.Fatal("invalid reasoning allowance reached provider")
	default:
	}
}

func TestAnthropicToolConversion(t *testing.T) {
	tools := []ToolSchema{{
		Name:        "get_weather",
		Description: "Get weather",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"city": map[string]any{"type": "string"}},
			"required":   []any{"city"},
		},
	}}
	raw, err := json.Marshal(toAnthropicTools(tools))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{`"name":"get_weather"`, `"input_schema"`, `"city"`, `"required":["city"]`} {
		if !strings.Contains(s, want) {
			t.Fatalf("tool payload missing %q\n%s", want, s)
		}
	}
}

func TestAnthropicContextWindowStopIsStreamError(t *testing.T) {
	message := assembleAnthropicMessage(Model{ID: "claude-test"}, anthropic.Message{
		StopReason: anthropic.StopReasonModelContextWindowExceeded,
	})
	if message.StopReason != StopReasonContextWindow {
		t.Fatalf("stop reason = %q", message.StopReason)
	}
	if _, ok := anthropicTerminalEvent(message).(StreamError); !ok {
		t.Fatalf("terminal event = %T, want StreamError", anthropicTerminalEvent(message))
	}
}

func TestReasoningTokenBudget(t *testing.T) {
	if reasoningTokenBudget("off") != 0 {
		t.Fatal("off should disable thinking")
	}
	if reasoningTokenBudget("high") <= reasoningTokenBudget("low") {
		t.Fatal("high budget should exceed low")
	}
}
