package droids

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	codexauth "github.com/akonwi/kit/internal/droids/openaicodex"
)

func TestOpenAICodexStreamsResponsesDialect(t *testing.T) {
	requestBody := make(chan map[string]any, 1)
	requestHeaders := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/backend-api/codex/responses" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer request.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requestBody <- body
		requestHeaders <- request.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"sequence_number\":1,\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"Hello from Codex\"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"sequence_number\":2,\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hello from Codex\",\"annotations\":[]}]}}\n\n")
		// Codex may omit output here, so the provider must use output_item.done.
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"sequence_number\":3,\"response\":{\"id\":\"resp_codex\",\"object\":\"response\",\"created_at\":1,\"model\":\"gpt-5.6-sol\",\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"input_tokens_details\":{\"cached_tokens\":4},\"output_tokens\":3,\"output_tokens_details\":{\"reasoning_tokens\":1},\"total_tokens\":13}}}\n\n")
	}))
	defer server.Close()

	providers, err := NewProviders(OpenAICodex{
		Credentials: OpenAICodexCredentials{
			AccessToken: "access-token", AccountID: "account-1", FedRAMP: true,
		},
		Originator: "kit",
		HTTPClient: server.Client(),
		responsesBaseURL: server.URL +
			"/backend-api/codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("openai-codex/gpt-5.6-sol")
	if !ok {
		t.Fatal("Codex model did not resolve")
	}
	batchTool := MustTool(Tool[struct{}]{
		Name: "batch",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{"type": "array", "minItems": 1},
			},
		},
		Execute: testNoopTool[struct{}],
	})
	stream := providers.Stream(context.Background(), model, Request{
		Messages:     []Message{UserMessage{Content: []InputContent{TextInput{Text: "hello"}}}},
		Reasoning:    "minimal",
		SystemPrompt: "Be concise.",
		Tools:        []ToolSchema{batchTool.schema()},
	})
	var events []StreamEvent
	for event := range stream.Events() {
		events = append(events, event)
	}
	message := stream.Result()
	if message.StopReason != StopReasonStop || message.Text() != "Hello from Codex" || message.ResponseID != "resp_codex" || message.ProviderScope == "" {
		t.Fatalf("message = %#v", message)
	}
	if len(message.Content) != 1 || !strings.Contains(message.Content[0].(TextContent).Signature, `"phase":"final_answer"`) {
		t.Fatalf("content = %#v", message.Content)
	}
	if len(events) != 4 {
		t.Fatalf("events = %#v, want start + text start + delta + done", events)
	}

	headers := <-requestHeaders
	if headers.Get("Authorization") != "Bearer access-token" || headers.Get("chatgpt-account-id") != "account-1" {
		t.Fatalf("auth headers = %#v", headers)
	}
	if headers.Get("OpenAI-Beta") != "responses=experimental" || headers.Get("originator") != "kit" || headers.Get("User-Agent") != "droids/openai-codex" || headers.Get("X-OpenAI-Fedramp") != "true" {
		t.Fatalf("Codex headers = %#v", headers)
	}
	body := <-requestBody
	if body["model"] != "gpt-5.6-sol" || body["store"] != false || body["stream"] != true || body["instructions"] != "Be concise." {
		t.Fatalf("request controls = %#v", body)
	}
	if body["parallel_tool_calls"] != true || body["tool_choice"] != "auto" {
		t.Fatalf("Codex request controls = %#v", body)
	}
	if _, sent := body["max_output_tokens"]; sent {
		t.Fatalf("Codex request included unsupported max_output_tokens: %#v", body)
	}
	if body["text"].(map[string]any)["verbosity"] != "low" || body["reasoning"].(map[string]any)["effort"] != "low" {
		t.Fatalf("Codex request dialect = %#v", body)
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	parameters := tool["parameters"].(map[string]any)
	properties := parameters["properties"].(map[string]any)
	if minimum := properties["items"].(map[string]any)["minItems"]; minimum != float64(1) {
		t.Fatalf("Codex numeric schema keyword = %#v (%T), want JSON number", minimum, minimum)
	}
}

