package contract

import (
	"testing"
	"time"
)

func eventForTest(sequence int64, payload SessionEventPayload) SessionEvent {
	return SessionEvent{StreamID: "stream_test", Sequence: sequence, SessionID: "session_test", TurnID: "turn_test", Payload: payload}
}

func TestSessionScopedEventValidation(t *testing.T) {
	event := eventForTest(1, SessionNameChangedEvent{SessionName: "Renamed session"})
	event.TurnID = ""
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	event.TurnID = "turn"
	if err := event.Validate(); err == nil {
		t.Fatal("session event accepted turn identity")
	}
}

func TestSessionEventValidatesPayloadVariants(t *testing.T) {
	valid := []SessionEventPayload{
		TurnStartedEvent{Status: TurnStatusRunning}, UserMessageAddedEvent{Text: "hello"},
		AssistantStartedEvent{MessageID: "message_1"}, AssistantTextDeltaEvent{MessageID: "message_1", Delta: "hello"},
		ThinkingDeltaEvent{MessageID: "message_1", Delta: "reasoning"}, AssistantCompletedEvent{MessageID: "message_1"},
		ToolPlannedEvent{MessageID: "message_1", ToolCallID: "call_1", ToolName: "read", ArgumentsTruncated: true},
		ToolStartedEvent{ToolCallID: "call_1", ToolName: "read", ArgumentsTruncated: true},
		ToolOutputDeltaEvent{ToolCallID: "call_1", ToolName: "read", Content: ToolResultContent{TextBlock("result")}},
		ToolCompletedEvent{ToolCallID: "call_1", ToolName: "read"},
		CompactionStartedEvent{CompactionID: "compact_00000000000000000000000000000001"},
		CompactionCompletedEvent{CompactionID: "compact_00000000000000000000000000000001"},
		CompactionCompletedEvent{CompactionID: "compact_00000000000000000000000000000001", ErrorMessage: "failed"},
		ProviderRetryScheduledEvent{ProviderRetry: &ProviderRetry{Count: 1, RetryAt: time.Now().UTC().Format(time.RFC3339Nano)}},
		ProviderRetryStartedEvent{ProviderRetry: &ProviderRetry{Count: 1}}, ContextChangedEvent{ContextWindow: 100},
		UsageChangedEvent{Usage: &SessionUsage{}}, TurnCompletedEvent{Status: TurnStatusCompleted},
	}
	for _, payload := range valid {
		event := eventForTest(1, payload)
		if err := event.Validate(); err != nil {
			t.Errorf("%T: %v", payload, err)
		}
		if event.Kind() != payload.sessionEventKind() {
			t.Errorf("%T kind mismatch", payload)
		}
	}
}

func TestSessionEventBatchValidatesPayloadSequence(t *testing.T) {
	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 5, Events: []SessionEvent{
		eventForTest(1, TurnStartedEvent{Status: TurnStatusRunning}), eventForTest(2, UserMessageAddedEvent{Text: "hello"}),
		eventForTest(3, AssistantStartedEvent{MessageID: "message_1"}), eventForTest(4, AssistantTextDeltaEvent{MessageID: "message_1", Delta: "answer"}),
		eventForTest(5, TurnCompletedEvent{Status: TurnStatusCompleted}),
	}}
	if err := batch.Validate(); err != nil {
		t.Fatal(err)
	}
	batch.Events[3].Payload = AssistantTextDeltaEvent{MessageID: "message_2", Delta: "answer"}
	if err := batch.Validate(); err == nil {
		t.Fatal("batch accepted assistant identity change")
	}
}

func TestSessionEventRejectsMissingPayload(t *testing.T) {
	if err := eventForTest(1, nil).Validate(); err == nil {
		t.Fatal("missing payload was valid")
	}
}
