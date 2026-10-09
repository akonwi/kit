package droids

import (
	"testing"
	"time"
)

func TestExecutionSnapshotProjectsRetryWhileWaitingOrRecovering(t *testing.T) {
	t.Parallel()

	retryAt := time.Date(2026, time.January, 2, 3, 4, 5, 6, time.UTC)
	state := durableRuntime{
		TurnID: "turn_test", Status: ExecutionRetrying,
		RetryCount: 2, RetryAt: retryAt,
	}
	snapshot := executionSnapshot(state)
	if snapshot.Retry == nil || snapshot.Retry.Count != 2 || !snapshot.Retry.RetryAt.Equal(retryAt) {
		t.Fatalf("retry snapshot = %+v", snapshot.Retry)
	}

	state.Status = ExecutionInterrupted
	snapshot = executionSnapshot(state)
	if snapshot.Retry == nil || snapshot.Retry.Count != 2 || !snapshot.Retry.RetryAt.Equal(retryAt) {
		t.Fatalf("interrupted retry snapshot = %+v", snapshot.Retry)
	}

	state.Status = ExecutionRunning
	snapshot = executionSnapshot(state)
	if snapshot.Retry != nil {
		t.Fatalf("running snapshot retained retry = %+v", snapshot.Retry)
	}
}
