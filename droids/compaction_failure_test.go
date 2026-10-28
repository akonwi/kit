package droids

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestCompactionFailureClassificationPrefersTheCompactionContext(t *testing.T) {
	live := context.Background()
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	// A provider's own timeout reports a deadline even though the compaction
	// itself was not canceled.
	providerTimeout := compactionFailed(CompactionFailureProviderError, &net.OpError{Op: "read", Err: context.DeadlineExceeded})
	for _, test := range []struct {
		name string
		ctx  context.Context
		err  error
		want CompactionFailureReason
	}{
		{"marked at its origin", live, compactionFailed(CompactionFailureSummaryTruncated, errors.New("stopped")), CompactionFailureSummaryTruncated},
		{"marked and wrapped", live, fmt.Errorf("summary: %w", compactionFailed(CompactionFailurePolicyStop, errors.New("policy"))), CompactionFailurePolicyStop},
		{"provider timeout", live, providerTimeout, CompactionFailureProviderError},
		{"compaction canceled", ended, providerTimeout, CompactionFailureCanceled},
		{"unmarked cancellation", live, fmt.Errorf("wait: %w", context.Canceled), CompactionFailureCanceled},
		{"unmarked deadline", live, context.DeadlineExceeded, CompactionFailureCanceled},
		{"closed", live, ErrClosed, CompactionFailureCanceled},
		{"busy", live, ErrBusy, CompactionFailureConflict},
		{"conflict", live, fmt.Errorf("configuration changed: %w", ErrConflict), CompactionFailureConflict},
		{"not adaptable", live, fmt.Errorf("too large: %w", ErrContextNotAdaptable), CompactionFailureNotAdaptable},
		{"anything else", live, errors.New("disk full"), CompactionFailureInternal},
		{"no context", nil, ErrBusy, CompactionFailureConflict},
	} {
		classified := classifyCompactionFailure(test.ctx, test.err)
		if classified.Reason != test.want || !errors.Is(classified, test.err) {
			t.Errorf("%s: classified %v as %q, want %q with its cause", test.name, test.err, classified.Reason, test.want)
		}
		if reason, ok := CompactionFailureOf(fmt.Errorf("wrapped: %w", classified)); !ok || reason != test.want {
			t.Errorf("%s: CompactionFailureOf() = %q, %v", test.name, reason, ok)
		}
	}
	if _, ok := CompactionFailureOf(errors.New("plain")); ok {
		t.Fatal("an unclassified error reported a compaction failure")
	}
	if _, ok := CompactionFailureOf(&CompactionError{Reason: "disk_full"}); ok {
		t.Fatal("an undeclared reason was reported")
	}
}

func TestExplicitCompactionFailuresAreClassifiedAndRecorded(t *testing.T) {
	providerFailure := errors.New("HTTP 500: secret request text")
	for _, test := range []struct {
		name      string
		summary   func(AssistantMessage) AssistantMessage
		streamErr error
		want      CompactionFailureReason
	}{
		{name: "length", want: CompactionFailureSummaryTruncated, summary: func(message AssistantMessage) AssistantMessage {
			message.StopReason = StopReasonLength
			return message
		}},
		{name: "empty", want: CompactionFailureSummaryInvalid, summary: func(message AssistantMessage) AssistantMessage {
			message.Content = []AssistantContent{TextContent{Text: " \n "}}
			return message
		}},
		{name: "tool call", want: CompactionFailureSummaryInvalid, summary: func(message AssistantMessage) AssistantMessage {
			message.StopReason = StopReasonToolUse
			message.Content = []AssistantContent{ToolCall{ID: "unexpected_call", Name: "bash", Arguments: []byte(`{}`)}}
			return message
		}},
		{name: "policy stop", want: CompactionFailurePolicyStop, summary: func(message AssistantMessage) AssistantMessage {
			message.StopReason = StopReasonError
			message.Error = &ProviderError{Kind: ProviderProtocol, Code: MisalignmentPolicyViolation, Message: "policy"}
			message.ErrorMessage = "policy"
			return message
		}},
		{name: "provider error stop", want: CompactionFailureProviderError, summary: func(message AssistantMessage) AssistantMessage {
			message.StopReason = StopReasonError
			message.ErrorKind = ProviderTransport
			message.ErrorMessage = "summary failed"
			return message
		}},
		{name: "summary overflow", want: CompactionFailureNotAdaptable, summary: func(message AssistantMessage) AssistantMessage {
			message.StopReason = StopReasonContextWindow
			return message
		}},
		{name: "aborted by provider", want: CompactionFailureCanceled, summary: func(message AssistantMessage) AssistantMessage {
			message.StopReason = StopReasonAborted
			return message
		}},
		{name: "request failed", want: CompactionFailureProviderError, streamErr: providerFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newCompactionTestProvider(90_000)
			store := NewMemoryStore()
			droid := spawnCompactionTestDroid(t, provider, store)
			seedCompactionContext(t, droid, longToolCompactionContext())
			provider.summaryResponse = func(int) AssistantMessage {
				if test.summary == nil {
					return provider.summaryMessage()
				}
				return test.summary(provider.summaryMessage())
			}
			provider.summaryErr = test.streamErr
			_, err := droid.CompactContext(t.Context(), CompactContextOptions{
				OperationID: "classified", Target: ContextTarget{Model: droid.model}, Force: true,
			})
			var failure *CompactionError
			if !errors.As(err, &failure) || failure.Reason != test.want {
				t.Fatalf("CompactContext() error = %#v, want reason %q", err, test.want)
			}
			if test.streamErr != nil && !errors.Is(err, test.streamErr) {
				t.Fatalf("the diagnostic cause was lost: %v", err)
			}
			recorded := recordedCompactionFailures(t, store)
			if len(recorded) != 1 || recorded[0].Reason != test.want || recorded[0].Error != "Context compaction failed" {
				t.Fatalf("recorded failures = %+v, want one %q without provider text", recorded, test.want)
			}
		})
	}
}

