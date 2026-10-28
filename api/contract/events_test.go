package contract

import (
	"encoding/json"
	"strings"
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
		PluginMessageAddedEvent{PluginID: "autoresearch", Text: "Continue."},
		AssistantStartedEvent{MessageID: "message_1"}, AssistantTextDeltaEvent{MessageID: "message_1", Delta: "hello"},
		ThinkingDeltaEvent{MessageID: "message_1", Delta: "reasoning"}, AssistantCompletedEvent{MessageID: "message_1"},
		ToolPlannedEvent{MessageID: "message_1", ToolCallID: "call_1", ToolName: "read", ArgumentsTruncated: true},
		ToolStartedEvent{ToolCallID: "call_1", ToolName: "read", ArgumentsTruncated: true},
		ToolOutputDeltaEvent{ToolCallID: "call_1", ToolName: "read", Content: ToolResultContent{TextBlock("result")}},
		ToolCompletedEvent{ToolCallID: "call_1", ToolName: "read"},
		CompactionStartedEvent{CompactionID: "compact_00000000000000000000000000000001"},
		CompactionCompletedEvent{CompactionID: "compact_00000000000000000000000000000001"},
		CompactionCompletedEvent{CompactionID: "compact_00000000000000000000000000000001", CompactionFailureReason: CompactionFailureSummaryTruncated},
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

func TestPluginMessageEventRequiresTurnTextAndPluginID(t *testing.T) {
	for name, event := range map[string]SessionEvent{
		"missing text":      eventForTest(1, PluginMessageAddedEvent{PluginID: "autoresearch"}),
		"missing plugin":    eventForTest(1, PluginMessageAddedEvent{Text: "Continue."}),
		"invalid plugin id": eventForTest(1, PluginMessageAddedEvent{PluginID: "Auto Research", Text: "Continue."}),
		"session scoped":    {StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Payload: PluginMessageAddedEvent{PluginID: "autoresearch", Text: "Continue."}},
	} {
		if err := event.Validate(); err == nil {
			t.Errorf("%s: Validate() accepted %+v", name, event)
		}
	}
	// Only plugin messages may carry a plugin id on the flat wire.
	var decoded SessionEvent
	err := json.Unmarshal([]byte(`{"streamId":"stream_test","sequence":1,"sessionId":"session_test","turnId":"turn_test","kind":"user.message.added","text":"hello","pluginId":"autoresearch"}`), &decoded)
	if err == nil {
		err = decoded.Validate()
	}
	if err == nil {
		t.Fatal("user message accepted a plugin id")
	}
}

func TestURLOpenRequestIsASessionScopedCanonicalWebAddress(t *testing.T) {
	session := func(payload URLOpenRequestedEvent) SessionEvent {
		return SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Payload: payload}
	}
	for _, payload := range []URLOpenRequestedEvent{{URL: "https://example.com/docs?q=1#top"}, {URL: "http://localhost:3000/"}, {}} {
		if err := session(payload).Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v", payload, err)
		}
	}
	for name, event := range map[string]SessionEvent{
		"turn scoped":    eventForTest(1, URLOpenRequestedEvent{URL: "https://example.com/"}),
		"file address":   session(URLOpenRequestedEvent{URL: "file:///etc/passwd"}),
		"script address": session(URLOpenRequestedEvent{URL: "javascript:alert(1)"}),
		"credentials":    session(URLOpenRequestedEvent{URL: "https://user:secret@example.com/"}),
		"not canonical":  session(URLOpenRequestedEvent{URL: "HTTPS://example.com/"}),
		"relative":       session(URLOpenRequestedEvent{URL: "/docs"}),
		"whitespace":     session(URLOpenRequestedEvent{URL: "https://example.com/a b"}),
		"oversized":      session(URLOpenRequestedEvent{URL: "https://example.com/" + strings.Repeat("a", 2048)}),
	} {
		if err := event.Validate(); err == nil {
			t.Errorf("%s: Validate() accepted %+v", name, event)
		}
	}
	// Only url open requests may carry a url on the flat wire.
	var decoded SessionEvent
	err := json.Unmarshal([]byte(`{"streamId":"stream_test","sequence":1,"sessionId":"session_test","kind":"session.name.changed","sessionName":"Renamed","url":"https://example.com/"}`), &decoded)
	if err == nil {
		err = decoded.Validate()
	}
	if err == nil {
		t.Fatal("rename accepted a url")
	}
	// A request between a turn's events does not disturb its assistant ordering.
	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 3, Events: []SessionEvent{
		eventForTest(1, AssistantTextDeltaEvent{MessageID: "message_1", Delta: "a"}),
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", Payload: URLOpenRequestedEvent{URL: "https://example.com/"}},
		eventForTest(3, AssistantTextDeltaEvent{MessageID: "message_2", Delta: "b"}),
	}}
	if err := batch.Validate(); err == nil {
		t.Fatal("batch accepted an assistant identity change across a url open request")
	}
}

