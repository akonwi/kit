package session

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/akonwi/kit/droids"
)

func TestCompactionFailureClassification(t *testing.T) {
	summary := &droids.CompactionError{Reason: droids.CompactionFailureSummaryTruncated, Cause: errors.New("droids: compaction model stopped with length: ")}
	for _, test := range []struct {
		name   string
		err    error
		reason CompactionFailureReason
	}{
		{"summary failure", summary, CompactionFailureSummaryTruncated},
		{"not adaptable", &droids.CompactionError{Reason: droids.CompactionFailureNotAdaptable, Cause: droids.ErrContextNotAdaptable}, CompactionFailureNotAdaptable},
		{"unclassified", errors.New("droid store is corrupt"), CompactionFailureInternal},
		{"provider timeout", &droids.CompactionError{Reason: droids.CompactionFailureProviderError, Cause: fmt.Errorf("read: %w", context.DeadlineExceeded)}, CompactionFailureProviderError},
		{"provider abort", &droids.CompactionError{Reason: droids.CompactionFailureCanceled, Cause: errors.New("compaction model stopped with aborted")}, CompactionFailureCanceled},
		{"classified cancellation", &droids.CompactionError{Reason: droids.CompactionFailureCanceled, Cause: context.Canceled}, ""},
		{"classified busy", &droids.CompactionError{Reason: droids.CompactionFailureConflict, Cause: droids.ErrBusy}, ""},
		{"undeclared", &droids.CompactionError{Reason: "disk_full"}, CompactionFailureInternal},
		{"canceled", fmt.Errorf("summary: %w", context.Canceled), ""},
		{"deadline", context.DeadlineExceeded, ""},
		{"busy", droids.ErrBusy, ""},
		{"closed", droids.ErrClosed, ""},
		{"conflict", droids.ErrConflict, ""},
	} {
		got := compactionFailure(test.err)
		if !errors.Is(got, test.err) {
			t.Fatalf("%s: cause lost: %v", test.name, got)
		}
		if errors.Is(got, ErrCompactionFailed) != (test.reason != "") {
			t.Fatalf("%s: compaction failure = %v, want reason %q", test.name, got, test.reason)
		}
		var failed *CompactionFailedError
		if test.reason != "" && (!errors.As(got, &failed) || failed.Reason != test.reason) {
			t.Fatalf("%s: reason = %+v, want %q", test.name, failed, test.reason)
		}
	}
}