func TestOversizedSourceCompactionIsNotAdaptable(t *testing.T) {
	provider := newCompactionTestProvider(1_000)
	droid := spawnCompactionTestDroid(t, provider, nil)
	messages := []Message{UserMessage{Content: []InputContent{TextInput{Text: strings.Repeat("x", 4_000)}}}}
	_, err := droid.sdk.summarizeCompaction(t.Context(), provider, droid.model, messages, "")
	if got := classifyCompactionFailure(t.Context(), err).Reason; got != CompactionFailureNotAdaptable {
		t.Fatalf("oversized source = %q (%v), want not_adaptable", got, err)
	}
}

func TestATurnThatCompactionFailedKeepsItsReasonAcrossReopen(t *testing.T) {
	provider := newCompactionTestProvider(90_000)
	provider.summaryResponse = func(int) AssistantMessage {
		message := provider.summaryMessage()
		message.StopReason = StopReasonLength
		return message
	}
	store := NewMemoryStore()
	droid := spawnCompactionTestDroid(t, provider, store)
	seedCompactionContext(t, droid, longToolCompactionContext())
	handle, err := droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "continue"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(t.Context())
	if err != nil || outcome.Status != ExecutionFailed || outcome.Error == nil {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	want := DroidError{Kind: DroidErrorCompaction, Message: "Context compaction failed", CompactionFailure: CompactionFailureSummaryTruncated}
	if got := *outcome.Error; got.Kind != want.Kind || got.Message != want.Message || got.CompactionFailure != want.CompactionFailure {
		t.Fatalf("turn error = %+v, want %+v", got, want)
	}
	if recorded := recordedCompactionFailures(t, store); len(recorded) != 1 || recorded[0].Reason != CompactionFailureSummaryTruncated {
		t.Fatalf("recorded failures = %+v", recorded)
	}
	if err := droid.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := spawnCompactionTestDroid(t, provider, store)
	turn, err := reopened.Turn(t.Context(), handle.TurnID())
	if err != nil || turn.Error == nil || turn.Error.CompactionFailure != CompactionFailureSummaryTruncated {
		t.Fatalf("reopened turn = %+v, %v", turn, err)
	}
}

func TestDurableErrorsKeepOnlyDeclaredCompactionReasons(t *testing.T) {
	for _, test := range []struct {
		name   string
		stored durableDroidError
		want   CompactionFailureReason
	}{
		{"declared", durableDroidError{Kind: DroidErrorCompaction, CompactionFailure: CompactionFailureConflict}, CompactionFailureConflict},
		{"recorded before reasons", durableDroidError{Kind: DroidErrorCompaction}, ""},
		{"from a later release", durableDroidError{Kind: DroidErrorCompaction, CompactionFailure: "disk_full"}, ""},
		{"not a compaction", durableDroidError{Kind: DroidErrorProvider, CompactionFailure: CompactionFailureConflict}, ""},
	} {
		if got := expandDurableError(&test.stored).CompactionFailure; got != test.want {
			t.Errorf("%s: reason = %q, want %q", test.name, got, test.want)
		}
	}
}

type recordedCompactionFailure struct {
	Reason CompactionFailureReason `json:"reason"`
	Error  string                  `json:"error"`
}

func recordedCompactionFailures(t *testing.T, store Store) []recordedCompactionFailure {
	t.Helper()
	page, err := store.Events(t.Context(), EventQuery{Limit: 1_000})
	if err != nil {
		t.Fatal(err)
	}
	var failures []recordedCompactionFailure
	for _, event := range page.Events {
		if event.Kind != "compaction.failed" {
			continue
		}
		var lifecycle durableLifecycleEvent
		var failure recordedCompactionFailure
		if err := json.Unmarshal(event.Payload, &lifecycle); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(lifecycle.Data, &failure); err != nil {
			t.Fatal(err)
		}
		failures = append(failures, failure)
	}
	return failures
}
