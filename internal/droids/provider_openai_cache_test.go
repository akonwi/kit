package droids

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

func openAIViaTestServer(baseURL string) option.RequestOption {
	target, _ := url.Parse(baseURL)
	return option.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "api.openai.com" {
			return nil, fmt.Errorf("request to %s, want api.openai.com", request.URL)
		}
		request = request.Clone(request.Context())
		request.URL.Scheme, request.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(request)
	})})
}

func captureOpenAIResponses(t *testing.T, configure func(baseURL string) OpenAI, selector string, request Request) map[string]any {
	t.Helper()
	captured := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		captured <- payload
		http.Error(writer, `{"error":{"message":"captured","type":"invalid_request_error"}}`, http.StatusBadRequest)
	}))
	defer server.Close()
	config := configure(server.URL)
	providers, err := NewProviders(config)
	if err != nil {
		t.Fatal(err)
	}
	model, err := providers.Resolve(selector)
	if err != nil {
		t.Fatal(err)
	}
	stream := providers.Stream(context.Background(), model, request)
	for range stream.Events() {
	}
	select {
	case body := <-captured:
		return body
	default:
		t.Fatalf("no request reached the server; result = %+v", stream.Result())
		return nil
	}
}

func TestOpenAIPromptCacheControlsAreEndpointAndModelSpecific(t *testing.T) {
	reasoning := AssistantMessage{
		Provider: "openai", Model: "gpt-6-sol", StopReason: StopReasonStop,
		Content: []AssistantContent{
			ThinkingContent{Thinking: "checked", Signature: `{"id":"rs_1","type":"reasoning","encrypted_content":"secret"}`},
			TextContent{Text: "done", Signature: `{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[]}]}`},
		},
	}
	request := Request{
		SystemPrompt: "Be concise.",
		Messages: []Message{
			UserMessage{Content: []InputContent{TextInput{Text: "Look it up"}}},
			reasoning,
			UserMessage{Content: []InputContent{TextInput{Text: "Again"}}},
		},
	}
	long := func() PromptCacheRetention { return PromptCacheLong }
	public := func(baseURL string) OpenAI {
		return OpenAI{APIKey: "key", PromptCacheRetention: long, Options: []option.RequestOption{openAIViaTestServer(baseURL)}}
	}
	gateway := func(baseURL string) OpenAI {
		return OpenAI{APIKey: "key", BaseURL: baseURL, PromptCacheRetention: long}
	}

	t.Run("gpt-5.6 boundary and ttl", func(t *testing.T) {
		body := captureOpenAIResponses(t, public, "openai/gpt-6-sol", request)
		if body["store"] != false {
			t.Fatalf("store = %#v, want false", body["store"])
		}
		if _, sent := body["previous_response_id"]; sent {
			t.Fatalf("previous_response_id = %#v", body["previous_response_id"])
		}
		if _, sent := body["instructions"]; sent {
			t.Fatalf("instructions duplicated the developer boundary: %#v", body["instructions"])
		}
		if _, sent := body["prompt_cache_retention"]; sent {
			t.Fatalf("deprecated retention sent with ttl controls: %#v", body["prompt_cache_retention"])
		}
		if _, sent := body["prompt_cache_key"]; sent {
			t.Fatalf("prompt_cache_key = %#v", body["prompt_cache_key"])
		}
		options := body["prompt_cache_options"].(map[string]any)
		if options["mode"] != "implicit" || options["ttl"] != "30m" || len(options) != 2 {
			t.Fatalf("prompt_cache_options = %#v", options)
		}
		include := body["include"].([]any)
		if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
			t.Fatalf("include = %#v", include)
		}
		input := body["input"].([]any)
		developer := input[0].(map[string]any)
		content := developer["content"].([]any)
		block := content[0].(map[string]any)
		if developer["role"] != "developer" || block["type"] != "input_text" || block["text"] != "Be concise." {
			t.Fatalf("developer boundary = %#v", developer)
		}
		if breakpoint := block["prompt_cache_breakpoint"].(map[string]any); breakpoint["mode"] != "explicit" || len(breakpoint) != 1 {
			t.Fatalf("breakpoint = %#v", breakpoint)
		}
		replayed := input[2].(map[string]any)
		if replayed["type"] != "reasoning" || replayed["encrypted_content"] != "secret" {
			t.Fatalf("same-model replay = %#v", replayed)
		}
		if input[1].(map[string]any)["role"] != "user" || input[3].(map[string]any)["type"] != "message" {
			t.Fatalf("input order = %#v", input)
		}
	})

	t.Run("model change drops encrypted reasoning without stored transcript", func(t *testing.T) {
		body := captureOpenAIResponses(t, public, "openai/gpt-6-luna", request)
		if body["store"] != false || body["prompt_cache_options"].(map[string]any)["ttl"] != "30m" {
			t.Fatalf("target cache controls = %#v", body)
		}
		encoded, err := json.Marshal(body["input"])
		if err != nil {
			t.Fatal(err)
		}
		text := string(encoded)
		if strings.Contains(text, "encrypted_content") || strings.Contains(text, `"type":"reasoning"`) {
			t.Fatalf("cross-model replay kept encrypted reasoning: %s", text)
		}
		if !strings.Contains(text, `"content":"done"`) || !strings.Contains(text, `"role":"assistant"`) || !strings.Contains(text, `"prompt_cache_breakpoint"`) {
			t.Fatalf("cross-model replay = %s", text)
		}
	})

	t.Run("earlier extended retention", func(t *testing.T) {
		body := captureOpenAIResponses(t, public, "openai/gpt-5.4", request)
		if body["instructions"] != "Be concise." || body["prompt_cache_retention"] != "24h" {
			t.Fatalf("retention request = %#v", body)
		}
		if _, sent := body["prompt_cache_options"]; sent {
			t.Fatalf("prompt_cache_options = %#v", body["prompt_cache_options"])
		}
		encoded, _ := json.Marshal(body["input"])
		if strings.Contains(string(encoded), "prompt_cache_breakpoint") {
			t.Fatalf("earlier model received a breakpoint: %s", encoded)
		}
	})

	t.Run("short retention is in_memory", func(t *testing.T) {
		body := captureOpenAIResponses(t, func(baseURL string) OpenAI {
			return OpenAI{APIKey: "key", Options: []option.RequestOption{openAIViaTestServer(baseURL)}}
		}, "openai/gpt-4.1", Request{SystemPrompt: "Be concise.", Messages: request.Messages[:1]})
		if body["prompt_cache_retention"] != "in_memory" || body["instructions"] != "Be concise." {
			t.Fatalf("short retention = %#v", body["prompt_cache_retention"])
		}
	})

	t.Run("gpt-5.5 short omits unsupported in_memory", func(t *testing.T) {
		body := captureOpenAIResponses(t, func(baseURL string) OpenAI {
			return OpenAI{APIKey: "key", Options: []option.RequestOption{openAIViaTestServer(baseURL)}}
		}, "openai/gpt-5.5", Request{SystemPrompt: "Be concise.", Messages: request.Messages[:1]})
		if _, sent := body["prompt_cache_retention"]; sent || body["instructions"] != "Be concise." {
			t.Fatalf("gpt-5.5 short = %#v", body)
		}
		longBody := captureOpenAIResponses(t, public, "openai/gpt-5.5", Request{SystemPrompt: "Be concise.", Messages: request.Messages[:1]})
		if longBody["prompt_cache_retention"] != "24h" {
			t.Fatalf("gpt-5.5 long = %#v", longBody["prompt_cache_retention"])
		}
	})

	t.Run("unlisted model and gateway omit controls", func(t *testing.T) {
		body := captureOpenAIResponses(t, public, "openai/gpt-5-mini", request)
		if _, sent := body["prompt_cache_options"]; sent {
			t.Fatalf("unlisted model options = %#v", body["prompt_cache_options"])
		}
		if _, sent := body["prompt_cache_retention"]; sent || body["instructions"] != "Be concise." {
			t.Fatalf("unlisted model retention = %#v", body)
		}
		gatewayBody := captureOpenAIResponses(t, gateway, "openai/gpt-6-sol", request)
		if _, sent := gatewayBody["prompt_cache_options"]; sent || gatewayBody["instructions"] != "Be concise." {
			t.Fatalf("gateway controls = %#v", gatewayBody)
		}
		encoded, _ := json.Marshal(gatewayBody["input"])
		if strings.Contains(string(encoded), "prompt_cache_breakpoint") {
			t.Fatalf("gateway received a breakpoint: %s", encoded)
		}
	})
}

