package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

// Drive the mounted shell through real frame timestamps so toast animations
// settle deterministically without wall-clock sleeps or inline async dispatch.
type abortTestApp struct {
	t       *testing.T
	app     *ui.App
	runner  *ui.Runner
	backend *abortTestBackend
	now     time.Time
}

type abortTestBackend struct {
	toastAnimationBackend
	dispatch func(func())
}

func (b *abortTestBackend) Dispatch(f func()) { b.dispatch(f) }

func newAbortTestApp(t *testing.T, root ui.Widget, dispatch func(func())) *abortTestApp {
	backend := &abortTestBackend{toastAnimationBackend: toastAnimationBackend{events: make(chan ui.Event), size: ui.Size{Width: 120, Height: 36}}, dispatch: dispatch}
	app := ui.NewApp(root)
	runner := ui.NewRunner(app, backend, ui.NewFrameScheduler(time.Second/60))
	now := time.Now()
	runner.Start(now)
	return &abortTestApp{t: t, app: app, runner: runner, backend: backend, now: now}
}
func (a *abortTestApp) Pump(int, int) {
	a.t.Helper()
	a.app.RequestFrame()
	a.now = a.now.Add(350 * time.Millisecond)
	if err := a.runner.HandleFrame(a.now); err != nil {
		a.t.Fatal(err)
	}
}
func (a *abortTestApp) Send(event ui.Event) { a.app.Send(event) }
func (a *abortTestApp) Click(x, y int) {
	a.Send(vaxis.Mouse{Col: x, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
}
func (a *abortTestApp) Text() string { return strings.Join(toastPainterRows(a.backend.painter), "\n") }

type abortTestSession struct {
	fakeSession
	abort    func(context.Context, string) error
	snapshot func(context.Context) (protocol.SessionSnapshot, error)
	streams  chan string
	stream   func(string) (sessionclient.EventStream, error)
}

func (s *abortTestSession) Abort(ctx context.Context, id string) error { return s.abort(ctx, id) }
func (s *abortTestSession) Snapshot(ctx context.Context) (protocol.SessionSnapshot, error) {
	return s.snapshot(ctx)
}

type abortTestStream struct{ updates chan []protocol.SessionEvent }

func (s abortTestStream) Updates() <-chan []protocol.SessionEvent { return s.updates }
func (abortTestStream) Err() error                                { return nil }
func (s *abortTestSession) Stream(_ context.Context, id string) (sessionclient.EventStream, error) {
	s.streams <- id
	if s.stream != nil {
		return s.stream(id)
	}
	return abortTestStream{updates: make(chan []protocol.SessionEvent)}, nil
}

type reconnectLookupSession struct {
	fakeSession
	lookup chan string
}

func (s *reconnectLookupSession) Stream(context.Context, string) (sessionclient.EventStream, error) {
	updates := make(chan []protocol.SessionEvent)
	close(updates)
	return abortTestStream{updates: updates}, nil
}

func (s *reconnectLookupSession) Turn(_ context.Context, id string) (protocol.TurnInfo, error) {
	select {
	case s.lookup <- id:
	default:
	}
	return protocol.TurnInfo{TurnID: id, Status: protocol.TurnStatusRunning}, nil
}

func TestReplacementRunWatcherClearsStaleRecovery(t *testing.T) {
	application, state, _ := mountRunAbort(t)
	session := &reconnectLookupSession{fakeSession: fakeSession{id: state.session.ID}, lookup: make(chan string, 2)}
	state.bound = session
	state.recovery = footerReconnectingActivity
	state.watchSession(session, state.operation, state.activeTurnID)
	select {
	case id := <-session.lookup:
		if id != "run_abort" {
			t.Fatalf("run lookup = %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("active run was not verified")
	}
	receiveAbortCompletion(t, state)()
	application.Pump(120, 36)
	if state.recovery != footerHealthy || !state.turnPending {
		t.Fatalf("recovered run: recovery=%v pending=%t", state.recovery, state.turnPending)
	}
}

type abortTestRun struct {
	appTestRun
	abort func(context.Context) error
}

func (r abortTestRun) Abort(ctx context.Context) error { return r.abort(ctx) }

type turnAbortHarness struct{ state *turnAbortTestState }

func (h turnAbortHarness) CreateState() ui.State { return h.state }

type turnAbortTestState struct {
	appState
	scroll      ui.ScrollController
	completions chan func()
	timeout     time.Duration
	cancel      context.CancelFunc
}

func (*turnAbortTestState) InitState()          {}
func (*turnAbortTestState) Dispose()            {}
func (s *turnAbortTestState) dispatch(f func()) { s.completions <- f }
func (s *turnAbortTestState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}
func (s *turnAbortTestState) Build(ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	messages := append(append([]transcriptMessage(nil), s.messages...), s.liveMessages...)
	return shellView{Snapshot: shellSnapshot{
		Phase: s.phase, Session: s.session, Messages: messages, Running: s.turnPending, AgentRunning: s.turnPending,
		TurnActivity: s.presentedTurnActivity(time.Now()), TurnThinking: s.turnThinking,
		Recovery: s.recovery, Scroll: &s.scroll, Toasts: s.toasts.Snapshot(),
		PendingInteractions: append([]protocol.InteractionRequest(nil), s.pendingInteractions...),
	}, Callbacks: shellCallbacks{
		InputOwner:             s.inputOwner,
		Dismiss:                func(ui.EventContext) { s.abortRunWithDispatch(s.dispatch, s.timeout) },
		FocusWorkspaceComposer: func(ui.EventContext) { s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusComposer) }) },
	}}
}

func mountRunAbort(t *testing.T) (*abortTestApp, *turnAbortTestState, *abortTestSession) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	session := &abortTestSession{fakeSession: fakeSession{id: "session_abort"}, streams: make(chan string, 4)}
	session.snapshot = func(context.Context) (protocol.SessionSnapshot, error) {
		// Active-turn content is intentionally omitted from bounded snapshots.
		return protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session_abort"}, ActiveTurnID: "run_abort", EventStreamID: "stream", EventCursor: 4}, nil
	}
	state := &turnAbortTestState{completions: make(chan func(), 8), timeout: time.Second, cancel: cancel, appState: appState{
		ctx: ctx, attachmentCtx: ctx, phase: phaseReady, session: protocol.SessionInfo{ID: "session_abort", Name: "Abort test", Model: "test/model"},
		bound: session, turnPending: true, activeTurnID: "run_abort", turnActivity: "Working…", liveStreamID: "stream", liveSequence: 4,
		messages:     []transcriptMessage{{ID: "old", Role: "assistant", Text: "Retained evidence"}},
		liveMessages: []transcriptMessage{{ID: "prompt", Role: "user", Text: "Current prompt"}},
	}}
	state.showToastOverride = func(input toastInput) { state.SetState(func() { state.storeToast(input) }) }
	application := newAbortTestApp(t, turnAbortHarness{state}, state.dispatch)
	application.Pump(120, 36)
	column, row := findTextCell(t, toastPainterRows(application.backend.painter), "Ask kit to do something…")
	application.Click(column, row)
	application.Pump(120, 36)
	return application, state, session
}

