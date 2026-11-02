package droids

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
)

func TestSanitizeProviderDetail(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, input, want string
	}{
		{"actionable reason", "Upstream request failed: Model is unavailable.", "Upstream request failed: Model is unavailable."},
		{"control characters", "line one\r\nline\ttwo\x00\x07 \x1b[31mred\x1b[0m end", "line one line two [31mred [0m end"},
		{"bidi and format characters", "safe\u202etxt.exe\u2066 hidden\u200b text\u2028next", "safe txt.exe hidden text next"},
		{"invalid utf8", "bad \xff\xfe bytes \xc3", "bad bytes"},
		{"whitespace", "   spaced    out\n\n\ttext   ", "spaced out text"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := sanitizeProviderDetail(test.input); got != test.want {
				t.Fatalf("sanitizeProviderDetail(%q)\n got %q\nwant %q", test.input, got, test.want)
			}
			if got := sanitizeProviderDetail(test.want); got != test.want {
				t.Fatalf("sanitizing again changed %q to %q", test.want, got)
			}
		})
	}
}

func TestSanitizeProviderDetailBoundsLongText(t *testing.T) {
	t.Parallel()
	input := "Prompt was rejected: " + strings.Repeat("é word ", 200)
	got := sanitizeProviderDetail(input)
	// 21 bytes of prefix and 34 repetitions of the 8-byte "é word " leave
	// "é w" before the 3-byte ellipsis within the 300-byte bound.
	want := "Prompt was rejected: " + strings.Repeat("é word ", 34) + "é w…"
	if got != want || len(got) != maxProviderDetailBytes || !utf8.ValidString(got) {
		t.Fatalf("got %q (%d bytes)\nwant %q", got, len(got), want)
	}
	if again := sanitizeProviderDetail(got); again != got {
		t.Fatalf("sanitizing again changed %q to %q", got, again)
	}
}

func TestSanitizeProviderCode(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"model_not_found":               "model_not_found",
		"invalid_request_error":         "invalid_request_error",
		"misalignment_policy_violation": "misalignment_policy_violation",
		" 400 ":                         "400",
		"has space":                     "",
		"<script>":                      "",
		strings.Repeat("a", 65):         "",
	} {
		if got := sanitizeProviderCode(input); got != want {
			t.Fatalf("sanitizeProviderCode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestProviderErrorFromBody(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, body, code, detail string
	}{
		{"plain text", "Upstream request failed: Model is unavailable.\n", "", "Upstream request failed: Model is unavailable."},
		{"openai envelope", `{"error":{"message":"The model does not exist","type":"invalid_request_error","code":"model_not_found","param":null}}`, "model_not_found", "The model does not exist"},
		{"anthropic envelope", `{"type":"error","error":{"type":"invalid_request_error","message":"image exceeds 5 MB maximum"},"request_id":"req_1"}`, "invalid_request_error", "image exceeds 5 MB maximum"},
		{"string error", `{"error":"Upstream request failed: Model is unavailable."}`, "", "Upstream request failed: Model is unavailable."},
		{"numeric code", `{"error":{"code":400,"message":"Image too large","status":"INVALID_ARGUMENT"}}`, "400", "Image too large"},
		{"top level message", `{"message":"Model is unavailable","type":"upstream_error"}`, "upstream_error", "Model is unavailable"},
		{"validation listing", `{"detail":[{"loc":["body","messages"],"input":"my private prompt"}]}`, "", ""},
		{"html page", "<html><body>502 Bad Gateway</body></html>", "", ""},
		{"truncated json", `{"error":{"message":"Model is unavailable","input":"my private prompt`, "", ""},
		{"empty", "  ", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			code, detail := providerErrorFromBody([]byte(test.body))
			if code != test.code || detail != test.detail {
				t.Fatalf("providerErrorFromBody() = %q, %q; want %q, %q", code, detail, test.code, test.detail)
			}
		})
	}
}

func TestProviderErrorExplanation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		err  ProviderError
		want string
	}{
		{ProviderError{Message: "Provider rejected the request", Detail: "Upstream request failed: Model is unavailable.", Status: 400},
			"Provider rejected the request: Upstream request failed: Model is unavailable. (HTTP 400)"},
		{ProviderError{Message: "Provider rejected the request", Status: 400}, "Provider rejected the request (HTTP 400)"},
		{ProviderError{Message: "Provider request failed", Detail: "stream ended"}, "Provider request failed: stream ended"},
		{ProviderError{Message: "Provider transport failed"}, "Provider transport failed"},
	} {
		if got := test.err.Explanation(); got != test.want {
			t.Fatalf("Explanation() = %q, want %q", got, test.want)
		}
	}
}

