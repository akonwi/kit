package droids

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenCodeGoCatalogRoutesModelsByProtocol(t *testing.T) {
	t.Parallel()
	counts := map[ModelAPI]int{}
	for _, model := range OpenCodeGoModels() {
		counts[model.API]++
		if model.Provider != "opencode-go" || model.BaseURL != defaultOpenCodeGoBaseURL {
			t.Fatalf("model = %#v", model)
		}
	}
	for _, api := range []ModelAPI{ModelAPIOpenAIResponses, ModelAPIOpenAIChat, ModelAPIAnthropicMessages} {
		if counts[api] == 0 {
			t.Fatalf("catalog has no %s models: %#v", api, counts)
		}
	}
}

func TestOpenCodeGoRoutesProtocolEndpoints(t *testing.T) {
	t.Parallel()
	paths := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("x-opencode-session") != "route-session" || request.Header.Get("User-Agent") != "kit/1.0" {
			t.Errorf("headers = %#v", request.Header)
		}
		paths <- request.URL.Path
		http.Error(writer, "expected test rejection", http.StatusBadRequest)
	}))
	defer server.Close()

	providers, err := NewProviders(OpenCodeGo{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[ModelAPI]string{
		ModelAPIOpenAIResponses:   "/responses",
		ModelAPIOpenAIChat:        "/chat/completions",
		ModelAPIAnthropicMessages: "/v1/messages",
	}
	models := map[ModelAPI]Model{}
	for _, model := range providers.Models() {
		if _, ok := models[model.API]; !ok {
			models[model.API] = model
		}
	}
	for api, wantPath := range wanted {
		model, ok := models[api]
		if !ok {
			t.Fatalf("no model for %s", api)
		}
		stream := providers.Stream(context.Background(), model, Request{SessionID: "route-session", Messages: []Message{
			UserMessage{Content: []InputContent{TextInput{Text: "hi"}}},
		}})
		for range stream.Events() {
		}
		if got := <-paths; got != wantPath {
			t.Fatalf("%s path = %q, want %q", api, got, wantPath)
		}
	}
}

func TestOpenCodeGoStreamsChatCompletionsWithSessionHeaders(t *testing.T) {
	t.Parallel()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := request.Header.Get("x-opencode-session"); got != "session-123" {
			t.Errorf("x-opencode-session = %q", got)
		}
		if got := request.Header.Get("User-Agent"); got != "kit/1.0" {
			t.Errorf("User-Agent = %q", got)
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"id\":\"chat_1\",\"model\":\"kimi-k2.7-code\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1,\"total_tokens\":4}}\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	providers, err := NewProviders(OpenCodeGo{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("opencode-go/kimi-k2.7-code")
	if !ok || model.API != ModelAPIOpenAIChat {
		t.Fatalf("model = %#v, %v", model, ok)
	}
	stream := providers.Stream(context.Background(), model, Request{SessionID: "session-123", Messages: []Message{
		UserMessage{Content: []InputContent{TextInput{Text: "hi"}}},
	}})
	for range stream.Events() {
	}
	message := stream.Result()
	if message.Text() != "hello" || message.ResponseID != "chat_1" || message.StopReason != StopReasonStop {
		t.Fatalf("message = %#v", message)
	}
	if message.Usage.Input != 3 || message.Usage.Output != 1 || message.Usage.TotalTokens != 4 {
		t.Fatalf("usage = %#v", message.Usage)
	}
	if got, _ := body["model"].(string); got != "kimi-k2.7-code" {
		t.Fatalf("request model = %q", got)
	}
	if messages, _ := body["messages"].([]any); len(messages) != 1 || !strings.Contains(string(mustJSON(messages)), "hi") {
		t.Fatalf("request messages = %#v", body["messages"])
	}
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}
