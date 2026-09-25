package droids_test

import (
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

func TestSDKProtocolFailureRetry(t *testing.T) {
	for _, source := range []string{"typed", "legacy", "validation"} {
		for _, scenario := range []string{"recovery", "exhausted", "disabled"} {
			t.Run(source+"/"+scenario, func(t *testing.T) {
				invalid := usageMessage(droids.StopReasonError, droids.Usage{})
				invalid.ErrorKind = droids.ProviderProtocol
				invalid.Error = &droids.ProviderError{Kind: droids.ProviderProtocol}
				switch source {
				case "legacy":
					invalid.Error = nil
				case "validation":
					invalid = usageMessage(droids.StopReasonStop, droids.Usage{})
					invalid.StopReason = "invalid"
				}
				responses := []droids.AssistantMessage{invalid}
				wantCalls := 2
				wantStatus := droids.ExecutionCompleted
				if scenario == "exhausted" {
					responses = append(responses, invalid)
					wantStatus = droids.ExecutionFailed
				}
				if scenario == "disabled" {
					wantCalls = 1
					wantStatus = droids.ExecutionFailed
				}
				providers := &usageProviders{responses: responses}
				droid, err := droids.Spawn(t.Context(), "protocol_retry", droids.Config{
					Model: resolvedTestModel(providers, "test/usage"),
					Retry: &droids.RetryPolicy{Enabled: scenario != "disabled", MaxRetries: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond},
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = droid.Close() })
				handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "hello"}}}, droids.PromptOptions{})
				if err != nil {
					t.Fatal(err)
				}
				outcome, err := handle.Wait(t.Context())
				if err != nil || outcome.Status != wantStatus {
					t.Fatalf("outcome = %+v, %v; want %s", outcome, err, wantStatus)
				}
				if calls := providers.calls(); calls != wantCalls {
					t.Fatalf("calls = %d, want %d", calls, wantCalls)
				}
			})
		}
	}
}