// providerRejection is what an adapter reports about a rejected request.
type providerRejection struct {
	kind         ProviderErrorKind
	status       int
	code, detail string
}

func streamRejection(t *testing.T, providers Providers, model Model) providerRejection {
	t.Helper()
	stream := providers.Stream(context.Background(), model, Request{Messages: []Message{
		UserMessage{Content: []InputContent{TextInput{Text: "hi"}}},
	}})
	for range stream.Events() {
	}
	final := stream.Result()
	if final.Error != nil && final.Error.Detail != sanitizeProviderDetail(final.Error.Detail) {
		t.Fatalf("the provider stream published unsanitized detail %q", final.Error.Detail)
	}
	normalizeProviderError(&final)
	if final.Error == nil {
		t.Fatalf("%s %s: no provider error in %+v", model.Provider, model.ID, final)
	}
	if final.ErrorMessage != final.Error.Message || final.ErrorKind != final.Error.Kind {
		t.Fatalf("message error %q/%q differs from typed error %+v", final.ErrorKind, final.ErrorMessage, final.Error)
	}
	return providerRejection{kind: final.Error.Kind, status: final.Error.Status, code: final.Error.Code, detail: final.Error.Detail}
}

func rejectingServer(t *testing.T, status int, contentType, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.Header().Set("Content-Type", contentType)
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestOpenCodeGoKeepsTheProviderReasonForEveryAPI(t *testing.T) {
	t.Parallel()
	const reason = "Upstream request failed: Model is unavailable."
	for _, test := range []struct {
		name, contentType, body string
		code                    string
	}{
		{"plain text", "text/plain", reason, ""},
		{"string error", "application/json", `{"error":"` + reason + `"}`, ""},
		{"openai envelope", "application/json", `{"error":{"message":"` + reason + `","type":"upstream_error","code":"model_unavailable"}}`, "model_unavailable"},
		{"anthropic envelope", "application/json", `{"type":"error","error":{"type":"invalid_request_error","message":"` + reason + `"}}`, "invalid_request_error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := rejectingServer(t, http.StatusBadRequest, test.contentType, test.body)
			providers, err := NewProviders(OpenCodeGo{APIKey: "test-key", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			for _, model := range modelPerAPI(t, providers) {
				id := model.ID
				got := streamRejection(t, providers, model)
				want := providerRejection{kind: ProviderInvalidRequest, status: http.StatusBadRequest, code: test.code, detail: reason}
				if test.name == "openai envelope" && model.API == ModelAPIAnthropicMessages {
					// The Anthropic SDK reports the envelope's type as the code.
					want.code = "upstream_error"
				}
				if got != want {
					t.Fatalf("%s (%s) = %+v, want %+v", id, model.API, got, want)
				}
			}
		})
	}
}

// modelPerAPI returns one catalog model for each wire API a gateway serves.
func modelPerAPI(t *testing.T, providers Providers) []Model {
	t.Helper()
	var models []Model
	for _, api := range []ModelAPI{ModelAPIOpenAIResponses, ModelAPIOpenAIChat, ModelAPIAnthropicMessages} {
		for _, model := range providers.Models() {
			if model.API == api {
				models = append(models, model)
				break
			}
		}
	}
	if len(models) != 3 {
		t.Fatalf("catalog serves %d of 3 wire APIs", len(models))
	}
	return models
}

func TestProviderAdaptersKeepTheReasonForRepresentativeRejections(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		status int
		body   string
		build  func(*httptest.Server) (Providers, string)
		want   providerRejection
	}{
		{
			name: "OpenAI unknown model", status: http.StatusNotFound,
			body: `{"error":{"message":"The model 'gpt-9' does not exist or you do not have access to it.","type":"invalid_request_error","param":null,"code":"model_not_found"}}`,
			build: func(server *httptest.Server) (Providers, string) {
				providers, _ := NewProviders(OpenAI{APIKey: "test-key", BaseURL: server.URL})
				return providers, "gpt-5.4"
			},
			want: providerRejection{kind: ProviderInvalidRequest, status: 404, code: "model_not_found",
				detail: "The model 'gpt-9' does not exist or you do not have access to it."},
		},
		{
			name: "Anthropic image rejection", status: http.StatusBadRequest,
			body: `{"type":"error","error":{"type":"invalid_request_error","message":"messages.0.content.1.image.source.base64: image exceeds 5 MB maximum: 5316764 bytes > 5242880 bytes"},"request_id":"req_011CTest"}`,
			build: func(server *httptest.Server) (Providers, string) {
				providers, _ := NewProviders(Anthropic{APIKey: "test-key", BaseURL: server.URL, Options: anthropicTestOptions()})
				return providers, "claude-haiku-4-5"
			},
			want: providerRejection{kind: ProviderInvalidRequest, status: 400, code: "invalid_request_error",
				detail: "messages.0.content.1.image.source.base64: image exceeds 5 MB maximum: 5316764 bytes > 5242880 bytes"},
		},
		{
			name: "Anthropic entitlement", status: http.StatusForbidden,
			body: `{"type":"error","error":{"type":"permission_error","message":"Your organization does not have access to this model."}}`,
			build: func(server *httptest.Server) (Providers, string) {
				providers, _ := NewProviders(Anthropic{APIKey: "test-key", BaseURL: server.URL, Options: anthropicTestOptions()})
				return providers, "claude-haiku-4-5"
			},
			want: providerRejection{kind: ProviderEntitlement, status: 403, code: "permission_error",
				detail: "Your organization does not have access to this model."},
		},
		{
			name: "OpenAI Codex unsupported model", status: http.StatusBadRequest,
			body: `{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}`,
			build: func(server *httptest.Server) (Providers, string) {
				providers, _ := NewProviders(OpenAICodex{Credentials: staticCodexCredentials(), HTTPClient: server.Client(),
					responsesBaseURL: server.URL + "/backend-api/codex"})
				return providers, "openai-codex/gpt-5.6-sol"
			},
			want: providerRejection{kind: ProviderInvalidRequest, status: 400,
				detail: "The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."},
		},
		{
			name: "OpenCode Go image rejection", status: http.StatusBadRequest,
			body: `{"error":{"message":"Upstream request failed: Image dimensions exceed max allowed size of 8000 pixels.","type":"invalid_request_error"}}`,
			build: func(server *httptest.Server) (Providers, string) {
				providers, _ := NewProviders(OpenCodeGo{APIKey: "test-key", BaseURL: server.URL})
				return providers, "glm-5.2"
			},
			want: providerRejection{kind: ProviderInvalidRequest, status: 400, code: "invalid_request_error",
				detail: "Upstream request failed: Image dimensions exceed max allowed size of 8000 pixels."},
		},
		{
			name: "gateway HTML error page", status: http.StatusBadGateway,
			body: "<html><head><title>502 Bad Gateway</title></head><body>nginx</body></html>",
			build: func(server *httptest.Server) (Providers, string) {
				providers, _ := NewProviders(OpenCodeGo{APIKey: "test-key", BaseURL: server.URL})
				return providers, "glm-5.2"
			},
			want: providerRejection{kind: ProviderInternal, status: 502},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := rejectingServer(t, test.status, "application/json", test.body)
			providers, id := test.build(server)
			if providers == nil {
				t.Fatal("providers were not built")
			}
			model, ok := providers.Model(id)
			if !ok {
				t.Fatalf("model %q is not in the catalog", id)
			}
			if got := streamRejection(t, providers, model); got != test.want {
				t.Fatalf("rejection = %+v\nwant %+v", got, test.want)
			}
		})
	}
}

