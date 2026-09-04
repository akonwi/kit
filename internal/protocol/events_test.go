package protocol

import "testing"

func TestSessionEventBatchValidatesExpectedTurnSequence(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 9, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", Kind: SessionEventRunStarted, Status: RunStatusRunning},
		{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", Kind: SessionEventUserMessage, Text: "hello"},
		{StreamID: "stream_test", Sequence: 6, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", MessageID: "message_test", Kind: SessionEventThinkingDelta, ContentIndex: 0, Delta: "plan"},
		{StreamID: "stream_test", Sequence: 7, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", MessageID: "message_test", Kind: SessionEventAssistantTextDelta, ContentIndex: 1, Delta: "answer"},
		{StreamID: "stream_test", Sequence: 8, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", Kind: SessionEventToolStarted, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
		{StreamID: "stream_test", Sequence: 9, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", Kind: SessionEventRunFinished, Status: RunStatusCompleted},
	}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSessionEventBatchRejectsSequenceGaps(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 5, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", Kind: SessionEventUserMessage, Text: "hello"},
		{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", MessageID: "message_test", Kind: SessionEventAssistantStarted},
	}}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted a sequence gap")
	}
}

func TestSessionEventToolPlanRequiresCompleteArguments(t *testing.T) {
	t.Parallel()

	event := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: "session_test",
		TurnID: "turn_test", RunID: "run_test", MessageID: "message_test",
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

func TestSessionEventRequiresAssistantMessageIdentity(t *testing.T) {
	t.Parallel()

	event := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: "session_test",
		TurnID: "turn_test", RunID: "run_test", Kind: SessionEventAssistantStarted,
	}
	if err := event.Validate(); err == nil {
		t.Fatal("Validate() accepted an assistant event without a message id")
	}
}

func TestSessionEventBatchRejectsAssistantIdentityChange(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 3, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", MessageID: "message_one", Kind: SessionEventAssistantStarted},
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", MessageID: "message_two", Kind: SessionEventAssistantTextDelta, Delta: "changed"},
	}}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted an assistant identity change")
	}
}

func TestSessionEventBatchScopesAssistantIdentityToRun(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 4, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_one", RunID: "run_one", MessageID: "message_one", Kind: SessionEventAssistantStarted},
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "turn_one", RunID: "run_one", Kind: SessionEventRunFinished, Status: RunStatusInterrupted, ErrorMessage: "daemon restarted"},
		{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "turn_two", RunID: "run_two", Kind: SessionEventRunStarted, Status: RunStatusRunning},
		{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "turn_two", RunID: "run_two", MessageID: "message_two", Kind: SessionEventAssistantStarted},
	}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSessionEventBatchRejectsOutOfOrderUpdates(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 5, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", Kind: SessionEventUserMessage, Text: "hello"},
		{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "turn_test", RunID: "run_test", MessageID: "message_test", Kind: SessionEventAssistantStarted},
	}}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted out-of-order updates")
	}
}
