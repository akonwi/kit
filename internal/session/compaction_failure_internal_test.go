package session

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/akonwi/kit/droids"
)

func TestCompactionFailureClassification(t *testing.T) {
	summary := errors.New("droids: compaction model stopped with length: ")
	for _, test := range []struct {
		name       string
		err        error
		compaction bool
	}{
		{"summary failure", summary, true},
		{"not adaptable", errors.Join(droids.ErrContextNotAdaptable, summary), true},
		{"canceled", fmt.Errorf("summary: %w", context.Canceled), false},
		{"deadline", context.DeadlineExceeded, false},
		{"busy", droids.ErrBusy, false},
		{"closed", droids.ErrClosed, false},
		{"conflict", droids.ErrConflict, false},
	} {
		got := compactionFailure(test.err)
		if !errors.Is(got, test.err) {
			t.Fatalf("%s: cause lost: %v", test.name, got)
		}
		if errors.Is(got, ErrCompactionFailed) != test.compaction {
			t.Fatalf("%s: compaction failure = %v, want %v", test.name, !test.compaction, test.compaction)
		}
	}
}