func TestOpenAICodexRejectsExplicitMaxTokens(t *testing.T) {
	providers, err := NewProviders(OpenAICodex{Credentials: staticCodexCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := providers.Model("openai-codex/gpt-5.6-sol")
	stream := providers.Stream(context.Background(), model, Request{MaxTokens: 32})
	for range stream.Events() {
	}
	if got := stream.Result(); got.StopReason != StopReasonError || !strings.Contains(got.ErrorMessage, "does not support") {
		t.Fatalf("result = %#v", got)
	}
	if _, err := resolveRequestMaxTokens(model, 32, ""); err == nil {
		t.Fatal("runtime accepted an unenforceable Codex MaxTokens limit")
	}
}

func TestOpenAICodexAcceptsResponseDoneDialect(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := newCodexResponsesServer(t, requests,
		`{"type":"response.done","sequence_number":1,"response":{"id":"resp_done","model":"gpt-5.6-sol","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	defer server.Close()
	providers, model := testOpenAICodexProvider(t, server)
	stream := providers.Stream(context.Background(), model, Request{})
	for range stream.Events() {
	}
	if got := stream.Result(); got.ResponseID != "resp_done" || got.Text() != "done" || got.StopReason != StopReasonStop {
		t.Fatalf("result = %#v", got)
	}
	<-requests
}

func TestOpenAICodexClassifiesNestedSSEError(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := newCodexResponsesServer(t, requests,
		`{"error":{"code":"usage_limit_reached","message":"subscription limit reached"}}`,
	)
	defer server.Close()
	providers, model := testOpenAICodexProvider(t, server)
	stream := providers.Stream(context.Background(), model, Request{})
	for range stream.Events() {
	}
	if got := stream.Result(); got.ErrorKind != ErrorUsageLimit || got.ErrorMessage != "subscription limit reached" {
		t.Fatalf("result = %#v", got)
	}
	<-requests
}

func TestOpenAICodexRejectsCrossAccountTranscriptReplay(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := newCodexResponsesServer(t, requests)
	defer server.Close()
	providers, model := testOpenAICodexProvider(t, server)
	for _, scope := range []string{"", openAICodexProviderScope("another-account")} {
		stream := providers.Stream(context.Background(), model, Request{Messages: []Message{
			AssistantMessage{
				Provider: "openai-codex", Model: model.ID, ProviderScope: scope,
				Content: []AssistantContent{TextContent{Text: "prior response"}}, StopReason: StopReasonStop,
			},
			UserMessage{Content: []InputContent{TextInput{Text: "continue"}}},
		}})
		for range stream.Events() {
		}
		if got := stream.Result(); got.ErrorKind != ErrorAuthentication {
			t.Fatalf("scope %q result = %#v", scope, got)
		}
	}
	select {
	case request := <-requests:
		t.Fatalf("cross-account transcript reached provider: %#v", request)
	default:
	}
}

func TestOpenAICodexRequestsSummaryAtProviderDefaultReasoning(t *testing.T) {
	model, _ := OpenAICodexModel("gpt-5.6-sol")
	params, err := buildOpenAICodexResponseParams(model, Request{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning = %#v, want provider-default effort with auto summary", body["reasoning"])
	}
	if _, explicit := reasoning["effort"]; explicit {
		t.Fatalf("reasoning effort = %#v, want provider default", reasoning["effort"])
	}
}

func TestOpenAICodexSendsExplicitNoneReasoning(t *testing.T) {
	model, _ := OpenAICodexModel("gpt-5.6-sol")
	params, err := buildOpenAICodexResponseParams(model, Request{Reasoning: "off"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "none" {
		t.Fatalf("reasoning = %#v", body["reasoning"])
	}
}

func TestOpenAICodexDoesNotRetryModelRequests(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, `{"error":{"code":"server_error","message":"unavailable"}}`)
	}))
	defer server.Close()
	providers, err := NewProviders(OpenAICodex{
		Credentials:      staticCodexCredentials(),
		HTTPClient:       server.Client(),
		responsesBaseURL: server.URL + "/backend-api/codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := providers.Model("openai-codex/gpt-5.6-sol")
	stream := providers.Stream(context.Background(), model, Request{})
	for range stream.Events() {
	}
	if requests.Load() != 1 {
		t.Fatalf("model request count = %d, want 1", requests.Load())
	}
}

func TestOpenAICodexRetainsAndReplaysToolItemID(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := newCodexResponsesServer(t, requests,
		`{"type":"response.output_item.done","sequence_number":1,"output_index":0,"item":{"id":"fc_item_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}","status":"completed"}}`,
		`{"type":"response.completed","sequence_number":2,"response":{"id":"resp_tool","model":"gpt-5.6-sol","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	defer server.Close()
	providers, model := testOpenAICodexProvider(t, server)
	stream := providers.Stream(context.Background(), model, Request{Messages: []Message{UserMessage{Content: []InputContent{TextInput{Text: "lookup"}}}}})
	for range stream.Events() {
	}
	message := stream.Result()
	calls := message.ToolCalls()
	if len(calls) != 1 || calls[0].Signature != "fc_item_1" {
		t.Fatalf("calls = %#v", calls)
	}
	input, err := toOpenAIInputForProvider([]Message{message}, "openai-codex")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(replayed), `"id":"fc_item_1"`) {
		t.Fatalf("replayed input = %s", replayed)
	}
	<-requests
}

func TestOpenAIReplayDropsOpaqueSignaturesAcrossModels(t *testing.T) {
	message := AssistantMessage{
		Provider: "openai-codex", Model: "gpt-5.6-sol", StopReason: StopReasonToolUse,
		Content: []AssistantContent{
			ThinkingContent{Thinking: "reasoning", Signature: `{"id":"rs_1","type":"reasoning","encrypted_content":"secret"}`},
			TextContent{Text: "text", Signature: `{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"text","annotations":[]}]}`},
			ToolCall{ID: "call_1", Name: "lookup", Arguments: []byte(`{}`), Signature: "fc_item_1"},
		},
	}
	target, _ := OpenAICodexModel("gpt-5.4")
	input, err := toOpenAIInputForModel([]Message{message}, target)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	if strings.Contains(body, "rs_1") || strings.Contains(body, "msg_1") || strings.Contains(body, "fc_item_1") {
		t.Fatalf("cross-model replay retained opaque signatures: %s", body)
	}
	if !strings.Contains(body, `"content":"text"`) || !strings.Contains(body, `"call_id":"call_1"`) {
		t.Fatalf("cross-model replay dropped neutral content: %s", body)
	}
}

func TestOpenAICodexDirectCredentialsServeRepeatedCalls(t *testing.T) {
	requests := make(chan map[string]any, 2)
	server := newCodexResponsesServer(t, requests,
		`{"type":"response.completed","sequence_number":1,"response":{"id":"resp","model":"gpt-5.6-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	defer server.Close()
	providers, err := NewProviders(OpenAICodex{
		Credentials:      staticCodexCredentials(),
		HTTPClient:       server.Client(),
		responsesBaseURL: server.URL + "/backend-api/codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := providers.Model("openai-codex/gpt-5.6-sol")
	for range 2 {
		stream := providers.Stream(context.Background(), model, Request{})
		for range stream.Events() {
		}
		<-requests
	}
}

func TestOpenAICodexAutomaticallyRefreshesInMemory(t *testing.T) {
	requestHeaders := make(chan http.Header, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestHeaders <- request.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"type":"response.completed","sequence_number":1,"response":{"id":"resp","model":"gpt-5.6-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`+"\n\n")
	}))
	defer server.Close()
	fixedNow := time.Unix(1_800_000_000, 0)
	refresher := &fakeOpenAICodexRefresher{refresh: func(_ context.Context, previous OpenAICodexCredentials) (OpenAICodexCredentials, error) {
		if previous.RefreshToken != "refresh-old" {
			t.Fatalf("refresh credentials = %#v", previous)
		}
		return OpenAICodexCredentials{
			AccessToken: "access-new", RefreshToken: "refresh-new", AccountID: "account-1",
			ExpiresAt: fixedNow.Add(time.Hour),
		}, nil
	}}
	providers, err := NewProviders(OpenAICodex{
		Credentials: OpenAICodexCredentials{
			AccessToken: "access-old", RefreshToken: "refresh-old", AccountID: "account-1",
			ExpiresAt: fixedNow,
		},
		HTTPClient:       server.Client(),
		responsesBaseURL: server.URL + "/backend-api/codex",
		now:              func() time.Time { return fixedNow },
		refresher:        refresher,
	})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := providers.Model("openai-codex/gpt-5.6-sol")
	for range 2 {
		stream := providers.Stream(context.Background(), model, Request{})
		for range stream.Events() {
		}
		if got := (<-requestHeaders).Get("Authorization"); got != "Bearer access-new" {
			t.Fatalf("authorization = %q", got)
		}
	}
	if refresher.calls.Load() != 1 {
		t.Fatalf("refresh calls = %d", refresher.calls.Load())
	}
}

func TestOpenAICodexCoalescesConcurrentRefresh(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	started := make(chan struct{})
	release := make(chan struct{})
	refresher := &fakeOpenAICodexRefresher{refresh: func(ctx context.Context, previous OpenAICodexCredentials) (OpenAICodexCredentials, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return OpenAICodexCredentials{}, ctx.Err()
		}
		previous.AccessToken = "fresh"
		previous.ExpiresAt = fixedNow.Add(time.Hour)
		return previous, nil
	}}
	manager := &openAICodexCredentialManager{
		current: OpenAICodexCredentials{
			AccessToken: "stale", RefreshToken: "refresh", AccountID: "account",
			ExpiresAt: fixedNow,
		},
		loaded: true, refresher: refresher, now: func() time.Time { return fixedNow },
	}
	const callers = 8
	results := make(chan error, callers)
	for range callers {
		go func() {
			credentials, err := manager.resolve(context.Background())
			if err == nil && credentials.AccessToken != "fresh" {
				err = fmt.Errorf("access token = %q", credentials.AccessToken)
			}
			results <- err
		}()
	}
	<-started
	close(release)
	for range callers {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if refresher.calls.Load() != 1 {
		t.Fatalf("refresh calls = %d", refresher.calls.Load())
	}
}

func TestOpenAICodexCallerCancellationDoesNotCancelSharedRefresh(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	started := make(chan struct{})
	release := make(chan struct{})
	refresher := &fakeOpenAICodexRefresher{refresh: func(ctx context.Context, previous OpenAICodexCredentials) (OpenAICodexCredentials, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return OpenAICodexCredentials{}, ctx.Err()
		}
		previous.AccessToken = "fresh"
		previous.ExpiresAt = fixedNow.Add(time.Hour)
		return previous, nil
	}}
	manager := &openAICodexCredentialManager{
		current: OpenAICodexCredentials{
			AccessToken: "stale", RefreshToken: "refresh", AccountID: "account",
			ExpiresAt: fixedNow,
		},
		loaded: true, refresher: refresher, now: func() time.Time { return fixedNow },
	}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := manager.resolve(ctx)
		first <- err
	}()
	<-started
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter error = %v", err)
	}
	close(release)
	credentials, err := manager.resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken != "fresh" || refresher.calls.Load() != 1 {
		t.Fatalf("credentials=%#v refreshes=%d", credentials, refresher.calls.Load())
	}
}

