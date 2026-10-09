package droids

import (
	"fmt"
	"github.com/openai/openai-go/v3/option"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAIPolicyStop(t *testing.T) {
	for _, mode := range []string{"http", "http500", "error", "wrapped_error", "failed"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("x-request-id", "req_policy")
				if mode == "http" || mode == "http500" {
					w.Header().Set("Content-Type", "application/json")
					if mode == "http500" {
						w.WriteHeader(http.StatusInternalServerError)
					} else {
						w.WriteHeader(http.StatusForbidden)
					}
					fmt.Fprint(w, `{"error":{"code":"misalignment_policy_violation","type":"invalid_request_error","message":"private provider text"}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_policy\",\"status\":\"in_progress\"}}\n\n")
				fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_policy\",\"call_id\":\"call_policy\",\"name\":\"read\",\"arguments\":\"{}\"}}\n\n")
				switch mode {
				case "error":
					fmt.Fprint(w, "data: {\"type\":\"error\",\"code\":\"misalignment_policy_violation\",\"message\":\"private provider text\"}\n\n")
				case "wrapped_error":
					fmt.Fprint(w, "data: {\"error\":{\"code\":\"misalignment_policy_violation\",\"message\":\"private provider text\"}}\n\n")
				case "failed":
					fmt.Fprint(w, "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_policy\",\"status\":\"failed\",\"error\":{\"code\":\"misalignment_policy_violation\",\"message\":\"private provider text\"}}}\n\n")
				}
			}))
			defer server.Close()
			providers, err := NewProviders(OpenAI{APIKey: "test", BaseURL: server.URL, Options: []option.RequestOption{option.WithMaxRetries(3)}})
			if err != nil {
				t.Fatal(err)
			}
			model, _ := providers.Model("gpt-4o-mini")
			stream := providers.Stream(t.Context(), model, Request{})
			for range stream.Events() {
			}
			final := stream.Result()
			if !final.IsPolicyStop() || final.StopReason != StopReasonError || final.Error.Retryable || final.Error.RequestID != "req_policy" || final.ErrorMessage != "Provider stopped this request (misalignment_policy_violation)" {
				t.Fatalf("policy result = %+v, error = %+v", final, final.Error)
			}
			if requests.Load() != 1 {
				t.Fatalf("SDK retried %d requests", requests.Load())
			}
			if mode != "http" && mode != "http500" {
				calls := final.ToolCalls()
				if final.ResponseID != "resp_policy" || len(calls) != 1 || calls[0].ID != "call_policy" {
					t.Fatalf("lost diagnostic output: %+v", final)
				}
			}
			encoded, err := messageEnvelopeToWire(MessageEnvelope{ID: "msg_policy", ConversationID: "conv_policy", TurnID: "turn_policy", CreatedAt: time.Now(), Message: final})
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := messageEnvelopeFromWire(encoded)
			if err != nil {
				t.Fatal(err)
			}
			got := decoded.Message.(AssistantMessage)
			if !got.IsPolicyStop() || got.Error.RequestID != "req_policy" || got.ResponseID != final.ResponseID {
				t.Fatalf("diagnostic roundtrip = %+v", got)
			}
		})
	}
}

func TestOpenAIPolicyStopMatchesCodeOnly(t *testing.T) {
	if got := classifyOpenAIError(500, MisalignmentPolicyViolation, "context length exceeded"); got != ProviderInvalidRequest {
		t.Fatalf("kind = %s", got)
	}
	for _, code := range []string{"", "MISALIGNMENT_POLICY_VIOLATION", "other"} {
		final := (openAIResponsesProfile{classify: classifyOpenAIError}).errorMessage(Model{}, t.Context(), 500, code, MisalignmentPolicyViolation)
		if final.IsPolicyStop() || final.ErrorKind != ProviderInternal {
			t.Fatalf("not an exact code match: %+v", final)
		}
	}
}

func TestOpenAIPolicyStopWinsConcurrentPause(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"code":"misalignment_policy_violation","message":"blocked"}}`)
	}))
	defer server.Close()
	defer unblock()
	providers, err := NewProviders(OpenAI{APIKey: "test", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	droid, err := Spawn(t.Context(), "pause_policy", Config{Model: resolvedTestModel(providers, "openai/gpt-4o-mini")})
	if err != nil {
		t.Fatal(err)
	}
	defer droid.Close()
	defer unblock()
	handle, err := droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "hello"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := droid.Pause(t.Context(), "concurrent pause"); err != nil {
		t.Fatal(err)
	}
	unblock()
	outcome, err := handle.Wait(t.Context())
	if err != nil || outcome.Status != ExecutionFailed {
		t.Fatalf("outcome=%+v %v", outcome, err)
	}
	if err := droid.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := droid.WaitQuiescent(t.Context())
	if err != nil || snapshot.Execution.Status != ExecutionFailed || requests.Load() != 1 {
		t.Fatalf("resume=%+v %v requests=%d", snapshot, err, requests.Load())
	}
}