func assertAbortText(t *testing.T, application *abortTestApp, text string) {
	t.Helper()

	if !strings.Contains(application.Text(), text) {
		t.Fatalf("missing %q:\n%s", text, application.Text())
	}
}
func pressAbort(application *abortTestApp) {
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	application.Pump(120, 36)
	application.Pump(120, 36)
	application.Pump(120, 36)
}
func receiveAbortCompletion(t *testing.T, state *turnAbortTestState) func() {
	t.Helper()
	select {
	case f := <-state.completions:
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("abort recovery did not complete")
		return nil
	}
}
func completeAbort(t *testing.T, application *abortTestApp, state *turnAbortTestState) {
	t.Helper()
	receiveAbortCompletion(t, state)()
	application.Pump(120, 36)
	application.Pump(120, 36)
	application.Pump(120, 36)
}

func TestRunAbortFailurePreservesEvidenceAndAllowsEscapeRetry(t *testing.T) {
	for _, activeHandle := range []bool{false, true} {
		t.Run(map[bool]string{false: "attached", true: "local"}[activeHandle], func(t *testing.T) {
			application, state, session := mountRunAbort(t)
			calls := make(chan string, 4)
			session.abort = func(_ context.Context, id string) error { calls <- id; return errors.New("provider unavailable") }
			if activeHandle {
				state.activeTurn = abortTestRun{appTestRun: appTestRun{id: "run_abort"}, abort: func(ctx context.Context) error { return session.Abort(ctx, "run_abort") }}
			}
			pressAbort(application)
			assertAbortText(t, application, "Stopping…")
			pressAbort(application) // Coalesced/repeated Esc must not issue a second request.
			completeAbort(t, application, state)
			assertAbortText(t, application, "Working…")
			assertAbortText(t, application, "Abort failed")
			assertAbortText(t, application, "provider unavailable")
			assertAbortText(t, application, "Press Esc to retry")
			assertAbortText(t, application, "Retained evidence")
			assertAbortText(t, application, "Current prompt")
			if len(calls) != 1 || !state.turnPending || state.turnStopping {
				t.Fatalf("recovery: calls=%d pending=%t stopping=%t", len(calls), state.turnPending, state.turnStopping)
			}
			pressAbort(application)
			completeAbort(t, application, state)
			if len(calls) != 2 {
				t.Fatalf("retry calls = %d", len(calls))
			}
		})
	}
}