func TestOpenAICodexPersistsRotatedCredentials(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	store := &fakeOpenAICodexCredentialStore{credentials: OpenAICodexCredentials{
		AccessToken: "stale", RefreshToken: "refresh-old", AccountID: "account",
		ExpiresAt: fixedNow,
	}, revision: "stored"}
	refresher := &fakeOpenAICodexRefresher{refresh: func(_ context.Context, previous OpenAICodexCredentials) (OpenAICodexCredentials, error) {
		previous.AccessToken = "fresh"
		previous.RefreshToken = "refresh-new"
		previous.ExpiresAt = fixedNow.Add(time.Hour)
		return previous, nil
	}}
	manager := &openAICodexCredentialManager{
		store: store, refresher: refresher, now: func() time.Time { return fixedNow },
	}
	credentials, err := manager.resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken != "fresh" || store.saved.RefreshToken != "refresh-new" || store.saves.Load() != 1 {
		t.Fatalf("credentials = %#v, saved = %#v", credentials, store.saved)
	}
}

func TestOpenAICodexReloadsStoredLoginAndLogoutGenerations(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	store := &fakeOpenAICodexCredentialStore{
		credentials: OpenAICodexCredentials{
			AccessToken: "account-a-access", AccountID: "account-a",
			ExpiresAt: fixedNow.Add(time.Hour),
		},
		revision: "login-a",
	}
	manager := &openAICodexCredentialManager{
		store: store, refresher: &fakeOpenAICodexRefresher{},
		now: func() time.Time { return fixedNow },
	}
	credentials, err := manager.resolve(context.Background())
	if err != nil || credentials.AccountID != "account-a" {
		t.Fatalf("first resolve = %#v, %v", credentials, err)
	}
	store.credentials = OpenAICodexCredentials{
		AccessToken: "account-b-access", AccountID: "account-b",
		ExpiresAt: fixedNow.Add(time.Hour),
	}
	store.revision = "login-b"
	credentials, err = manager.resolve(context.Background())
	if err != nil || credentials.AccountID != "account-b" {
		t.Fatalf("replacement resolve = %#v, %v", credentials, err)
	}
	store.credentials = OpenAICodexCredentials{}
	store.revision = ""
	if _, err := manager.resolve(context.Background()); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("logout resolve error = %v", err)
	}
}

