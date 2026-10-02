package droids

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// captureMessagesRequest streams request to selector through providers built
// by configure against a server that records the request body and fails it.
func captureMessagesRequest(t *testing.T, configure func(baseURL string) ProviderConfig, selector string, request Request) map[string]any {
	t.Helper()
	captured := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		captured <- payload
		http.Error(writer, `{"error":{"type":"invalid_request_error","message":"captured"}}`, http.StatusBadRequest)
	}))
	defer server.Close()
	providers, err := NewProviders(configure(server.URL))
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

func jsonValue(t *testing.T, value any) any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestAnthropicPromptCachingBreakpoints(t *testing.T) {
	tools := []ToolSchema{{Name: "read", Description: "Read a file", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}}
	fiveMinutes := map[string]any{"type": "ephemeral"}
	oneHour := map[string]any{"type": "ephemeral", "ttl": "1h"}
	retention := func(value PromptCacheRetention) func() PromptCacheRetention {
		return func() PromptCacheRetention { return value }
	}
	for _, test := range []struct {
		name       string
		config     Anthropic
		selector   string
		reasoning  string
		system     string
		wantSystem []any
		wantCache  any
	}{
		{
			name: "default lifetime", config: Anthropic{APIKey: "key"}, selector: "anthropic/claude-haiku-4-5", system: "Be concise.",
			wantSystem: []any{map[string]any{"type": "text", "text": "Be concise.", "cache_control": fiveMinutes}}, wantCache: fiveMinutes,
		},
		{
			name: "short retention", config: Anthropic{APIKey: "key", PromptCacheRetention: retention(PromptCacheShort)}, selector: "anthropic/claude-haiku-4-5", system: "Be concise.",
			wantSystem: []any{map[string]any{"type": "text", "text": "Be concise.", "cache_control": fiveMinutes}}, wantCache: fiveMinutes,
		},
		{
			name: "long retention", config: Anthropic{APIKey: "key", PromptCacheRetention: retention(PromptCacheLong)}, selector: "anthropic/claude-haiku-4-5", system: "Be concise.",
			wantSystem: []any{map[string]any{"type": "text", "text": "Be concise.", "cache_control": oneHour}}, wantCache: oneHour,
		},
		{
			name: "unknown retention", config: Anthropic{APIKey: "key", PromptCacheRetention: retention("forever")}, selector: "anthropic/claude-haiku-4-5", system: "Be concise.",
			wantSystem: []any{map[string]any{"type": "text", "text": "Be concise.", "cache_control": fiveMinutes}}, wantCache: fiveMinutes,
		},
		{
			name: "no system prompt", config: Anthropic{APIKey: "key"}, selector: "anthropic/claude-haiku-4-5",
			wantCache: fiveMinutes,
		},
		{
			name: "managed effort", config: Anthropic{APIKey: "key", PromptCacheRetention: retention(PromptCacheLong)}, selector: "anthropic/claude-opus-5-5", reasoning: "medium", system: "Be concise.",
			wantSystem: []any{map[string]any{"type": "text", "text": "Be concise.", "cache_control": oneHour}}, wantCache: oneHour,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := captureMessagesRequest(t, func(baseURL string) ProviderConfig {
				config := test.config
				config.BaseURL = baseURL
				return config
			}, test.selector, Request{
				SystemPrompt: test.system, Tools: tools, Reasoning: test.reasoning,
				Messages: []Message{UserMessage{Content: []InputContent{TextInput{Text: "hello"}}}},
			})
			if !reflect.DeepEqual(body["cache_control"], test.wantCache) {
				t.Fatalf("cache_control = %#v, want %#v", body["cache_control"], test.wantCache)
			}
			gotSystem, _ := body["system"].([]any)
			if !reflect.DeepEqual(gotSystem, test.wantSystem) {
				t.Fatalf("system = %#v, want %#v", body["system"], test.wantSystem)
			}
			if want := jsonValue(t, toAnthropicTools(tools)); !reflect.DeepEqual(body["tools"], want) {
				t.Fatalf("tools = %#v, want %#v", body["tools"], want)
			}
		})
	}
}

