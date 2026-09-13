package session

import (
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

func TestProjectActiveCompactionRequiresMatchingActiveRun(t *testing.T) {
	active := &droids.ExecutionSnapshot{
		TurnID: "turn_test", Status: droids.ExecutionInterrupted,
		Compaction: &droids.CompactionSnapshot{ID: "compact_00000000000000000000000000000001", TurnID: "turn_test"},
	}
	projected := projectActiveCompaction("turn_test", active)
	if projected == nil || projected.ID != active.Compaction.ID || projected.RunID != "turn_test" {
		t.Fatalf("projected active compaction = %+v", projected)
	}
	if got := projectActiveCompaction("turn_other", active); got != nil {
		t.Fatalf("projected mismatched compaction = %+v", got)
	}
	active.Compaction.TurnID = "turn_other"
	if got := projectActiveCompaction("turn_test", active); got != nil {
		t.Fatalf("projected stale compaction = %+v", got)
	}
}
