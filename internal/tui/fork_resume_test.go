package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
)

// TestResumeInstalledForkShowsFirstMessageDuringFirstTurn covers a fork whose
// first message the server admitted: the installed child is already running
// that turn, and its first message must appear before the turn ends.
func TestResumeInstalledForkShowsFirstMessageDuringFirstTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	const childID, turnID = "session_fork_child", "turn_first"
	updates := make(chan []protocol.SessionEvent, 1)
	child := &abortTestSession{fakeSession: fakeSession{id: childID}, streams: make(chan string, 4)}
	child.stream = func(string) (EventStream, error) { return abortTestStream{updates: updates}, nil }
	snapshot := protocol.SessionSnapshot{
		Session:      protocol.SessionInfo{ID: childID, Name: "fork: Parent", Model: "test/model"},
		ActiveTurnID: turnID, EventStreamID: "stream", EventCursor: 2, EventReplayAvailable: true,
	}
	child.snapshot = func(context.Context) (protocol.SessionSnapshot, error) { return snapshot, nil }
	state := &turnAbortTestState{completions: make(chan func(), 16), timeout: time.Second, cancel: cancel, appState: appState{
		ctx: ctx, attachmentCtx: ctx, phase: phaseReady, session: snapshot.Session, bound: child,
		turnPending: true, activeTurnID: turnID, turnActivity: "Working…",
	}}
	application := newAbortTestApp(t, turnAbortHarness{state}, state.dispatch)
	application.Pump(120, 36)

	state.resumeInstalledSession(child, state.operation, snapshot)
	select {
	case id := <-child.streams:
		if id != turnID {
			t.Fatalf("watched turn = %q, want %q", id, turnID)
		}
	case <-time.After(time.Second):
		t.Fatal("the fork's running first turn was not watched")
	}
	updates <- []protocol.SessionEvent{
		{StreamID: "stream", Sequence: 1, SessionID: childID, TurnID: turnID, Payload: protocol.TurnStartedEvent{}},
		{StreamID: "stream", Sequence: 2, SessionID: childID, TurnID: turnID, Payload: protocol.UserMessageAddedEvent{Text: "explore the other approach"}},
	}
	deadline := time.After(3 * time.Second)
	for !strings.Contains(application.Text(), "explore the other approach") {
		select {
		case apply := <-state.completions:
			apply()
			application.Pump(120, 36)
		case <-deadline:
			t.Fatalf("first message missing while the first turn runs:\n%s", application.Text())
		}
	}
	if !state.turnPending || state.activeTurnID != turnID {
		t.Fatalf("turn state = pending:%t active:%q, want the running first turn", state.turnPending, state.activeTurnID)
	}
}

// TestForkFirstTurnErrorFailsLikeAPromptSubmission covers a fork whose first
// message could not start: the child is installed, the message returns to the
// composer, and the failure appears in the child's transcript.
func TestForkFirstTurnErrorFailsLikeAPromptSubmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	child := &abortTestSession{fakeSession: fakeSession{id: "session_fork_child"}, streams: make(chan string, 1)}
	state := &turnAbortTestState{completions: make(chan func(), 1), timeout: time.Second, cancel: cancel, appState: appState{
		ctx: ctx, attachmentCtx: ctx, phase: phaseReady, bound: child,
		session: protocol.SessionInfo{ID: "session_fork_child", Name: "fork: Parent", Model: "test/model"},
	}}
	application := newAbortTestApp(t, turnAbortHarness{state}, state.dispatch)

	application.Pump(120, 36)
	state.SetState(func() {
		state.applyForkFirstTurnError(
			&protocol.PromptInput{Text: "explore the other approach"},
			&protocol.FirstTurnError{Code: protocol.FirstTurnUnavailable, Message: "session is unavailable"},
		)
	})
	application.Pump(120, 36)
	application.Pump(120, 36)

	if state.composer != "explore the other approach" {
		t.Fatalf("composer = %q, want the first message restored", state.composer)
	}
	want := []transcriptMessage{{Role: "error", Text: "session is unavailable"}}
	if !reflect.DeepEqual(state.messages, want) {
		t.Fatalf("messages = %+v, want %+v", state.messages, want)
	}
	if text := application.Text(); !strings.Contains(text, "session is unavailable") {
		t.Fatalf("screen missing the first-turn error:\n%s", text)
	}
}