func TestOpenAICodexDoesNotOverwriteConcurrentCredentialReplacement(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	store := &fakeOpenAICodexCredentialStore{
		credentials: OpenAICodexCredentials{
			AccessToken: "stale-a", RefreshToken: "refresh-a", AccountID: "account-a",
			ExpiresAt: fixedNow,
		},
		revision: "login-a",
	}
	refresher := &fakeOpenAICodexRefresher{refresh: func(_ context.Context, previous OpenAICodexCredentials) (OpenAICodexCredentials, error) {
		store.credentials = OpenAICodexCredentials{
			AccessToken: "fresh-b", RefreshToken: "refresh-b", AccountID: "account-b",
			ExpiresAt: fixedNow.Add(time.Hour),
		}
		store.revision = "login-b"
		previous.AccessToken = "fresh-a"
		previous.ExpiresAt = fixedNow.Add(time.Hour)
		return previous, nil
	}}
	manager := &openAICodexCredentialManager{
		store: store, refresher: refresher, now: func() time.Time { return fixedNow },
	}
	credentials, err := manager.resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccountID != "account-b" || store.credentials.AccountID != "account-b" {
		t.Fatalf("resolved = %#v, stored = %#v", credentials, store.credentials)
	}
	if store.saved.AccountID == "account-a" {
		t.Fatal("stale refreshed credentials overwrote the replacement login")
	}
}

