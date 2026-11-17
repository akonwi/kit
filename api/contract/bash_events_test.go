package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func runningBash() *BashExecution {
	return &BashExecution{
		ID: "bash_1", SessionID: "session_1", Sequence: 4, Command: "make test",
		Status: BashExecutionRunning, ExcludeFromContext: true, StartedAt: "2026-01-01T00:00:00Z",
	}
}

func settledBash() *BashExecution {
	code := 2
	execution := runningBash()
	execution.Status, execution.Output, execution.ExitCode, execution.CompletedAt = BashExecutionCompleted, "FAIL", &code, "2026-01-01T00:00:01Z"
	return execution
}

func TestBashEventsEncodeTheExecution(t *testing.T) {
	t.Parallel()
	event := SessionEvent{StreamID: "stream_1", Sequence: 7, SessionID: "session_1", Payload: BashCompletedEvent{BashExecution: settledBash()}}
	got, err := json.Marshal(event)
	want := `{"streamId":"stream_1","sequence":7,"sessionId":"session_1","kind":"bash.completed","bashExecution":{"id":"bash_1","sessionId":"session_1","sequence":4,"command":"make test","status":"completed","output":"FAIL","exitCode":2,"excludeFromContext":true,"startedAt":"2026-01-01T00:00:00Z","completedAt":"2026-01-01T00:00:01Z"}}`
	if err != nil || string(got) != want {
		t.Fatalf("Marshal() = %s, %v; want %s", got, err, want)
	}
	var decoded SessionEvent
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	payload, ok := decoded.Payload.(BashCompletedEvent)
	if !ok || payload.BashExecution.Output != "FAIL" || *payload.BashExecution.ExitCode != 2 {
		t.Fatalf("decoded payload = %#v", decoded.Payload)
	}
}

func TestBashEventsRejectInconsistentExecutions(t *testing.T) {
	t.Parallel()
	otherSession := runningBash()
	otherSession.SessionID = "session_2"
	cases := map[string]SessionEvent{
		"started settled":     {StreamID: "stream_1", Sequence: 1, SessionID: "session_1", Payload: BashStartedEvent{BashExecution: settledBash()}},
		"completed running":   {StreamID: "stream_1", Sequence: 1, SessionID: "session_1", Payload: BashCompletedEvent{BashExecution: runningBash()}},
		"turn identity":       {StreamID: "stream_1", Sequence: 1, SessionID: "session_1", TurnID: "turn_1", Payload: BashStartedEvent{BashExecution: runningBash()}},
		"another session":     {StreamID: "stream_1", Sequence: 1, SessionID: "session_1", Payload: BashStartedEvent{BashExecution: otherSession}},
		"missing execution":   {StreamID: "stream_1", Sequence: 1, SessionID: "session_1", Payload: BashStartedEvent{}},
		"invalid execution":   {StreamID: "stream_1", Sequence: 1, SessionID: "session_1", Payload: BashStartedEvent{BashExecution: &BashExecution{ID: "bash_1", SessionID: "session_1", Status: BashExecutionRunning}}},
		"unrecognized as own": {StreamID: "stream_1", Sequence: 1, SessionID: "session_1", Payload: UnrecognizedSessionEvent{Kind: SessionEventBashStarted}},
	}
	for name, event := range cases {
		if err := event.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil; want error", name)
		}
	}
	var decoded SessionEvent
	err := json.Unmarshal([]byte(`{"streamId":"stream_1","sequence":1,"sessionId":"session_1","kind":"follow_ups.changed","followUps":{"previews":[]},"bashExecution":{}}`), &decoded)
	if err == nil || !strings.Contains(err.Error(), "does not carry bashExecution") {
		t.Fatalf("known kind with a bash execution: Unmarshal() = %v", err)
	}
}

