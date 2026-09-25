package session

import (
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

func TestProjectProviderRetryRequiresMatchingActiveRetryingRun(t *testing.T) {
	t.Parallel()

	retryAt := time.Date(2026, time.January, 2, 3, 4, 5, 6, time.UTC)
	active := &droids.ExecutionSnapshot{
		TurnID: "turn_test", Status: droids.ExecutionRetrying,
		Retry: &droids.ProviderRetry{Count: 2, RetryAt: retryAt},
	}
	projected := projectProviderRetry("turn_test", active)
	if projected == nil || projected.Count != 2 || !projected.RetryAt.Equal(retryAt) {
		t.Fatalf("provider retry = %+v", projected)
	}
	if got := projectProviderRetry("turn_other", active); got != nil {
		t.Fatalf("mismatched run projected retry = %+v", got)
	}
	active.Status = droids.ExecutionInterrupted
	if got := projectProviderRetry("turn_test", active); got == nil || !got.RetryAt.Equal(retryAt) {
		t.Fatalf("recovering retry = %+v", got)
	}
	active.Status = droids.ExecutionRunning
	if got := projectProviderRetry("turn_test", active); got != nil {
		t.Fatalf("running execution projected retry = %+v", got)
	}
}