func TestOpenAICodexDoesNotRecreateConcurrentLogout(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	store := &fakeOpenAICodexCredentialStore{
		credentials: OpenAICodexCredentials{
			AccessToken: "stale", RefreshToken: "refresh", AccountID: "account",
			ExpiresAt: fixedNow,
		},
		revision: "login",
	}
	refresher := &fakeOpenAICodexRefresher{refresh: func(_ context.Context, previous OpenAICodexCredentials) (OpenAICodexCredentials, error) {
		store.credentials = OpenAICodexCredentials{}
		store.revision = ""
		previous.AccessToken = "fresh"
		previous.ExpiresAt = fixedNow.Add(time.Hour)
		return previous, nil
	}}
	manager := &openAICodexCredentialManager{
		store: store, refresher: refresher, now: func() time.Time { return fixedNow },
	}
	if _, err := manager.resolve(context.Background()); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("resolve after logout error = %v", err)
	}
	if openAICodexCredentialsConfigured(store.credentials) {
		t.Fatalf("logout was recreated as %#v", store.credentials)
	}
}

func TestOpenAICodexRetainsRotatedCredentialsWhenPersistenceFails(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	store := &fakeOpenAICodexCredentialStore{
		credentials: OpenAICodexCredentials{
			AccessToken: "stale", RefreshToken: "refresh-old", AccountID: "account",
			ExpiresAt: fixedNow,
		},
		revision: "stored",
		saveErr:  fmt.Errorf("disk unavailable with secret-refresh"),
	}
	refresher := &fakeOpenAICodexRefresher{refresh: func(_ context.Context, previous OpenAICodexCredentials) (OpenAICodexCredentials, error) {
		previous.AccessToken = "fresh"
		previous.RefreshToken = "refresh-new"
		previous.ExpiresAt = fixedNow.Add(time.Hour)
		return previous, nil
	}}
	manager := &openAICodexCredentialManager{
		store: store, refresher: refresher, now: func() time.Time { return fixedNow },
	}
	if _, err := manager.resolve(context.Background()); err == nil || strings.Contains(err.Error(), "secret-refresh") {
		t.Fatalf("first resolve error = %v", err)
	}
	store.saveErr = nil
	credentials, err := manager.resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken != "fresh" || store.saved.RefreshToken != "refresh-new" {
		t.Fatalf("credentials = %#v, saved = %#v", credentials, store.saved)
	}
	if refresher.calls.Load() != 1 || store.saves.Load() != 2 {
		t.Fatalf("refreshes=%d saves=%d", refresher.calls.Load(), store.saves.Load())
	}
}

