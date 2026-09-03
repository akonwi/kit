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

// Verify neutral messages/tools translate to the Anthropic wire shape.
func TestAnthropicMessageConversion(t *testing.T) {
	msgs := []Message{
		UserMessage{Content: []Content{TextContent{Text: "hi"}}},
		AssistantMessage{Content: []Content{
			TextContent{Text: "let me check"},
			ToolCall{ID: "t1", Name: "get_weather", Arguments: []byte(`{"city":"Paris"}`)},
		}},
		ToolResultMessage{ToolCallID: "t1", ToolName: "get_weather", Content: []Content{TextContent{Text: "sunny"}}},
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
		UserMessage{Content: []Content{NewFileData("report.pdf", "application/pdf", []byte("pdf"))}},
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
		Messages:  []Message{UserMessage{Content: []Content{TextContent{Text: "hi"}}}},
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