func TestSessionEventsOfUnknownKindsAreSkippable(t *testing.T) {
	t.Parallel()
	data := `{"streamId":"stream_1","sequence":8,"sessionId":"session_1","kind":"widget.spun","widget":{"speed":3}}`
	var event SessionEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}
	if event.Kind() != "widget.spun" || event.StreamID != "stream_1" || event.Sequence != 8 || event.SessionID != "session_1" {
		t.Fatalf("decoded event = %#v", event)
	}
	if _, ok := event.Payload.(UnrecognizedSessionEvent); !ok {
		t.Fatalf("payload = %T; want UnrecognizedSessionEvent", event.Payload)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	encoded, err := json.Marshal(event)
	if err != nil || string(encoded) != `{"streamId":"stream_1","sequence":8,"sessionId":"session_1","kind":"widget.spun"}` {
		t.Fatalf("Marshal() = %s, %v", encoded, err)
	}

	batch := SessionEventBatch{StreamID: "stream_1", FirstSequence: 7, LastSequence: 9}
	for _, raw := range []string{
		`{"streamId":"stream_1","sequence":7,"sessionId":"session_1","kind":"session.name.changed","sessionName":"named"}`,
		data,
		`{"streamId":"stream_1","sequence":9,"sessionId":"session_1","kind":"bash.started","bashExecution":{"id":"bash_1","sessionId":"session_1","sequence":4,"command":"make test","status":"running","startedAt":"2026-01-01T00:00:00Z"}}`,
	} {
		var decoded SessionEvent
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("Unmarshal(%s) = %v", raw, err)
		}
		batch.Events = append(batch.Events, decoded)
	}
	if err := batch.Validate(); err != nil {
		t.Fatalf("batch with an unknown kind: Validate() = %v", err)
	}
}

func TestSessionEventsOfUnknownKindsStillNeedCommonMembers(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"invalid kind":      `{"streamId":"stream_1","sequence":1,"sessionId":"session_1","kind":"Widget Spun"}`,
		"kind without verb": `{"streamId":"stream_1","sequence":1,"sessionId":"session_1","kind":"widget"}`,
		"mistyped sequence": `{"streamId":"stream_1","sequence":"1","sessionId":"session_1","kind":"widget.spun"}`,
	} {
		var event SessionEvent
		if err := json.Unmarshal([]byte(data), &event); err == nil {
			t.Errorf("%s: Unmarshal() = nil; want error", name)
		}
	}
	for name, data := range map[string]string{
		"missing stream":   `{"sequence":1,"sessionId":"session_1","kind":"widget.spun"}`,
		"missing sequence": `{"streamId":"stream_1","sessionId":"session_1","kind":"widget.spun"}`,
		"missing session":  `{"streamId":"stream_1","sequence":1,"kind":"widget.spun"}`,
	} {
		var event SessionEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			t.Fatalf("%s: Unmarshal() = %v", name, err)
		}
		if err := event.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil; want error", name)
		}
	}
}

func TestSessionSnapshotSkipsUnknownTopLevelMembers(t *testing.T) {
	t.Parallel()
	snapshot := validTranscriptSnapshot()
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	extended := strings.TrimSuffix(string(encoded), "}") + `,"widgets":[{"speed":3}]}`
	var decoded SessionSnapshot
	if err := json.Unmarshal([]byte(extended), &decoded); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}
	if decoded.Session.ID != "session_1" || len(decoded.Messages) != 2 {
		t.Fatalf("decoded snapshot = %#v", decoded)
	}
	nested := strings.Replace(string(encoded), `"session":{`, `"session":{"widget":1,`, 1)
	if err := json.Unmarshal([]byte(nested), &decoded); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown member inside a known member: Unmarshal() = %v; want an unknown field error", err)
	}
}

func TestSessionSnapshotBashExecutions(t *testing.T) {
	t.Parallel()
	settled := *settledBash()
	running := BashExecution{ID: "bash_2", SessionID: "session_1", Sequence: 5, Command: "ls", Status: BashExecutionRunning, StartedAt: "2026-01-01T00:00:02Z"}
	valid := validTranscriptSnapshot()
	valid.ActiveBashExecutionID = running.ID
	valid.BashExecutions = []BashExecution{settled, running}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	outOfOrder := valid
	outOfOrder.BashExecutions = []BashExecution{running, settled}
	notActive := valid
	notActive.ActiveBashExecutionID = ""
	unlisted := valid
	unlisted.BashExecutions = []BashExecution{settled}
	duplicate := valid
	duplicate.ActiveBashExecutionID = ""
	duplicate.BashExecutions = []BashExecution{settled, settled}
	tooMany := validTranscriptSnapshot()
	for index := range MaxSnapshotBashExecutions + 1 {
		execution := settled
		execution.ID, execution.Sequence = "bash_"+string(rune('a'+index%26))+string(rune('a'+index/26)), int64(index)
		tooMany.BashExecutions = append(tooMany.BashExecutions, execution)
	}
	for name, snapshot := range map[string]SessionSnapshot{
		"out of order": outOfOrder, "running but not active": notActive, "active not listed": unlisted,
		"duplicate": duplicate, "too many": tooMany,
	} {
		if err := snapshot.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil; want error", name)
		}
	}
}