func TestOpenAICodexRejectsRedirects(t *testing.T) {
	var leaked atomic.Bool
	attacker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		leaked.Store(true)
	}))
	defer attacker.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, attacker.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	providers, err := NewProviders(OpenAICodex{
		Credentials:      staticCodexCredentials(),
		HTTPClient:       redirector.Client(),
		responsesBaseURL: redirector.URL + "/backend-api/codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := providers.Model("openai-codex/gpt-5.6-sol")
	stream := providers.Stream(context.Background(), model, Request{})
	for range stream.Events() {
	}
	if leaked.Load() {
		t.Fatal("Codex bearer credential followed a redirect")
	}
	if stream.Result().StopReason != StopReasonError {
		t.Fatalf("result = %#v", stream.Result())
	}
}

func TestOpenAICodexClassifiesFailures(t *testing.T) {
	tests := []struct {
		status  int
		code    string
		message string
		want    ErrorKind
	}{
		{http.StatusUnauthorized, "", "unauthorized", ErrorAuthentication},
		{http.StatusForbidden, "", "workspace unavailable", ErrorEntitlement},
		{http.StatusTooManyRequests, "usage_limit_reached", "limit", ErrorUsageLimit},
		{http.StatusTooManyRequests, "rate_limit_exceeded", "slow down", ErrorRateLimit},
		{http.StatusServiceUnavailable, "server_error", "unavailable", ErrorTransport},
	}
	for _, tt := range tests {
		if got := classifyOpenAICodexError(tt.status, tt.code, tt.message); got != tt.want {
			t.Errorf("classify(%d, %q, %q) = %q, want %q", tt.status, tt.code, tt.message, got, tt.want)
		}
	}
}

func TestOpenAICodexClassifiesRefreshFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{name: "invalid grant", err: &codexauth.Error{StatusCode: 400, Code: "invalid_grant"}, want: ErrorAuthentication},
		{name: "bad request", err: &codexauth.Error{StatusCode: 400, Code: "invalid_request"}, want: ErrorProtocol},
		{name: "rate limit", err: &codexauth.Error{StatusCode: 429, Code: "rate_limit_exceeded"}, want: ErrorRateLimit},
		{name: "transport", err: fmt.Errorf("network unavailable"), want: ErrorTransport},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var credentialErr *OpenAICodexCredentialError
			if err := classifyOpenAICodexRefreshError(test.err); !errors.As(err, &credentialErr) || credentialErr.Kind != test.want {
				t.Fatalf("error = %#v, want kind %q", err, test.want)
			}
		})
	}
}

