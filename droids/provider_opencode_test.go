package droids

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenCodeCatalogRoutesModelsByProtocol(t *testing.T) {
	t.Parallel()
	counts := map[ModelAPI]int{}
	for _, model := range OpenCodeModels() {
		counts[model.API]++
		if model.Provider != "opencode" || model.BaseURL != defaultOpenCodeBaseURL {
			t.Fatalf("model = %#v", model)
		}
	}
	for _, api := range []ModelAPI{ModelAPIOpenAIResponses, ModelAPIOpenAIChat, ModelAPIAnthropicMessages} {
		if counts[api] == 0 {
			t.Fatalf("catalog has no %s models: %#v", api, counts)
		}
	}
	models := OpenCodeModels()
	if len(models) == 0 {
		t.Fatal("catalog is empty")
	}
	if model, ok := OpenCodeModel(models[0].ID); !ok || model.Provider != "opencode" {
		t.Fatalf("OpenCodeModel(%q) = %#v, %v", models[0].ID, model, ok)
	}
	if model, ok := OpenCodeModel("grok-build-0.1"); !ok || model.Reasoning {
		t.Fatalf("fixed-reasoning model = %#v, %v", model, ok)
	}
}

func TestOpenCodeReportsRawResponseStreamErrors(t *testing.T) {
	t.Parallel()
	reported := make(chan error, 1)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: {\n\n")),
			Request:    request,
		}, nil
	})}
	providers, err := NewProviders(OpenCode{
		APIKey: "test-key", HTTPClient: client,
		ErrorReporter: func(_ context.Context, _ Model, err error) { reported <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	var model Model
	for _, candidate := range providers.Models() {
		if candidate.API == ModelAPIOpenAIResponses {
			model = candidate
			break
		}
	}
	stream := providers.Stream(context.Background(), model, Request{Messages: []Message{
		UserMessage{Content: []InputContent{TextInput{Text: "hi"}}},
	}})
	for range stream.Events() {
	}
	select {
	case err := <-reported:
		if err == nil {
			t.Fatal("reported nil error")
		}
	default:
		t.Fatal("raw response stream error was not reported")
	}
}

func TestOpenCodeReportsRawTransportErrors(t *testing.T) {
	t.Parallel()
	want := errors.New("connection reset by peer")
	reported := make(chan error, 1)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, want
	})}
	providers, err := NewProviders(OpenCode{
		APIKey: "test-key", HTTPClient: client,
		ErrorReporter: func(_ context.Context, _ Model, err error) { reported <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	var model Model
	for _, candidate := range providers.Models() {
		if candidate.API == ModelAPIOpenAIChat {
			model = candidate
			break
		}
	}
	if model.ID == "" {
		t.Fatal("catalog has no Chat Completions model")
	}
	stream := providers.Stream(context.Background(), model, Request{Messages: []Message{
		UserMessage{Content: []InputContent{TextInput{Text: "hi"}}},
	}})
	for range stream.Events() {
	}
	if got := <-reported; !errors.Is(got, want) {
		t.Fatalf("reported error = %v, want %v", got, want)
	}
}

func TestOpenCodeRejectsTwoAPIKeySources(t *testing.T) {
	t.Parallel()
	_, err := NewProviders(OpenCode{APIKey: "direct", APIKeySource: func(context.Context) (string, error) {
		return "dynamic", nil
	}})
	if err == nil {
		t.Fatal("two API key sources were accepted")
	}
}

func TestOpenCodeRoutesProtocolEndpoints(t *testing.T) {
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

	providers, err := NewProviders(OpenCode{APIKey: "test-key", BaseURL: server.URL})
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