func TestFollowUpsChangedIsASessionScopedQueue(t *testing.T) {
	session := func(payload SessionEventPayload) SessionEvent {
		return SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Payload: payload}
	}
	for name, queue := range map[string]FollowUpQueue{
		"empty":  {},
		"queued": {Count: 2, Previews: []string{"one", "two"}, AnnotationIDs: []uint64{4}},
		"held":   {Count: 1, Previews: []string{"one"}, AdmissionFailure: &FollowUpAdmissionFailure{Message: "attachment is unavailable"}},
	} {
		if err := session(FollowUpsChangedEvent{FollowUps: &queue}).Validate(); err != nil {
			t.Errorf("%s: Validate() = %v", name, err)
		}
	}
	held := FollowUpQueue{Count: 1, AdmissionFailure: &FollowUpAdmissionFailure{Message: "failed"}}
	for name, event := range map[string]SessionEvent{
		"turn scoped":          eventForTest(1, FollowUpsChangedEvent{FollowUps: &held}),
		"missing queue":        session(FollowUpsChangedEvent{}),
		"failure without work": session(FollowUpsChangedEvent{FollowUps: &FollowUpQueue{AdmissionFailure: &FollowUpAdmissionFailure{Message: "failed"}}}),
		"blank failure":        session(FollowUpsChangedEvent{FollowUps: &FollowUpQueue{Count: 1, AdmissionFailure: &FollowUpAdmissionFailure{Message: " "}}}),
		"control failure":      session(FollowUpsChangedEvent{FollowUps: &FollowUpQueue{Count: 1, AdmissionFailure: &FollowUpAdmissionFailure{Message: "a\x1bb"}}}),
		"negative count":       session(FollowUpsChangedEvent{FollowUps: &FollowUpQueue{Count: -1}}),
	} {
		if err := event.Validate(); err == nil {
			t.Errorf("%s: Validate() accepted %+v", name, event)
		}
	}
	// Only follow-up queue events may carry a queue on the flat wire.
	var decoded SessionEvent
	err := json.Unmarshal([]byte(`{"streamId":"stream_test","sequence":1,"sessionId":"session_test","kind":"session.name.changed","sessionName":"Renamed","followUps":{"count":0}}`), &decoded)
	if err == nil {
		err = decoded.Validate()
	}
	if err == nil {
		t.Fatal("rename accepted a follow-up queue")
	}
	// A queue change between a turn's events does not disturb its assistant ordering.
	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 3, Events: []SessionEvent{
		eventForTest(1, AssistantTextDeltaEvent{MessageID: "message_1", Delta: "a"}),
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", Payload: FollowUpsChangedEvent{FollowUps: &FollowUpQueue{Count: 1, Previews: []string{"next"}}}},
		eventForTest(3, AssistantTextDeltaEvent{MessageID: "message_1", Delta: "b"}),
	}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("batch rejected a queue change inside a turn: %v", err)
	}
}

func TestSessionConfigurationChangedIsASessionScopedConfiguration(t *testing.T) {
	session := func(configuration *SessionConfiguration) SessionEvent {
		return SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Payload: SessionConfigurationChangedEvent{Configuration: configuration}}
	}
	if err := session(&SessionConfiguration{Model: "openai/gpt-5", ThinkingLevel: "low", ConfigurationRevision: 2}).Validate(); err != nil {
		t.Fatal(err)
	}
	turnScoped := session(&SessionConfiguration{Model: "openai/gpt-5", ThinkingLevel: "low", ConfigurationRevision: 2})
	turnScoped.TurnID = "turn_test"
	for name, event := range map[string]SessionEvent{
		"turn scoped":      turnScoped,
		"missing":          session(nil),
		"unnamespaced":     session(&SessionConfiguration{Model: "gpt-5", ThinkingLevel: "low", ConfigurationRevision: 2}),
		"unknown thinking": session(&SessionConfiguration{Model: "openai/gpt-5", ThinkingLevel: "extreme", ConfigurationRevision: 2}),
		"missing thinking": session(&SessionConfiguration{Model: "openai/gpt-5", ConfigurationRevision: 2}),
		"zero revision":    session(&SessionConfiguration{Model: "openai/gpt-5", ThinkingLevel: "low"}),
		"unknown input":    session(&SessionConfiguration{Model: "openai/gpt-5", ThinkingLevel: "low", Inputs: []ModelInputKind{"audio"}, ConfigurationRevision: 2}),
	} {
		if err := event.Validate(); err == nil {
			t.Errorf("%s: Validate() accepted %+v", name, event)
		}
	}
}

