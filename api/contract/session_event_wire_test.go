package contract

import (
	"encoding/json"
	"testing"
)

// TestSessionEventFlatWireFixtures freezes representative records whose
// omitted session identity fields and optional zero fields are easy to lose
// while converting SessionEvent to a payload union.
func TestSessionEventFlatWireFixtures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		event SessionEvent
		want  string
	}{
		{
			name:  "session scoped rename omits turn identity",
			event: SessionEvent{StreamID: "stream_1", Sequence: 1, SessionID: "session_1", Payload: SessionNameChangedEvent{SessionName: "Renamed"}},
			want:  `{"streamId":"stream_1","sequence":1,"sessionId":"session_1","kind":"session.name.changed","sessionName":"Renamed"}`,
		},
		{
			name:  "assistant delta omits zero content index",
			event: SessionEvent{StreamID: "stream_1", Sequence: 2, SessionID: "session_1", TurnID: "turn_1", Payload: AssistantTextDeltaEvent{MessageID: "message_1", Delta: "hello"}},
			want:  `{"streamId":"stream_1","sequence":2,"sessionId":"session_1","turnId":"turn_1","messageId":"message_1","kind":"assistant.text.delta","delta":"hello"}`,
		},
		{
			name:  "tool result retains raw details",
			event: SessionEvent{StreamID: "stream_1", Sequence: 3, SessionID: "session_1", TurnID: "turn_1", Payload: ToolCompletedEvent{ToolCallID: "call_1", ToolName: "read", Details: json.RawMessage(`{"lines":2}`)}},
			want:  `{"streamId":"stream_1","sequence":3,"sessionId":"session_1","turnId":"turn_1","kind":"tool.completed","toolCallId":"call_1","toolName":"read","details":{"lines":2}}`,
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
	if len(variants) != 28 {
		t.Fatalf("session event variants = %d; want 28", len(variants))
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
	event := SessionEvent{Payload: UsageChangedEvent{Usage: &usage}}
	payload, err := event.PayloadVariant()
	if err != nil {
		t.Fatalf("PayloadVariant() error = %v", err)
	}
	if got := payload.(UsageChangedEvent).Usage; got == nil || *got != usage {
		t.Fatalf("usage = %#v; want %#v", got, usage)
	}
	if _, err := (SessionEvent{}).PayloadVariant(); err == nil {
		t.Fatal("missing payload was accepted")
	}
	if got := event.Kind(); got != SessionEventUsageChanged {
		t.Fatalf("Kind() = %q", got)
	}
}

func TestSessionEventUnionCodecPreservesFlatWire(t *testing.T) {
	t.Parallel()
	wire := []byte(`{"streamId":"stream_1","sequence":2,"sessionId":"session_1","turnId":"turn_1","messageId":"message_1","kind":"assistant.text.delta","delta":"hello"}`)
	event, err := decodeSessionEventUnion(wire)
	if err != nil {
		t.Fatalf("decodeSessionEventUnion() error = %v", err)
	}
	if _, ok := event.Payload.(AssistantTextDeltaEvent); !ok {
		t.Fatalf("payload = %T", event.Payload)
	}
	got, err := encodeSessionEventUnion(event)
	if err != nil || string(got) != string(wire) {
		t.Fatalf("encode = %s, %v; want %s", got, err, wire)
	}
	if _, err := decodeSessionEventUnion([]byte(`{"streamId":"s","sequence":1,"sessionId":"s","kind":"assistant.text.delta","text":"invalid"}`)); err == nil {
		t.Fatal("cross-variant field was accepted")
	}
	if _, err := decodeSessionEventUnion([]byte(`{"streamId":"s","sequence":1,"sessionId":"s","turnId":"t","kind":"usage.changed"}`)); err == nil {
		t.Fatal("missing required payload was accepted")
	}
	if _, err := decodeSessionEventUnion([]byte(`{"streamId":"s","sequence":1,"sessionId":"s","turnId":"t","runId":"t","kind":"turn.started","status":"running"}`)); err == nil {
		t.Fatal("legacy run identity was accepted")
	}
}

func TestSessionEventPayloadVariantRetainsLiveFields(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		event SessionEvent
		check func(t *testing.T, payload SessionEventPayload)
	}{
		{SessionEvent{Payload: AssistantStartedEvent{MessageID: "message_1", Thinking: "reason"}}, func(t *testing.T, p SessionEventPayload) {
			if got := p.(AssistantStartedEvent); got.Thinking != "reason" {
				t.Fatalf("started thinking = %q", got.Thinking)
			}
		}},
		{SessionEvent{Payload: AssistantCompletedEvent{MessageID: "message_1", Text: "answer"}}, func(t *testing.T, p SessionEventPayload) {
			if got := p.(AssistantCompletedEvent); got.Text != "answer" {
				t.Fatalf("completed text = %q", got.Text)
			}
		}},
		{SessionEvent{Payload: ToolPlannedEvent{MessageID: "message_1", ContentIndex: 3, ToolCallID: "call_1", ToolName: "read"}}, func(t *testing.T, p SessionEventPayload) {
			if got := p.(ToolPlannedEvent); got.ContentIndex != 3 {
				t.Fatalf("planned content index = %d", got.ContentIndex)
			}
		}},
		{SessionEvent{Payload: ToolOutputDeltaEvent{ToolCallID: "call_1", ToolName: "read", Content: ToolResultContent{TextBlock("error")}, IsError: true}}, func(t *testing.T, p SessionEventPayload) {
			if got := p.(ToolOutputDeltaEvent); !got.IsError || len(got.Content) != 1 {
				t.Fatalf("updated payload = %#v", got)
			}
		}},
	} {
		payload, err := test.event.PayloadVariant()
		if err != nil {
			t.Fatalf("PayloadVariant() error = %v", err)
		}
		test.check(t, payload)
	}
}
