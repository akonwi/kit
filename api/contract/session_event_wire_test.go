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
			name:  "plugin message carries its plugin id",
			event: SessionEvent{StreamID: "stream_1", Sequence: 2, SessionID: "session_1", TurnID: "turn_1", Payload: PluginMessageAddedEvent{PluginID: "autoresearch", Text: "Continue."}},
			want:  `{"streamId":"stream_1","sequence":2,"sessionId":"session_1","turnId":"turn_1","kind":"plugin.message.added","text":"Continue.","pluginId":"autoresearch"}`,
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
		{
			name: "boundary record omits turn identity",
			event: SessionEvent{StreamID: "stream_1", Sequence: 4, SessionID: "session_1", Payload: BoundaryCreatedEvent{
				BoundaryID: "cwd:1", BoundaryKind: "cwd", BoundarySource: "user", Content: ContextContent{TextBlock("[cwd from user]")},
				Details: json.RawMessage(`{"version":1}`), CreatedAt: "2026-01-01T00:00:00Z",
			}},
			want: `{"streamId":"stream_1","sequence":4,"sessionId":"session_1","kind":"boundary.created","content":[{"kind":"text","text":"[cwd from user]"}],"details":{"version":1},"boundaryId":"cwd:1","boundaryKind":"cwd","boundarySource":"user","createdAt":"2026-01-01T00:00:00Z"}`,
		},
		{
			name:  "live url open request carries its address",
			event: SessionEvent{StreamID: "stream_1", Sequence: 5, SessionID: "session_1", Payload: URLOpenRequestedEvent{URL: "https://example.com/docs"}},
			want:  `{"streamId":"stream_1","sequence":5,"sessionId":"session_1","kind":"url.open.requested","url":"https://example.com/docs"}`,
		},
		{
			name:  "replayed url open request omits its address",
			event: SessionEvent{StreamID: "stream_1", Sequence: 6, SessionID: "session_1", Payload: URLOpenRequestedEvent{}},
			want:  `{"streamId":"stream_1","sequence":6,"sessionId":"session_1","kind":"url.open.requested"}`,
		},
		{
			name: "client plugin call request on its instance's stream carries the call",
			event: SessionEvent{StreamID: "stream_1", Sequence: 7, SessionID: "session_1", Payload: ClientPluginCallRequestedEvent{
				ClientPluginCallID: "clientcall_0123456789abcdef0123456789abcdef", ClientPluginInstanceID: "clientplugin_0123456789abcdef0123456789abcdef",
				ClientPluginCall: &ClientPluginCall{PluginID: "browser", ToolID: "snapshot", ToolCallID: "call_1", TurnID: "run_1", Input: json.RawMessage(`{"tab":"t1"}`)},
			}},
			want: `{"streamId":"stream_1","sequence":7,"sessionId":"session_1","kind":"client_plugin.call.requested","clientPluginCallId":"clientcall_0123456789abcdef0123456789abcdef","clientPluginInstanceId":"clientplugin_0123456789abcdef0123456789abcdef","clientPluginCall":{"pluginId":"browser","toolId":"snapshot","toolCallId":"call_1","turnId":"run_1","input":{"tab":"t1"}}}`,
		},
		{
			name: "client plugin call request elsewhere omits the call",
			event: SessionEvent{StreamID: "stream_1", Sequence: 8, SessionID: "session_1", Payload: ClientPluginCallRequestedEvent{
				ClientPluginCallID: "clientcall_0123456789abcdef0123456789abcdef", ClientPluginInstanceID: "clientplugin_0123456789abcdef0123456789abcdef",
			}},
			want: `{"streamId":"stream_1","sequence":8,"sessionId":"session_1","kind":"client_plugin.call.requested","clientPluginCallId":"clientcall_0123456789abcdef0123456789abcdef","clientPluginInstanceId":"clientplugin_0123456789abcdef0123456789abcdef"}`,
		},
		{
			name: "client plugin call resolution",
			event: SessionEvent{StreamID: "stream_1", Sequence: 9, SessionID: "session_1", Payload: ClientPluginCallResolvedEvent{
				ClientPluginCallID: "clientcall_0123456789abcdef0123456789abcdef", ClientPluginInstanceID: "clientplugin_0123456789abcdef0123456789abcdef", ClientPluginCallResolution: ClientPluginCallTimedOut,
			}},
			want: `{"streamId":"stream_1","sequence":9,"sessionId":"session_1","kind":"client_plugin.call.resolved","clientPluginCallId":"clientcall_0123456789abcdef0123456789abcdef","clientPluginInstanceId":"clientplugin_0123456789abcdef0123456789abcdef","clientPluginCallResolution":"timed_out"}`,
		},
		{
			name: "held follow-up queue carries its admission failure",
			event: SessionEvent{StreamID: "stream_1", Sequence: 10, SessionID: "session_1", Payload: FollowUpsChangedEvent{FollowUps: &FollowUpQueue{
				Count: 1, Previews: []string{"next"}, AdmissionFailure: &FollowUpAdmissionFailure{Message: "attachment is unavailable"},
			}}},
			want: `{"streamId":"stream_1","sequence":10,"sessionId":"session_1","kind":"follow_ups.changed","followUps":{"count":1,"previews":["next"],"admissionFailure":{"message":"attachment is unavailable"}}}`,
		},
		{
			name:  "empty follow-up queue keeps its count",
			event: SessionEvent{StreamID: "stream_1", Sequence: 11, SessionID: "session_1", Payload: FollowUpsChangedEvent{FollowUps: &FollowUpQueue{}}},
			want:  `{"streamId":"stream_1","sequence":11,"sessionId":"session_1","kind":"follow_ups.changed","followUps":{"count":0}}`,
		},
		{
			name: "session configuration",
			event: SessionEvent{StreamID: "stream_1", Sequence: 12, SessionID: "session_1", Payload: SessionConfigurationChangedEvent{Configuration: &SessionConfiguration{
				Model: "openai/gpt-5", ThinkingLevel: "high", Inputs: []ModelInputKind{ModelInputText}, ConfigurationRevision: 3,
			}}},
			want: `{"streamId":"stream_1","sequence":12,"sessionId":"session_1","kind":"session.configuration.changed","configuration":{"model":"openai/gpt-5","thinkingLevel":"high","inputs":["text"],"configurationRevision":3}}`,
		},
		{
			name:  "session compaction omits turn identity",
			event: SessionEvent{StreamID: "stream_1", Sequence: 13, SessionID: "session_1", Payload: CompactionCompletedEvent{CompactionID: "compact_1", ErrorMessage: "Context compaction failed"}},
			want:  `{"streamId":"stream_1","sequence":13,"sessionId":"session_1","kind":"compaction.completed","errorMessage":"Context compaction failed","compactionId":"compact_1"}`,
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
	if len(variants) != 35 {
		t.Fatalf("session event variants = %d; want 35", len(variants))
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
