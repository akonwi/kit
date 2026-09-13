package droids

import (
	"encoding/json"
	"testing"
)

func TestDurableRuntimeCompactionIdentityMigratesAndProjects(t *testing.T) {
	legacy := EncodedRecord{Kind: runtimeRecordKind, ID: runtimeRecordID, Version: recordVersion, Payload: json.RawMessage(`{"status":"ready","turn_id":"turn_legacy"}`)}
	state, err := decodeRuntime([]EncodedRecord{legacy})
	if err != nil {
		t.Fatalf("decode legacy runtime: %v", err)
	}
	if state.Compaction != nil || executionSnapshot(state).Compaction != nil {
		t.Fatalf("legacy runtime gained compaction state: %+v", state.Compaction)
	}

	state.Status = ExecutionInterrupted
	state.SessionUsageInitialized = true
	state.Compaction = &durableCompaction{ID: "compact_00000000000000000000000000000001", TurnID: state.TurnID}
	record, err := runtimeEncodedRecord(state)
	if err != nil {
		t.Fatalf("encode runtime: %v", err)
	}
	restored, err := decodeRuntime([]EncodedRecord{record})
	if err != nil {
		t.Fatalf("decode runtime: %v", err)
	}
	if err := validateOpenedRuntime(restored); err != nil {
		t.Fatalf("validate runtime: %v", err)
	}
	projected := executionSnapshot(restored).Compaction
	if projected == nil || projected.ID != state.Compaction.ID || projected.TurnID != state.TurnID {
		t.Fatalf("projected compaction = %+v", projected)
	}
}

func TestPersistedCompactionIdentityMustMatchActiveTurn(t *testing.T) {
	state := durableRuntime{
		Status: ExecutionInterrupted,
		TurnID: "turn_current",
		Compaction: &durableCompaction{
			ID: "compact_00000000000000000000000000000001", TurnID: "turn_stale",
		},
		SessionUsageInitialized: true,
	}
	if err := validateOpenedRuntime(state); err == nil {
		t.Fatal("accepted compaction identity from a different turn")
	}
}
