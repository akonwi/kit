package protocol

import (
	"encoding/json"
	"testing"
)

// TestSessionEventFlatWireFixtures freezes representative records whose
// required empty identity fields and optional zero fields are easy to lose
// while converting SessionEvent to a payload union.
func TestSessionEventFlatWireFixtures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		event SessionEvent
		want  string
	}{
		{
			name:  "session scoped rename retains empty turn identity",
			event: SessionEvent{StreamID: "stream_1", Sequence: 1, SessionID: "session_1", Kind: SessionEventSessionRenamed, SessionName: "Renamed"},
			want:  `{"streamId":"stream_1","sequence":1,"sessionId":"session_1","turnId":"","runId":"","kind":"session.renamed","sessionName":"Renamed"}`,
		},
		{
			name:  "assistant delta omits zero content index",
			event: SessionEvent{StreamID: "stream_1", Sequence: 2, SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1", MessageID: "message_1", Kind: SessionEventAssistantTextDelta, Delta: "hello"},
			want:  `{"streamId":"stream_1","sequence":2,"sessionId":"session_1","turnId":"turn_1","runId":"turn_1","messageId":"message_1","kind":"assistant.text.delta","delta":"hello"}`,
		},
		{
			name:  "tool result retains raw details",
			event: SessionEvent{StreamID: "stream_1", Sequence: 3, SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1", Kind: SessionEventToolCompleted, ToolCallID: "call_1", ToolName: "read", Details: json.RawMessage(`{"lines":2}`)},
			want:  `{"streamId":"stream_1","sequence":3,"sessionId":"session_1","turnId":"turn_1","runId":"turn_1","kind":"tool.completed","toolCallId":"call_1","toolName":"read","details":{"lines":2}}`,
		},
	}
	for _, test := range cases {
		got, err := json.Marshal(test.event)
		if err != nil || string(got) != test.want {
			t.Fatalf("%s: MarshalJSON() = %s, %v; want %s", test.name, got, err, test.want)
		}
	}
}

func TestSessionEventPayloadVocabularyIsClosed(t *testing.T) {
	t.Parallel()
	variants := sessionEventVariants()
	if len(variants) != 29 {
		t.Fatalf("session event variants = %d; want 29", len(variants))
	}
	seen := make(map[SessionEventKind]bool, len(variants))
	for _, variant := range variants {
		payload := variant.Payload.(SessionEventPayload)
		if payload.sessionEventKind() != SessionEventKind(variant.Kind) {
			t.Fatalf("payload %T kind = %q; mapping = %q", payload, payload.sessionEventKind(), variant.Kind)
		}
		if seen[payload.sessionEventKind()] {
			t.Fatalf("kind %q is declared twice", payload.sessionEventKind())
		}
		seen[payload.sessionEventKind()] = true
	}
}

func TestSessionEventPayloadVariantProjectsFlatFields(t *testing.T) {
	t.Parallel()
	usage := SessionUsage{Input: 3, Output: 2, TotalTokens: 5}
	event := SessionEvent{Kind: SessionEventUsageUpdated, Usage: &usage}
	payload, err := event.PayloadVariant()
	if err != nil {
		t.Fatalf("PayloadVariant() error = %v", err)
	}
	if got := payload.(UsageUpdatedEvent).Usage; got != usage {
		t.Fatalf("usage = %#v; want %#v", got, usage)
	}
	if _, err := (SessionEvent{Kind: SessionEventUsageUpdated}).PayloadVariant(); err == nil {
		t.Fatal("nil usage was accepted")
	}
	if _, err := (SessionEvent{Kind: "unknown"}).PayloadVariant(); err == nil {
		t.Fatal("unknown kind was accepted")
	}
}
