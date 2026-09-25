package droids

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids/anthropicoauth"
	"github.com/anthropics/anthropic-sdk-go"
)

func TestAnthropicExtraUsageIsUsageLimit(t *testing.T) {
	apiErr := &anthropic.Error{StatusCode: http.StatusBadRequest}
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"You're out of extra usage. Add more at claude.ai/settings/usage and keep going."},"request_id":"req_secret"}`)
	if err := apiErr.UnmarshalJSON(body); err != nil {
		t.Fatal(err)
	}
	if got := classifyAnthropicError(apiErr); got != ProviderUsageLimit {
		t.Fatalf("classify = %q, want usage limit", got)
	}
	detail := anthropicProviderMessage(apiErr)
	if got := sanitizedUsageDetail(detail); got != detail {
		t.Fatalf("sanitized detail = %q, want %q", got, detail)
	}
	if got := safeProviderMessage(ProviderUsageLimit, detail); got != detail {
		t.Fatalf("safe message = %q, want %q", got, detail)
	}
	for _, unsafe := range []string{
		"POST \"https://api.anthropic.com\" request-id secret",
		"Usage quota exhausted; x-api-key: reflected-secret",
	} {
		if got := safeProviderMessage(ProviderUsageLimit, unsafe); got != "Provider usage limit reached" {
			t.Fatalf("provider error leaked: %q", got)
		}
	}
}

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
			ThinkingContent{Thinking: "checking", Signature: "signature"},
			TextContent{Text: "let me check"},
			ToolCall{ID: "t1", Name: "get_weather", Arguments: []byte(`{"city":"Paris"}`)},
		}},
		ToolResultMessage{ToolCallID: "t1", ToolName: "get_weather", Content: []ResultContent{TextContent{Text: "sunny"}}},
	}

	params := toAnthropicMessages(Model{Provider: "anthropic", ID: "claude-opus-5-5"}, msgs)
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
		`"type":"thinking"`,
		`"signature":"signature"`,
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

func TestAnthropicDropsForeignThinkingSignatures(t *testing.T) {
	params := toAnthropicMessages(Model{Provider: "anthropic", ID: "claude-opus-5-5"}, []Message{
		AssistantMessage{
			Provider: "opencode-go", Model: "grok-4.7",
			Content: []AssistantContent{
				ThinkingContent{Thinking: "foreign", Signature: "not-anthropic"},
				TextContent{Text: "visible"},
				ToolCall{ID: "t1", ProviderCallID: "call-1", Name: "bash", Arguments: []byte(`{"command":"pwd"}`)},
			},
		},
		AssistantMessage{Provider: "anthropic", Model: "claude-opus-5", Content: []AssistantContent{
			ThinkingContent{Thinking: "other model", Signature: "opus-5-signature"},
			TextContent{Text: "kept"},
		}},
		AssistantMessage{Provider: "anthropic", Model: "claude-opus-5-5", StopReason: StopReasonError},
	})
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, unwanted := range []string{"foreign", "not-anthropic", "opus-5-signature", "other model"} {
		if strings.Contains(s, unwanted) {
			t.Fatalf("foreign thinking replayed %q\n%s", unwanted, s)
		}
	}
	for _, want := range []string{`"text":"visible"`, `"name":"bash"`, `"text":"kept"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("wire payload missing %q\n%s", want, s)
		}
	}
	if len(params) != 2 {
		t.Fatalf("messages = %d, want 2 (empty error omitted)\n%s", len(params), s)
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

func TestAnthropicManagedEffortModelsUseManagedRequest(t *testing.T) {
	for _, modelID := range []string{"claude-fable-5-1", "claude-opus-5-5"} {
		t.Run(modelID, func(t *testing.T) {
			testAnthropicManagedEffortRequest(t, modelID)
		})
	}
}

func testAnthropicManagedEffortRequest(t *testing.T, modelID string) {
	type capturedRequest struct {
		body map[string]any
		beta string
	}
	captured := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode request: %v\n%s", err, body)
		}
		captured <- capturedRequest{body: payload, beta: request.Header.Get("anthropic-beta")}
		http.Error(writer, `{"error":{"type":"invalid_request_error","message":"captured"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	providers, err := NewProviders(Anthropic{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model(modelID)
	if !ok {
		t.Fatalf("%s missing", modelID)
	}
	temperature := 0.5
	stream := providers.Stream(context.Background(), model, Request{
		Messages: []Message{
			UserMessage{Content: []InputContent{TextInput{Text: "hello"}}},
			AssistantMessage{Provider: "anthropic", Model: modelID, Content: []AssistantContent{TextContent{Text: "hello"}}},
			UserMessage{Content: []InputContent{TextInput{Text: "continue"}}},
		},
		Reasoning:   "medium",
		Temperature: &temperature,
	})
	for range stream.Events() {
	}

	request := <-captured
	if _, ok := request.body["temperature"]; ok {
		t.Fatalf("managed effort request included temperature: %#v", request.body)
	}
	thinking, _ := request.body["thinking"].(map[string]any)
	binding, _ := thinking["block_binding"].(map[string]any)
	if thinking["type"] != "adaptive" || thinking["display"] != "summarized" || binding["prefix_mismatch_behavior"] != "drop_block" {
		t.Fatalf("thinking = %#v", thinking)
	}
	output, _ := request.body["output_config"].(map[string]any)
	if output["effort"] != "high" {
		t.Fatalf("output_config = %#v", output)
	}
	messages, _ := request.body["messages"].([]any)
	if historicalConfig, _ := messages[1].(map[string]any); historicalConfig["role"] != "system" {
		t.Fatalf("historical managed output config missing: %#v", messages)
	}
	last, _ := messages[len(messages)-1].(map[string]any)
	if last["role"] != "system" {
		t.Fatalf("managed messages = %#v", messages)
	}
	lastOutput, _ := last["output_config"].(map[string]any)
	if lastOutput["effort"] != "high" {
		t.Fatalf("managed output config = %#v", last)
	}
	for _, beta := range []string{anthropicMidConversationOutputConfigBeta, anthropicThinkingBindingControlsBeta} {
		if !strings.Contains(request.beta, beta) {
			t.Fatalf("anthropic-beta %q missing %q", request.beta, beta)
		}
	}
}

func TestAnthropicLongContextBetaFeatures(t *testing.T) {
	if got := anthropicBetaFeatures(false, Model{ContextWindow: 1_000_000}, false); got != anthropicLongContextBeta {
		t.Fatalf("long-context beta features = %q", got)
	}
	if got := anthropicBetaFeatures(false, Model{ContextWindow: 200_000}, false); got != "" {
		t.Fatalf("standard-context beta features = %q", got)
	}
}

func TestAnthropicAdaptiveEffortMapping(t *testing.T) {
	if got := anthropicEffort("minimal"); got != anthropic.OutputConfigEffortLow {
		t.Fatalf("minimal effort = %q", got)
	}
	if got := anthropicEffort("max"); got != anthropic.OutputConfigEffortMax {
		t.Fatalf("max effort = %q", got)
	}
	if got := anthropicBetaFeatures(true, Model{SupportsMidConversationEffort: true}, true); got != "claude-code-20250219,oauth-2025-04-20,"+anthropicFineGrainedToolsBeta+","+anthropicMidConversationOutputConfigBeta+","+anthropicThinkingBindingControlsBeta {
		t.Fatalf("OAuth beta features = %q", got)
	}
	if anthropicClaudeCodeVersion != "2.1.280" {
		t.Fatalf("Claude Code version = %q", anthropicClaudeCodeVersion)
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

type anthropicTestStore struct {
	mu     sync.Mutex
	record AnthropicCredentialRecord
	saves  int
}

func (s *anthropicTestStore) LoadAnthropicCredentials(context.Context) (AnthropicCredentialRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record, nil
}

func (s *anthropicTestStore) SaveAnthropicCredentials(_ context.Context, revision string, credentials AnthropicCredentials) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision != s.record.Revision {
		return "", ErrAnthropicCredentialsChanged
	}
	s.saves++
	s.record = AnthropicCredentialRecord{Credentials: credentials, Revision: "revision-two"}
	return s.record.Revision, nil
}

type anthropicTestRefresher struct {
	mu    sync.Mutex
	calls int
}

func (r *anthropicTestRefresher) Refresh(context.Context, anthropicoauth.Credentials) (anthropicoauth.Credentials, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return anthropicoauth.Credentials{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func TestAnthropicCredentialManagerSerializesRefresh(t *testing.T) {
	now := time.Now()
	store := &anthropicTestStore{record: AnthropicCredentialRecord{Credentials: AnthropicCredentials{
		AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: now.Add(-time.Minute),
	}, Revision: "revision-one"}}
	refresher := &anthropicTestRefresher{}
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	manager := &anthropicCredentialManager{store: store, refresher: refresher, now: func() time.Time { return now }, gate: gate}

	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			credentials, err := manager.resolve(context.Background())
			if err != nil {
				t.Errorf("resolve: %v", err)
				return
			}
			if credentials.AccessToken != "new-access" {
				t.Errorf("access token = %q", credentials.AccessToken)
			}
		}()
	}
	wait.Wait()
	if refresher.calls != 1 || store.saves != 1 {
		t.Fatalf("refresh calls = %d, saves = %d", refresher.calls, store.saves)
	}
}