func TestOpenAICodexValidatesCredentialsAndContent(t *testing.T) {
	if _, err := NewProviders(OpenAICodex{}); err == nil {
		t.Fatal("missing credentials were accepted")
	}
	store := &fakeOpenAICodexCredentialStore{}
	if _, err := NewProviders(OpenAICodex{
		Credentials: staticCodexCredentials(), CredentialStore: store,
	}); err == nil {
		t.Fatal("direct credentials plus a store were accepted")
	}
	providers, err := NewProviders(OpenAICodex{CredentialStore: store})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := providers.Model("openai-codex/gpt-5.6-sol")
	stream := providers.Stream(context.Background(), model, Request{})
	for range stream.Events() {
	}
	if got := stream.Result(); got.ErrorKind != ErrorAuthentication || !strings.Contains(got.ErrorMessage, "not configured") {
		t.Fatalf("missing stored credential result = %#v", got)
	}
	for _, malformedStore := range []*fakeOpenAICodexCredentialStore{
		{credentials: staticCodexCredentials()},
		{revision: "phantom"},
	} {
		providers, err = NewProviders(OpenAICodex{CredentialStore: malformedStore})
		if err != nil {
			t.Fatal(err)
		}
		model, _ = providers.Model("openai-codex/gpt-5.6-sol")
		stream = providers.Stream(context.Background(), model, Request{})
		for range stream.Events() {
		}
		if got := stream.Result(); got.ErrorKind != ErrorProtocol {
			t.Fatalf("malformed store result = %#v", got)
		}
	}
	if _, err := NewProviders(OpenAICodex{Credentials: staticCodexCredentials(), Originator: "bad\nvalue"}); err == nil {
		t.Fatal("invalid originator was accepted")
	}

	providers, err = NewProviders(OpenAICodex{Credentials: OpenAICodexCredentials{
		AccessToken: "expired", AccountID: "account", ExpiresAt: time.Now(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	model, _ = providers.Model("openai-codex/gpt-5.6-sol")
	stream = providers.Stream(context.Background(), model, Request{})
	for range stream.Events() {
	}
	if got := stream.Result(); got.ErrorKind != ErrorAuthentication {
		t.Fatalf("expired credential result = %#v", got)
	}

	claims, _ := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "claimed-account"},
	})
	jwt := "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	if err := validateOpenAICodexAccess(OpenAICodexCredentials{AccessToken: jwt, AccountID: "different-account"}, time.Now()); err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("account mismatch error = %v", err)
	}

	if err := validateOpenAICodexContent(model, []Message{UserMessage{Content: []InputContent{NewFileInputData("doc.pdf", "application/pdf", []byte("pdf"))}}}); err == nil {
		t.Fatal("non-image file was accepted")
	}
	spark, _ := OpenAICodexModel("gpt-5.3-codex-spark")
	if err := validateOpenAICodexContent(spark, []Message{UserMessage{Content: []InputContent{NewFileInputData("", "image/png", []byte("image"))}}}); err == nil {
		t.Fatal("image was accepted by text-only model")
	}
}