func TestOpenAIPromptCacheIgnoresCatalogWritePrice(t *testing.T) {
	priced := Model{ID: "gpt-5.4", API: ModelAPIOpenAIResponses, BaseURL: defaultOpenAIBaseURL, Cost: Cost{CacheWrite: 9}}
	if openAIPromptCacheOptionsModel(priced.ID) {
		t.Fatal("catalog cache_write price selected prompt_cache_options")
	}
	var params responses.ResponseNewParams
	applyOpenAIPromptCache(priced, PromptCacheLong, &params)
	if params.PromptCacheOptions.Mode != "" || params.PromptCacheOptions.Ttl != "" {
		t.Fatalf("options inferred from price: %#v", params.PromptCacheOptions)
	}
	if params.PromptCacheRetention != responses.ResponseNewParamsPromptCacheRetention24h {
		t.Fatalf("retention = %q", params.PromptCacheRetention)
	}

	unpriced := Model{ID: "gpt-6-sol", API: ModelAPIOpenAIResponses, BaseURL: defaultOpenAIBaseURL}
	applyOpenAIPromptCache(unpriced, PromptCacheShort, &params)
	if params.PromptCacheOptions.Ttl != "30m" || params.PromptCacheOptions.Mode != "implicit" {
		t.Fatalf("missing catalog price disabled options: %#v", params.PromptCacheOptions)
	}

	empty := responses.ResponseNewParams{}
	applyOpenAIPromptCache(unpriced, PromptCacheLong, &empty)
	if empty.Instructions.Valid() || len(empty.Input.OfInputItemList) != 0 || empty.PromptCacheRetention != "" {
		t.Fatalf("empty prompt changed retention or input: %#v", empty)
	}
	if empty.PromptCacheOptions.Ttl != "30m" {
		t.Fatal("empty system prompt omitted ttl")
	}
}

