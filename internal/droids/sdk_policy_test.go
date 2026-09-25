package droids_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

func TestSDKPolicyStopDoesNotRetryOrDispatchAndAllowsManualRetry(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		name := "valid_partial"
		if malformed {
			name = "malformed_partial"
		}
		t.Run(name, func(t *testing.T) {
			tool := usageMessage(droids.StopReasonToolUse, droids.Usage{})
			tool.Content = []droids.AssistantContent{droids.ToolCall{ID: "prior_call", Name: "read_fixture", Arguments: []byte(`{"path":"fixture"}`)}}
			stopped := usageMessage(droids.StopReasonError, droids.Usage{})
			stopped.ResponseID = "resp_policy"
			stopped.Error = &droids.ProviderError{Kind: droids.ProviderTransport, Code: droids.MisalignmentPolicyViolation, RequestID: "req_policy", Retryable: true}
			stopped.Content = []droids.AssistantContent{droids.ToolCall{ID: "never_call", Name: "read_fixture", Arguments: []byte(`{"path":"fixture"}`)}}
			if malformed {
				stopped.Usage.Input = -1
			}
			providers := &usageProviders{responses: []droids.AssistantMessage{tool, stopped}}
			var runs atomic.Int32
			droid, err := droids.Spawn(t.Context(), "policy_stop", droids.Config{
				Model: resolvedTestModel(providers, "test/usage"), Tools: []droids.AnyTool{readOnlyTool(&runs)},
				Retry: &droids.RetryPolicy{Enabled: true, MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer droid.Close()
			prompt := func() droids.Outcome {
				handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "continue"}}}, droids.PromptOptions{})
				if err != nil {
					t.Fatal(err)
				}
				result, err := handle.Wait(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			outcome := prompt()
			if outcome.Status != droids.ExecutionFailed || providers.calls() != 2 || runs.Load() != 1 {
				t.Fatalf("outcome=%+v requests=%d actions=%d", outcome, providers.calls(), runs.Load())
			}
			if outcome.FinalMessage == nil {
				t.Fatal("missing final diagnostic")
			}
			final := outcome.FinalMessage.Message.(droids.AssistantMessage)
			if !final.IsPolicyStop() || final.Error.Retryable || final.ResponseID != "resp_policy" || final.Error.RequestID != "req_policy" {
				t.Fatalf("final=%+v error=%+v", final, final.Error)
			}
			history, err := droid.History(t.Context(), droids.HistoryQuery{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			toolResults := 0
			for _, envelope := range history.Messages {
				if result, ok := envelope.Message.(droids.ToolResultMessage); ok && result.ToolCallID != "" {
					toolResults++
				}
			}
			if toolResults != 1 {
				t.Fatalf("completed action records=%d, want 1", toolResults)
			}
			outcome = prompt()
			if outcome.Status != droids.ExecutionCompleted || providers.calls() != 3 || runs.Load() != 1 {
				t.Fatalf("manual retry=%+v calls=%d actions=%d", outcome, providers.calls(), runs.Load())
			}
		})
	}
}