func TestOpenAICodexNormalizesAccessAndIDTokenIdentity(t *testing.T) {
	fedRAMP := true
	credentials, err := normalizeOpenAICodexCredentials(OpenAICodexCredentials{
		AccessToken: testOpenAICodexJWT(t, "account-1", nil),
		IDToken:     testOpenAICodexJWT(t, "account-1", &fedRAMP),
	})
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccountID != "account-1" || !credentials.FedRAMP {
		t.Fatalf("credentials = %#v", credentials)
	}
	_, err = normalizeOpenAICodexCredentials(OpenAICodexCredentials{
		AccessToken: testOpenAICodexJWT(t, "account-1", nil),
		IDToken:     testOpenAICodexJWT(t, "account-2", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "different accounts") {
		t.Fatalf("identity conflict error = %v", err)
	}
}

func TestOpenAICodexRedactsCredentialStoreAndRefreshErrors(t *testing.T) {
	store := &fakeOpenAICodexCredentialStore{loadErr: fmt.Errorf("store failed while reading secret-token")}
	providers, err := NewProviders(OpenAICodex{CredentialStore: store})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := providers.Model("openai-codex/gpt-5.6-sol")
	stream := providers.Stream(context.Background(), model, Request{})
	for range stream.Events() {
	}
	got := stream.Result()
	if got.ErrorKind != ErrorTransport || strings.Contains(got.ErrorMessage, "secret-token") {
		t.Fatalf("store result = %#v", got)
	}

	fixedNow := time.Unix(1_800_000_000, 0)
	providers, err = NewProviders(OpenAICodex{
		Credentials: OpenAICodexCredentials{
			AccessToken: "stale", RefreshToken: "secret-refresh", AccountID: "account",
			ExpiresAt: fixedNow,
		},
		now: func() time.Time { return fixedNow },
		refresher: &fakeOpenAICodexRefresher{refresh: func(context.Context, OpenAICodexCredentials) (OpenAICodexCredentials, error) {
			return OpenAICodexCredentials{}, fmt.Errorf("refresh rejected secret-refresh")
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	model, _ = providers.Model("openai-codex/gpt-5.6-sol")
	stream = providers.Stream(context.Background(), model, Request{})
	for range stream.Events() {
	}
	got = stream.Result()
	if got.ErrorKind != ErrorTransport || strings.Contains(got.ErrorMessage, "secret-refresh") {
		t.Fatalf("refresh result = %#v", got)
	}
}

type fakeOpenAICodexRefresher struct {
	calls   atomic.Int32
	refresh func(context.Context, OpenAICodexCredentials) (OpenAICodexCredentials, error)
}

func (f *fakeOpenAICodexRefresher) Refresh(ctx context.Context, credentials OpenAICodexCredentials) (OpenAICodexCredentials, error) {
	f.calls.Add(1)
	return f.refresh(ctx, credentials)
}

type fakeOpenAICodexCredentialStore struct {
	credentials OpenAICodexCredentials
	saved       OpenAICodexCredentials
	revision    string
	loadErr     error
	saveErr     error
	loads       atomic.Int32
	saves       atomic.Int32
}

func (s *fakeOpenAICodexCredentialStore) LoadOpenAICodexCredentials(context.Context) (OpenAICodexCredentialRecord, error) {
	s.loads.Add(1)
	return OpenAICodexCredentialRecord{Credentials: s.credentials, Revision: s.revision}, s.loadErr
}

func (s *fakeOpenAICodexCredentialStore) SaveOpenAICodexCredentials(_ context.Context, expectedRevision string, credentials OpenAICodexCredentials) (string, error) {
	s.saves.Add(1)
	if expectedRevision == "" || expectedRevision != s.revision {
		return "", ErrOpenAICodexCredentialsChanged
	}
	if s.saveErr != nil {
		return "", s.saveErr
	}
	s.saved = credentials
	s.credentials = credentials
	s.revision = fmt.Sprintf("revision-%d", s.saves.Load())
	return s.revision, nil
}

func testOpenAICodexJWT(t *testing.T, accountID string, fedRAMP *bool) string {
	t.Helper()
	auth := map[string]any{"chatgpt_account_id": accountID}
	if fedRAMP != nil {
		auth["chatgpt_account_is_fedramp"] = *fedRAMP
	}
	claims, err := json.Marshal(map[string]any{"https://api.openai.com/auth": auth})
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
}

func staticCodexCredentials() OpenAICodexCredentials {
	return OpenAICodexCredentials{
		AccessToken: "access-token",
		AccountID:   "account-1",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
}

func testOpenAICodexProvider(t *testing.T, server *httptest.Server) (Providers, Model) {
	t.Helper()
	providers, err := NewProviders(OpenAICodex{
		Credentials:      staticCodexCredentials(),
		HTTPClient:       server.Client(),
		responsesBaseURL: server.URL + "/backend-api/codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("openai-codex/gpt-5.6-sol")
	if !ok {
		t.Fatal("Codex test model did not resolve")
	}
	return providers, model
}

func newCodexResponsesServer(t *testing.T, requests chan<- map[string]any, events ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/backend-api/codex/responses" {
			t.Errorf("request = %s %s, want POST /backend-api/codex/responses", request.Method, request.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer request.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}