func TestRunAbortTimeoutAndUnavailableSnapshotPermitRetry(t *testing.T) {
	application, state, session := mountRunAbort(t)
	state.timeout = 5 * time.Millisecond
	session.abort = func(ctx context.Context, _ string) error { <-ctx.Done(); return ctx.Err() }
	session.snapshot = func(ctx context.Context) (protocol.SessionSnapshot, error) {
		<-ctx.Done()
		return protocol.SessionSnapshot{}, ctx.Err()
	}
	pressAbort(application)
	assertAbortText(t, application, "Stopping…")
	completeAbort(t, application, state)
	assertAbortText(t, application, "Working…")
	assertAbortText(t, application, "context deadline exceeded")
	assertAbortText(t, application, "Run status is unconfirmed")
	assertAbortText(t, application, "Retained evidence")
	assertAbortText(t, application, "Current prompt")
	if !state.turnPending || state.turnStopping {
		t.Fatalf("timeout terminalized the run: %+v", state.appState.activeTurnID)
	}
	pressAbort(application)
	completeAbort(t, application, state)
}

func TestRunAbortFailureReconcilesTerminalSnapshot(t *testing.T) {
	application, state, session := mountRunAbort(t)
	session.abort = func(context.Context, string) error { return errors.New("run already finished") }
	session.snapshot = func(context.Context) (protocol.SessionSnapshot, error) {
		return protocol.SessionSnapshot{Session: state.session, EventStreamID: "stream", EventCursor: 5, Messages: []protocol.TranscriptMessage{
			{ID: "old", Role: "assistant", Content: []protocol.TranscriptContent{protocol.TextBlock("Retained evidence")}},
			{ID: "prompt", Role: "user", Content: []protocol.TranscriptContent{protocol.TextBlock("Current prompt")}},
			{ID: "done", Role: "assistant", Content: []protocol.TranscriptContent{protocol.TextBlock("Finished response")}},
		}}, nil
	}
	pressAbort(application)
	completeAbort(t, application, state)
	assertAbortText(t, application, "Finished response")
	assertAbortText(t, application, "Retained evidence")
	assertAbortText(t, application, "Current prompt")
	if state.turnPending || state.turnStopping || state.activeTurnID != "" || len(state.toasts.Snapshot()) != 0 {
		t.Fatalf("terminal recovery: pending=%t stopping=%t id=%q toasts=%+v", state.turnPending, state.turnStopping, state.activeTurnID, state.toasts.Snapshot())
	}
}

