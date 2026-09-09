package protocol

import (
	"encoding/json"
	"testing"
)

func TestSessionEventBatchValidatesExpectedTurnSequence(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 9, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventRunStarted, Status: RunStatusRunning},
		{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventUserMessage, Text: "hello"},
		{StreamID: "stream_test", Sequence: 6, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_test", Kind: SessionEventThinkingDelta, ContentIndex: 0, Delta: "plan"},
		{StreamID: "stream_test", Sequence: 7, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_test", Kind: SessionEventAssistantTextDelta, ContentIndex: 1, Delta: "answer"},
		{StreamID: "stream_test", Sequence: 8, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventToolStarted, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
		{StreamID: "stream_test", Sequence: 9, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventRunFinished, Status: RunStatusCompleted},
	}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSessionEventBatchValidatesAbsoluteUsageUpdates(t *testing.T) {
	t.Parallel()

	first := SessionUsage{Input: 10, TotalTokens: 10, Cost: SessionUsageCost{Total: 0.1}}
	second := SessionUsage{Input: 20, Output: 5, TotalTokens: 25, Cost: SessionUsageCost{Total: 0.2}}
	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 2, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventUsageUpdated, Usage: &first},
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventUsageUpdated, Usage: &second},
	}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	regressed := SessionUsage{Input: 9, TotalTokens: 9, Cost: SessionUsageCost{Total: 0.09}}
	batch.Events[1].Usage = &regressed
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted decreasing session usage")
	}
	batch = SessionEventBatch{
		StreamID: "stream_test", FirstSequence: 1, LastSequence: 2, UsageBaseline: &first,
		Events: []SessionEvent{{
			StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test",
			Kind: SessionEventUsageUpdated, Usage: &regressed,
		}},
	}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted a decrease from the preceding page baseline")
	}
}

func TestSessionEventBatchRejectsSequenceGaps(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 5, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventUserMessage, Text: "hello"},
		{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_test", Kind: SessionEventAssistantStarted},
	}}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted a sequence gap")
	}
}

func TestSessionEventToolPlanRequiresCompleteArguments(t *testing.T) {
	t.Parallel()

	event := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: "session_test",
		TurnID: "turn_test", RunID: "turn_test", MessageID: "message_test",
		Kind: SessionEventToolPlanned, ToolCallID: "call_test", ToolName: "read",
	}
	if err := event.Validate(); err == nil {
		t.Fatal("Validate() accepted a planned tool call without arguments")
	}
	event.Arguments = `{"path":"README.md"}`
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSessionEventValidatesStructuredToolResultLifecycle(t *testing.T) {
	t.Parallel()

	update := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: "session_test",
		TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventToolUpdated,
		ToolCallID: "call_test", ToolName: "read",
		Content: []TranscriptContent{{Kind: TranscriptContentText, Text: "chunk"}},
	}
	if err := update.Validate(); err != nil {
		t.Fatalf("update Validate() error = %v", err)
	}
	completed := update
	completed.Kind = SessionEventToolCompleted
	completed.Content = []TranscriptContent{
		{Kind: TranscriptContentText, Text: "complete"},
		{Kind: TranscriptContentFile, Filename: "report.txt", MediaType: "text/plain"},
	}
	completed.Details = json.RawMessage(`{"lines":3}`)
	if err := completed.Validate(); err != nil {
		t.Fatalf("completed Validate() error = %v", err)
	}
	completed.Text = "flattened"
	if err := completed.Validate(); err == nil {
		t.Fatal("Validate() accepted flattened tool text")
	}
	completed.Text = ""
	completed.Content = make([]TranscriptContent, maxSessionEventContentBlocks+1)
	if err := completed.Validate(); err == nil {
		t.Fatal("Validate() accepted too many tool content blocks")
	}
}

func TestSessionEventRequiresAssistantMessageIdentity(t *testing.T) {
	t.Parallel()

	event := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: "session_test",
		TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventAssistantStarted,
	}
	if err := event.Validate(); err == nil {
		t.Fatal("Validate() accepted an assistant event without a message id")
	}
}

func TestSessionEventBatchRejectsAssistantIdentityChange(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 3, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_one", Kind: SessionEventAssistantStarted},
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_two", Kind: SessionEventAssistantTextDelta, Delta: "changed"},
	}}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted an assistant identity change")
	}
}

func TestSessionEventBatchScopesAssistantIdentityToRun(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 4, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_one", RunID: "turn_one", MessageID: "message_one", Kind: SessionEventAssistantStarted},
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "turn_one", RunID: "turn_one", Kind: SessionEventRunFinished, Status: RunStatusInterrupted, ErrorMessage: "daemon restarted"},
		{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "turn_two", RunID: "turn_two", Kind: SessionEventRunStarted, Status: RunStatusRunning},
		{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "turn_two", RunID: "turn_two", MessageID: "message_two", Kind: SessionEventAssistantStarted},
	}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSessionEventBatchRejectsOutOfOrderUpdates(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 5, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventUserMessage, Text: "hello"},
		{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_test", Kind: SessionEventAssistantStarted},
	}}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted out-of-order updates")
	}
}
