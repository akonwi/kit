package droids

import (
	"context"
	"errors"
	"testing"
)

func TestCompactionCancellationDoesNotRecordFailureLifecycle(t *testing.T) {
	runtime := &sdkRuntime{}
	if err := runtime.recordCompactionFailure("turn_cancelled", context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if err := runtime.recordCompactionFailure("turn_timed_out", context.DeadlineExceeded); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
}

func TestLegacyCompactionIdentityPreservesOriginalNonForcedSemantics(t *testing.T) {
	target := ContextTarget{Model: "test/model", Reasoning: "off"}
	legacyIntent := durableCompactionIntent{OperationID: "legacy_operation", TargetModel: target.Model, Reasoning: target.Reasoning}
	force, err := legacyIntent.effectiveForce(target, true)
	if err != nil || force {
		t.Fatalf("legacy intent force = %v, %v; want original false semantics", force, err)
	}
	usage := durableContextUsage{
		Model: Model{Provider: "test", ID: "model"}, ContextWindow: 100,
		MaxInputTokens: 90, Remaining: 90,
	}
	legacyReceipt := durableCompactionReceipt{
		OperationID: "legacy_operation", TargetModel: target.Model, Reasoning: target.Reasoning,
		Before: usage, After: usage,
	}
	result, err := legacyReceipt.result(target, true)
	if err != nil || result.Forced {
		t.Fatalf("legacy receipt result = %+v, %v; want replayed non-forced result", result, err)
	}

	forced := true
	newIntent := legacyIntent
	newIntent.Force = &forced
	if _, err := newIntent.effectiveForce(target, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("new intent force mismatch error = %v, want conflict", err)
	}
}