// Codex classifies streamed failures, which carry no HTTP status, by code;
// these codes are unclassified, so they are protocol failures.
func TestOpenAIStreamedFailuresKeepTheProviderReason(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, event string
		want        providerRejection
	}{
		{
			name:  "error event",
			event: `{"type":"error","sequence_number":1,"code":"invalid_prompt","message":"Invalid prompt: your prompt was flagged as potentially violating our usage policy.","param":null}`,
			want: providerRejection{kind: ProviderProtocol, code: "invalid_prompt",
				detail: "Invalid prompt: your prompt was flagged as potentially violating our usage policy."},
		},
		{
			name:  "failed response",
			event: `{"type":"response.failed","sequence_number":1,"response":{"id":"resp_1","model":"gpt-5.6-sol","status":"failed","output":[],"error":{"code":"server_error","message":"The server had an error while processing your request."}}}`,
			want: providerRejection{kind: ProviderProtocol, code: "server_error",
				detail: "The server had an error while processing your request."},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := newCodexResponsesServer(t, make(chan map[string]any, 1), test.event)
			t.Cleanup(server.Close)
			providers, model := testOpenAICodexProvider(t, server)
			if got := streamRejection(t, providers, model); got != test.want {
				t.Fatalf("rejection = %+v\nwant %+v", got, test.want)
			}
		})
	}
}

func anthropicTestOptions() []anthropicoption.RequestOption {
	return []anthropicoption.RequestOption{anthropicoption.WithoutEnvironmentDefaults(), anthropicoption.WithMaxRetries(0)}
}