func TestCompactionEventsMayBelongToTheSession(t *testing.T) {
	for _, payload := range []SessionEventPayload{
		CompactionStartedEvent{CompactionID: "compact_1"},
		CompactionCompletedEvent{CompactionID: "compact_1"},
		CompactionCompletedEvent{CompactionID: "compact_1", CompactionFailureReason: CompactionFailureProviderError},
	} {
		event := SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Payload: payload}
		if err := event.Validate(); err != nil {
			t.Errorf("%T: %v", payload, err)
		}
	}
	// Other turn events still require their turn.
	if err := (SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Payload: TurnStartedEvent{Status: TurnStatusRunning}}).Validate(); err == nil {
		t.Fatal("turn start accepted no turn identity")
	}
}

func TestSessionEventRejectsMissingPayload(t *testing.T) {
	if err := eventForTest(1, nil).Validate(); err == nil {
		t.Fatal("missing payload was valid")
	}
}

func TestBoundaryCreatedEventIsASessionScopedRecord(t *testing.T) {
	valid := BoundaryCreatedEvent{
		BoundaryID: "bash_1", BoundaryKind: "bash", BoundarySource: "composer",
		Content:   ContextContent{TextBlock("[bash from composer]\n[bash command: ls]")},
		Details:   json.RawMessage(`{"version":1,"command":"ls","status":"completed","startedAt":"2026-01-01T00:00:00Z","completedAt":"2026-01-01T00:00:01Z"}`),
		CreatedAt: "2026-01-01T00:00:01Z",
	}
	event := SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Payload: valid}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	omitted := valid
	omitted.Details, omitted.DetailsOmitted = nil, true
	if err := (SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Payload: omitted}).Validate(); err != nil {
		t.Fatalf("omitted details: %v", err)
	}
	for name, payload := range map[string]BoundaryCreatedEvent{
		"missing identity": func() BoundaryCreatedEvent { value := valid; value.BoundaryID = ""; return value }(),
		"missing kind":     func() BoundaryCreatedEvent { value := valid; value.BoundaryKind = ""; return value }(),
		"missing content":  func() BoundaryCreatedEvent { value := valid; value.Content = nil; return value }(),
		"invalid time":     func() BoundaryCreatedEvent { value := valid; value.CreatedAt = "yesterday"; return value }(),
		"invalid details": func() BoundaryCreatedEvent {
			value := valid
			value.Details = json.RawMessage(`{"version":2}`)
			return value
		}(),
	} {
		if err := (SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Payload: payload}).Validate(); err == nil {
			t.Errorf("%s: Validate() accepted %+v", name, payload)
		}
	}
	if err := eventForTest(1, valid).Validate(); err == nil {
		t.Fatal("boundary event accepted a turn identity")
	}
}

func TestCompactionFailureReasonsAreClosed(t *testing.T) {
	for name, event := range map[string]SessionEvent{
		"undeclared compaction reason": eventForTest(1, CompactionCompletedEvent{CompactionID: "compact_1", CompactionFailureReason: "disk_full"}),
		"reason on a completed turn":   eventForTest(1, TurnCompletedEvent{Status: TurnStatusCompleted, CompactionFailureReason: CompactionFailureProviderError}),
		"reason on an aborted turn":    eventForTest(1, TurnCompletedEvent{Status: TurnStatusAborted, ErrorMessage: "Stopped", CompactionFailureReason: CompactionFailureCanceled}),
		"undeclared turn reason":       eventForTest(1, TurnCompletedEvent{Status: TurnStatusFailed, ErrorMessage: "Context compaction failed", CompactionFailureReason: "disk_full"}),
	} {
		if err := event.Validate(); err == nil {
			t.Errorf("%s: Validate() accepted %+v", name, event)
		}
	}
	failed := eventForTest(1, TurnCompletedEvent{Status: TurnStatusFailed, ErrorMessage: "Context compaction failed", CompactionFailureReason: CompactionFailureSummaryInvalid})
	if err := failed.Validate(); err != nil {
		t.Fatalf("failed turn with a reason: %v", err)
	}

	turn := TurnInfo{SessionID: "session_1", TurnID: "turn_1", Status: TurnStatusFailed, ErrorMessage: "Context compaction failed", CompactionFailureReason: CompactionFailureConflict}
	if err := turn.Validate(); err != nil {
		t.Fatalf("turn info with a reason: %v", err)
	}
	turn.CompactionFailureReason = "disk_full"
	if err := turn.Validate(); err == nil {
		t.Fatal("turn info accepted an undeclared reason")
	}
	outcome := PromptOutcome{SessionID: "session_1", TurnID: "turn_1", Status: TurnStatusCompleted, CompactionFailureReason: CompactionFailureConflict}
	if err := outcome.Validate(); err == nil {
		t.Fatal("completed outcome accepted a compaction failure reason")
	}
	if err := (CompactionFailedErrorDetails{Reason: "disk_full"}).Validate(); err == nil {
		t.Fatal("error details accepted an undeclared reason")
	}
}
