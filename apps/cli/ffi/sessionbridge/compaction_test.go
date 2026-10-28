package sessionbridge

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

func TestCompactionFailureTextExplainsEachReason(t *testing.T) {
	t.Parallel()

	want := map[protocol.CompactionFailureReason]string{
		protocol.CompactionFailureSummaryTruncated: "The summary was longer than the compaction model's output limit.",
		protocol.CompactionFailureSummaryInvalid:   "The compaction model didn't return a usable summary.",
		protocol.CompactionFailurePolicyStop:       "The provider stopped the summary under its usage policy.",
		protocol.CompactionFailureProviderError:    "The request to the model provider failed.",
		protocol.CompactionFailureNotAdaptable:     "The conversation can't be reduced to fit the model's context.",
		protocol.CompactionFailureCanceled:         "Compaction was canceled before it finished.",
		protocol.CompactionFailureConflict:         "The session changed while compacting.",
		protocol.CompactionFailureInternal:         "Kit couldn't finish compacting because of an internal error.",
	}
	for _, reason := range protocol.CompactionFailureReason("").Values() {
		if got := CompactionFailureText(string(reason)); got != want[reason] {
			t.Errorf("CompactionFailureText(%q) = %q, want %q", reason, got, want[reason])
		}
	}
	if got, want := CompactionFailedTurnText("conflict"), "Context compaction failed. The session changed while compacting."; got != want {
		t.Fatalf("CompactionFailedTurnText() = %q, want %q", got, want)
	}
	if got, want := CompactionFailedTurnText("disk_full"), "Context compaction failed."; got != want {
		t.Fatalf("an unknown reason reads %q, want %q", got, want)
	}
}

func TestOperationErrorTextExplainsAFailedCompaction(t *testing.T) {
	t.Parallel()

	failed := fmt.Errorf("compact session: %w", &kit.ServerError{
		Code: kit.ErrorCompactionFailed, Message: "context compaction failed",
		Details: protocol.CompactionFailedErrorDetails{Reason: protocol.CompactionFailurePolicyStop},
	})
	if got, want := OperationErrorText(failed), "The provider stopped the summary under its usage policy."; got != want {
		t.Fatalf("OperationErrorText(compaction) = %q, want %q", got, want)
	}
	other := &kit.ServerError{Code: kit.ErrorConflict, Message: "operation conflicts with session state"}
	if got, want := OperationErrorText(other), "Kit server request failed (conflict): operation conflicts with session state"; got != want {
		t.Fatalf("OperationErrorText(other) = %q, want %q", got, want)
	}
	if got := CompactionFailureReason(errors.New(http.StatusText(http.StatusUnprocessableEntity))); got != "" {
		t.Fatalf("CompactionFailureReason(plain) = %q, want none", got)
	}
}