// Subscription requests are pinned to Anthropic's API origin, so the
// two-block system prompt they send is checked on the request parameters.
func TestAnthropicPromptCachingMarksOnlyTheLastSystemBlock(t *testing.T) {
	params := anthropic.MessageNewParams{System: []anthropic.TextBlockParam{
		{Text: "You are Claude Code, Anthropic's official CLI for Claude."}, {Text: "Be concise."},
	}}
	applyAnthropicPromptCaching(&params, PromptCacheLong)
	oneHour := map[string]any{"type": "ephemeral", "ttl": "1h"}
	want := []any{
		map[string]any{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."},
		map[string]any{"type": "text", "text": "Be concise.", "cache_control": oneHour},
	}
	if got := jsonValue(t, params.System); !reflect.DeepEqual(got, want) {
		t.Fatalf("system = %#v, want %#v", got, want)
	}
	if got := jsonValue(t, params.CacheControl); !reflect.DeepEqual(got, oneHour) {
		t.Fatalf("cache_control = %#v, want %#v", got, oneHour)
	}
}

func TestPromptCachingCanBeDisabled(t *testing.T) {
	request := Request{SystemPrompt: "Be concise.", Messages: []Message{UserMessage{Content: []InputContent{TextInput{Text: "hello"}}}}}
	for _, test := range []struct {
		name      string
		provider  string
		model     string
		configure func(baseURL string) ProviderConfig
	}{
		{"disabled", "anthropic", "claude-haiku-4-5", func(baseURL string) ProviderConfig {
			return Anthropic{APIKey: "key", BaseURL: baseURL, DisablePromptCaching: true}
		}},
		{"OpenCode Go Messages API", "opencode-go", "minimax-m3", func(baseURL string) ProviderConfig {
			return OpenCodeGo{APIKey: "key", BaseURL: baseURL + "/v1"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := captureMessagesRequest(t, test.configure, test.provider+"/"+test.model, request)
			want := map[string]any{
				"model": test.model, "max_tokens": body["max_tokens"], "stream": true,
				"system":   []any{map[string]any{"type": "text", "text": "Be concise."}},
				"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "hello"}}}},
			}
			for _, key := range []string{"thinking", "output_config"} {
				if value, ok := body[key]; ok {
					want[key] = value
				}
			}
			if !reflect.DeepEqual(body, want) {
				t.Fatalf("request = %#v\nwant %#v", body, want)
			}
		})
	}
}

func TestAnthropicUsageIncludesCacheTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[],"stop_reason":null,"usage":{"input_tokens":50,"cache_read_input_tokens":1000,"cache_creation_input_tokens":300,"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":200},"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}`,
			`{"type":"message_stop"}`,
		} {
			var typed struct{ Type string }
			_ = json.Unmarshal([]byte(event), &typed)
			_, _ = io.WriteString(writer, "event: "+typed.Type+"\ndata: "+event+"\n\n")
		}
	}))
	defer server.Close()
	providers, err := NewProviders(Anthropic{APIKey: "key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := providers.Resolve("anthropic/claude-haiku-4-5")
	if err != nil {
		t.Fatal(err)
	}
	stream := providers.Stream(context.Background(), model, Request{Messages: []Message{UserMessage{Content: []InputContent{TextInput{Text: "hello"}}}}})
	for range stream.Events() {
	}
	message := stream.Result()
	want := Usage{Input: 50, Output: 20, CacheRead: 1000, CacheWrite: 300, CacheWrite1h: 200, TotalTokens: 1370}
	if message.StopReason != StopReasonStop || message.Usage != want {
		t.Fatalf("result = %s %+v, want usage %+v", message.StopReason, message.Usage, want)
	}
}

func TestCalculateCostPricesOneHourCacheWritesAtTwiceInput(t *testing.T) {
	model := Model{Cost: Cost{Input: 4, Output: 20, CacheRead: 0.2, CacheWrite: 5}}
	usage := Usage{Input: 1_000, Output: 100, CacheRead: 10_000, CacheWrite: 3_000, CacheWrite1h: 1_000}
	calculateCost(model, &usage)
	want := UsageCost{Input: 0.004, Output: 0.002, CacheRead: 0.002, CacheWrite: 0.018, Total: 0.026}
	for name, pair := range map[string][2]float64{
		"input": {usage.Cost.Input, want.Input}, "output": {usage.Cost.Output, want.Output},
		"cache read": {usage.Cost.CacheRead, want.CacheRead}, "cache write": {usage.Cost.CacheWrite, want.CacheWrite},
		"total": {usage.Cost.Total, want.Total},
	} {
		if diff := pair[0] - pair[1]; diff > 1e-12 || diff < -1e-12 {
			t.Errorf("%s cost = %v, want %v", name, pair[0], pair[1])
		}
	}
}

func TestUsageValidatesAndMergesOneHourCacheWrites(t *testing.T) {
	if err := validateUsageTokens(Usage{CacheWrite: 10, CacheWrite1h: 11}); err == nil || err.Error() != "usage cache_write_1h exceeds cache_write" {
		t.Fatalf("validateUsageTokens() = %v, want the subset error", err)
	}
	merged, err := mergeUsage(Usage{CacheWrite: 10, CacheWrite1h: 4}, Usage{CacheWrite: 5, CacheWrite1h: 5})
	if err != nil || merged.CacheWrite != 15 || merged.CacheWrite1h != 9 {
		t.Fatalf("mergeUsage() = %+v, %v; want 15 cache writes, 9 one-hour", merged, err)
	}
}
