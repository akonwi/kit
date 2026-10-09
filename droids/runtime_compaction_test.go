package droids

import (
	"encoding/json"
	"strings"
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

func TestCompactionKeepsTwentyThousandTokenSuffix(t *testing.T) {
	messages := make([]Message, 6)
	for i := range messages {
		// A user message has 36 bytes of estimator framing.
		messages[i] = UserMessage{Content: []InputContent{TextInput{Text: strings.Repeat("x", 20_000-36)}}}
	}
	ends, err := compactionPrefixEnds(messages, len(messages))
	if err != nil {
		t.Fatal(err)
	}
	if len(ends) != 3 || ends[0] != 4 || ends[1] != 5 || ends[2] != 6 {
		t.Fatalf("prefix ends = %v, want [4 5 6]", ends)
	}
	if got := EstimateMessagesTokens(messages[ends[0]:]); got != 20_000 {
		t.Fatalf("retained tokens = %d, want 20000", got)
	}
}

func TestCompactionKeepsCompleteToolBatchAcrossSuffixBoundary(t *testing.T) {
	messages := []Message{
		UserMessage{Content: []InputContent{TextInput{Text: "old request"}}},
		AssistantMessage{StopReason: StopReasonToolUse, Content: []AssistantContent{
			ToolCall{ID: "call_a", Name: "read", Arguments: []byte(`{}`)},
			ToolCall{ID: "call_b", Name: "read", Arguments: []byte(`{}`)},
		}},
		ToolResultMessage{ToolCallID: "call_a", ToolName: "read", Content: []ResultContent{TextContent{Text: strings.Repeat("a", 24_000)}}},
		ToolResultMessage{ToolCallID: "call_b", ToolName: "read", Content: []ResultContent{TextContent{Text: strings.Repeat("b", 24_000)}}},
		AssistantMessage{Content: []AssistantContent{TextContent{Text: "latest reply"}}},
	}
	ends, err := compactionPrefixEnds(messages, len(messages))
	if err != nil {
		t.Fatal(err)
	}
	if len(ends) != 3 || ends[0] != 1 || ends[1] != 4 || ends[2] != 5 {
		t.Fatalf("prefix ends = %v, want [1 4 5]", ends)
	}
	for _, end := range ends {
		if err := validateMessageSequence(messages[:end]); err != nil {
			t.Fatal(err)
		}
		if err := validateMessageSequence(messages[end:]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompactionRejectsIncompleteToolBatch(t *testing.T) {
	messages := []Message{
		UserMessage{Content: []InputContent{TextInput{Text: "request"}}},
		AssistantMessage{StopReason: StopReasonToolUse, Content: []AssistantContent{ToolCall{ID: "call_a", Name: "read", Arguments: []byte(`{}`)}}},
	}
	if _, err := compactionPrefixEnds(messages, len(messages)); err == nil {
		t.Fatal("accepted an incomplete tool batch")
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