func TestRunAbortSuccessWaitsForAuthoritativeTerminalEvent(t *testing.T) {
	application, state, session := mountRunAbort(t)
	called := make(chan struct{}, 1)
	session.abort = func(context.Context, string) error { called <- struct{}{}; return nil }
	pressAbort(application)
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("abort not called")
	}
	assertAbortText(t, application, "Stopping…")
	if !state.turnPending {
		t.Fatal("successful abort RPC prematurely settled run")
	}
	state.SetState(func() {
		state.applyTurnEvents([]protocol.SessionEvent{{StreamID: "stream", Sequence: 5, TurnID: "run_abort", Payload: protocol.TurnCompletedEvent{Status: protocol.TurnStatusAborted}}})
	})
	application.Pump(120, 36)
	if state.turnStopping {
		t.Fatal("terminal event retained stopping state")
	}
	assertAbortText(t, application, "Retained evidence")
	assertAbortText(t, application, "Current prompt")
	// The watcher follows the terminal event with the authoritative transcript.
	state.SetState(func() {
		state.applySnapshot(protocol.SessionSnapshot{Session: state.session, EventStreamID: "stream", EventCursor: 5, Messages: []protocol.TranscriptMessage{
			{ID: "old", Role: "assistant", Content: []protocol.TranscriptContent{protocol.TextBlock("Retained evidence")}},
			{ID: "prompt", Role: "user", Content: []protocol.TranscriptContent{protocol.TextBlock("Current prompt")}},
			{ID: "aborted", Role: "assistant", StopReason: "aborted", ErrorMessage: "Run aborted"},
		}})
	})
	application.Pump(120, 36)
	application.Pump(120, 36)
	assertAbortText(t, application, "Run aborted")
	assertAbortText(t, application, "Retained evidence")
	if state.turnPending || state.activeTurnID != "" {
		t.Fatal("terminal snapshot did not settle the aborted run")
	}
}

func TestRunAbortLateRecoveryDoesNotAffectNewRunOrDisposedApp(t *testing.T) {
	for _, tc := range []string{"successor", "disposed", "newer event", "newer stream"} {
		t.Run(tc, func(t *testing.T) {
			application, state, session := mountRunAbort(t)
			session.abort = func(context.Context, string) error { return errors.New("old failure") }
			if tc == "newer event" || tc == "newer stream" {
				session.snapshot = func(context.Context) (protocol.SessionSnapshot, error) {
					return protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session_abort"}, EventStreamID: "stream", EventCursor: 4}, nil
				}
			}
			pressAbort(application)
			completion := receiveAbortCompletion(t, state)
			if tc == "disposed" {
				state.cancel()
			} else if tc == "successor" {
				state.SetState(func() { state.activeTurnID = "run_next"; state.turnActivity = "New run stopping…" })
			} else {
				state.SetState(func() {
					state.liveSequence = 6
					if tc == "newer stream" {
						state.liveStreamID = "new-stream"
						state.liveSequence = 1
					}
					state.liveMessages = append(state.liveMessages, transcriptMessage{ID: "new", Role: "user", Text: "Newer evidence"})
				})
			}
			completion()
			application.Pump(120, 36)
			application.Pump(120, 36)
			application.Pump(120, 36)
			switch tc {
			case "successor":
				assertAbortText(t, application, "New run stopping…")
			case "disposed":
				assertAbortText(t, application, "Stopping…")
			case "newer event", "newer stream":
				assertAbortText(t, application, "Newer evidence")
				assertAbortText(t, application, "Working…")
			}
		})
	}
}