func TestOpenAIUsageProjectsCacheWriteBilling(t *testing.T) {
	var response responses.Response
	if err := json.Unmarshal([]byte(`{
		"id":"resp_usage","model":"gpt-6-sol","status":"completed","output":[],
		"usage":{"input_tokens":20,"input_tokens_details":{"cached_tokens":5,"cache_write_tokens":7},"output_tokens":3,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":23}
	}`), &response); err != nil {
		t.Fatal(err)
	}
	message := assembleResponse(Model{ID: "gpt-6-sol", Provider: "openai"}, response)
	if message.Usage.Input != 8 || message.Usage.CacheRead != 5 || message.Usage.CacheWrite != 7 || message.Usage.CacheWrite1h != 0 || message.Usage.Output != 3 || message.Usage.Reasoning != 1 || message.Usage.TotalTokens != 23 {
		t.Fatalf("usage = %#v", message.Usage)
	}
	model, ok := OpenAIModel("gpt-6-sol")
	if !ok {
		t.Fatal("gpt-6-sol missing")
	}
	calculateCost(model, &message.Usage)
	want := UsageCost{
		Input:      model.Cost.Input / 1_000_000 * 8,
		Output:     model.Cost.Output / 1_000_000 * 3,
		CacheRead:  model.Cost.CacheRead / 1_000_000 * 5,
		CacheWrite: model.Cost.CacheWrite / 1_000_000 * 7,
	}
	want.Total = want.Input + want.Output + want.CacheRead + want.CacheWrite
	got := message.Usage.Cost
	for _, pair := range []struct {
		name string
		got  float64
		want float64
	}{
		{"input", got.Input, want.Input}, {"output", got.Output, want.Output},
		{"cache read", got.CacheRead, want.CacheRead}, {"cache write", got.CacheWrite, want.CacheWrite},
		{"total", got.Total, want.Total},
	} {
		if math.Abs(pair.got-pair.want) > 1e-12 {
			t.Fatalf("%s cost = %v, want %v", pair.name, pair.got, pair.want)
		}
	}
	anthropicLongWrite := 2 * model.Cost.Input / 1_000_000 * 7
	if math.Abs(got.CacheWrite-anthropicLongWrite) < 1e-12 {
		t.Fatalf("cache write used Anthropic one-hour pricing %v", anthropicLongWrite)
	}

	over := assembleResponse(Model{ID: "gpt-6-sol", Provider: "openai"}, responses.Response{})
	over.Usage = openAIUsage(responses.ResponseUsage{InputTokens: 5, InputTokensDetails: responses.ResponseUsageInputTokensDetails{CachedTokens: 4, CacheWriteTokens: 3}})
	if err := validateUsageTokens(over.Usage); err == nil {
		t.Fatalf("overlapping breakdown accepted: %#v", over.Usage)
	}
}