func TestRunAbortDuringAdmissionIsSentOnceAfterAcceptance(t *testing.T) {
	application, state, session := mountRunAbort(t)
	state.turnPending = false
	state.activeTurnID = ""
	calls := make(chan struct{}, 4)
	session.abort = func(context.Context, string) error { calls <- struct{}{}; return errors.New("abort rejected") }
	run := abortTestRun{appTestRun: appTestRun{id: "run_abort"}, abort: func(ctx context.Context) error { return session.Abort(ctx, "run_abort") }}
	release := make(chan struct{})
	state.startPromptSubmission("Current prompt", func(ctx context.Context) (sessionclient.Turn, error) {
		select {
		case <-release:
			return run, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	application.Pump(120, 36)
	pressAbort(application)
	pressAbort(application)
	assertAbortText(t, application, "Stopping…")
	if len(calls) != 0 || !state.prompt.abort.Load() {
		t.Fatal("pending admission was not deferred")
	}
	close(release)
	completeAbort(t, application, state) // Production admission dispatch adopts the handle and starts watching.
	select {
	case id := <-session.streams:
		if id != "run_abort" {
			t.Fatalf("watching %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("admitted run was not watched")
	}
	completeAbort(t, application, state) // Production abort failure dispatch restores retryability.
	assertAbortText(t, application, "Abort failed")
	assertAbortText(t, application, "Working…")
	if len(calls) != 1 || state.prompt.abort.Load() {
		t.Fatalf("admitted abort attempts=%d pending=%t", len(calls), state.prompt.abort.Load())
	}
}

func TestRunAbortRequestSurvivesDetach(t *testing.T) {
	application, state, session := mountRunAbort(t)
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	finished := make(chan struct{})
	session.abort = func(ctx context.Context, _ string) error {
		entered <- ctx
		<-release
		close(finished)
		return nil
	}
	pressAbort(application)
	var request context.Context
	select {
	case request = <-entered:
	case <-time.After(time.Second):
		t.Fatal("abort not started")
	}
	state.cancel()
	if request.Err() != nil {
		t.Fatalf("detach withdrew explicit abort: %v", request.Err())
	}
	if _, bounded := request.Deadline(); !bounded {
		t.Fatal("detached abort must remain bounded")
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("abort did not finish")
	}
}

func TestRunAbortRecoveryRestoresSnapshotCompaction(t *testing.T) {
	application, state, session := mountRunAbort(t)
	session.abort = func(context.Context, string) error { return errors.New("abort rejected") }
	session.snapshot = func(context.Context) (protocol.SessionSnapshot, error) {
		return protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session_abort"}, ActiveTurnID: "run_abort", EventStreamID: "stream", EventCursor: 5, ActiveCompaction: &protocol.ActiveCompaction{ID: "compact", TurnID: "run_abort"}}, nil
	}
	pressAbort(application)
	completeAbort(t, application, state)
	assertAbortText(t, application, "Compacting session…")
	assertAbortText(t, application, "Current prompt")
}

func TestRunAbortRecoveryAdoptsSuccessorWithoutAbortingIt(t *testing.T) {
	application, state, session := mountRunAbort(t)
	oldWatcherCanceled := false
	state.turnWatchID = "run_abort"
	state.turnWatchCancel = func() { oldWatcherCanceled = true }
	state.prompt = &promptAdmission{}
	calls := make(chan string, 4)
	session.abort = func(_ context.Context, id string) error { calls <- id; return errors.New("previous run finished") }
	session.snapshot = func(context.Context) (protocol.SessionSnapshot, error) {
		return protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session_abort"}, ActiveTurnID: "run_successor", EventStreamID: "stream", EventCursor: 5, Messages: []protocol.TranscriptMessage{
			{ID: "old", Role: "assistant", Content: []protocol.TranscriptContent{protocol.TextBlock("Retained evidence")}},
		}}, nil
	}
	pressAbort(application)
	completeAbort(t, application, state)
	select {
	case id := <-session.streams:
		if id != "run_successor" {
			t.Fatalf("watching %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("successor watcher was not started")
	}
	assertAbortText(t, application, "Working…")
	assertAbortText(t, application, "Retained evidence")
	assertAbortText(t, application, "The previous run finished")
	if state.activeTurnID != "run_successor" || state.turnStopping || !state.turnPending || state.activeTurn != nil || state.prompt != nil || !oldWatcherCanceled {
		t.Fatalf("successor state = %q, pending=%t stopping=%t", state.activeTurnID, state.turnPending, state.turnStopping)
	}
	if len(calls) != 1 || <-calls != "run_abort" {
		t.Fatal("recovery aborted the successor")
	}
}

func TestRunAbortRecoveryRestoresRetryAndFeedback(t *testing.T) {
	for _, tc := range []string{"retry", "feedback"} {
		t.Run(tc, func(t *testing.T) {
			application, state, session := mountRunAbort(t)
			session.abort = func(context.Context, string) error { return errors.New("abort rejected") }
			session.snapshot = func(context.Context) (protocol.SessionSnapshot, error) {
				snapshot := protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session_abort"}, ActiveTurnID: "run_abort", EventStreamID: "stream", EventCursor: 5}
				if tc == "retry" {
					snapshot.ProviderRetry = &protocol.ProviderRetry{Count: 2, RetryAt: "2000-01-01T00:00:00Z"}
				} else {
					snapshot.PendingInteractions = []protocol.InteractionRequest{{ID: "interaction_test", Kind: protocol.InteractionInput, Title: "Provide next step"}}
				}
				return snapshot, nil
			}
			pressAbort(application)
			completeAbort(t, application, state)
			if tc == "retry" {
				assertAbortText(t, application, "Retry 2 in 0s…")
			} else {
				assertAbortText(t, application, "Provide next step")
				if state.inputOwner() != inputInteraction || !state.agentFeedbackPending {
					t.Fatal("feedback did not acquire input ownership")
				}
			}
			assertAbortText(t, application, "Current prompt")
			assertAbortText(t, application, "Retained evidence")
		})
	}
}
