package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func testScratchpad(revision protocol.ScratchpadRevision, content string) protocol.Scratchpad {
	return protocol.Scratchpad{OwnerSessionID: "session_owner", Revision: revision, Content: content, UpdatedAt: "2025-01-01T00:00:00Z"}
}

func TestScratchpadWorkspaceIsSingleton(t *testing.T) {
	t.Parallel()
	var controller workspaceController
	first, opened, err := controller.Open(scratchpadWorkspacePane())
	if err != nil || !opened || first != "scratchpad" {
		t.Fatalf("first open = %q, %t, %v", first, opened, err)
	}
	second, opened, err := controller.Open(scratchpadWorkspacePane())
	if err != nil || opened || second != first || len(controller.Panes()) != 1 {
		t.Fatalf("second open = %q, %t, %v panes=%d", second, opened, err, len(controller.Panes()))
	}
}

func TestScratchpadEditorReconcilesCleanAndConflictingUpdates(t *testing.T) {
	t.Parallel()
	initial := testScratchpad(1, "shared one")
	var editor scratchpadEditorState
	editor.reset(&initial)

	clean := testScratchpad(2, "shared two")
	if !editor.reconcile(clean) || editor.Draft != clean.Content || editor.State != scratchpadSaved {
		t.Fatalf("clean reconcile = %+v", editor)
	}

	editor.edit("my draft")
	remote := testScratchpad(3, "shared three")
	if !editor.reconcile(remote) || editor.Draft != "my draft" || editor.State != scratchpadConflict || editor.Conflict == nil || *editor.Conflict != remote {
		t.Fatalf("dirty reconcile = %+v", editor)
	}
	if editor.reconcile(testScratchpad(2, "stale")) || editor.Draft != "my draft" || editor.Authoritative != remote {
		t.Fatalf("stale record regressed editor = %+v", editor)
	}
}

func TestScratchpadMatchingRemoteDraftBecomesClean(t *testing.T) {
	t.Parallel()
	initial := testScratchpad(4, "before")
	var editor scratchpadEditorState
	editor.reset(&initial)
	editor.edit("mine")
	matching := testScratchpad(5, "mine")
	editor.reconcile(matching)
	if editor.State != scratchpadSaved || editor.Draft != "mine" || editor.Authoritative != matching || editor.Conflict != nil {
		t.Fatalf("matching reconcile = %+v", editor)
	}
}

func TestScratchpadConflictActionsPreserveOrAdoptDraft(t *testing.T) {
	t.Parallel()
	initial := testScratchpad(1, "shared")
	remote := testScratchpad(2, "new shared")
	var editor scratchpadEditorState
	editor.reset(&initial)
	editor.edit("mine")
	editor.reconcile(remote)
	editor.Review = true

	editor.Review = false // Keep editing.
	if editor.State != scratchpadConflict || editor.Draft != "mine" || editor.Conflict == nil {
		t.Fatalf("keep editing resolved conflict: %+v", editor)
	}
	editor.useShared()
	if editor.State != scratchpadSaved || editor.Draft != remote.Content || editor.Authoritative != remote || editor.Conflict != nil || editor.Review {
		t.Fatalf("use shared = %+v", editor)
	}
}

type scratchpadPaneRebuildHarness struct {
	state **scratchpadPaneRebuildHarnessState
}

func (w scratchpadPaneRebuildHarness) CreateState() ui.State {
	state := &scratchpadPaneRebuildHarnessState{}
	*w.state = state
	return state
}

type scratchpadPaneRebuildHarnessState struct {
	ui.StateBase
	editor scratchpadEditorState
}

func (s *scratchpadPaneRebuildHarnessState) Build(ui.BuildContext) ui.Widget {
	return workspaceScratchpadPane{Presentation: workspacePanePresentation{Active: true, Focused: true}, Editor: s.editor}
}

func TestScratchpadPaneRebuildsFromLoadingToEditor(t *testing.T) {
	t.Parallel()
	var state *scratchpadPaneRebuildHarnessState
	application := uitest.New(ui.Provider[ui.Theme]{Value: ui.DefaultTheme(), Child: scratchpadPaneRebuildHarness{state: &state}})
	application.Pump(60, 8)
	if !application.Contains("Loading scratchpad…") {
		t.Fatal("loading scratchpad presentation was not painted")
	}
	record := testScratchpad(1, "working notes")
	state.SetState(func() { state.editor.reset(&record) })
	application.Pump(60, 8)
	if !application.Contains("working notes") {
		t.Fatal("scratchpad editor was not painted after loading")
	}
}

func TestScratchpadCleanFooterKeepsExpectedStateQuiet(t *testing.T) {
	t.Parallel()
	record := testScratchpad(1, "working notes")
	editor := scratchpadEditorState{}
	editor.reset(&record)
	application := uitest.New(ui.Provider[ui.Theme]{Value: ui.DefaultTheme(), Child: workspaceScratchpadPane{Editor: editor}})
	application.Pump(60, 7)
	if !application.Contains("tab composer · shift+tab scratchpad") {
		t.Fatal("clean scratchpad footer did not retain focus guidance")
	}
	if application.Contains("Saved") {
		t.Fatal("clean scratchpad footer rendered redundant Saved state")
	}
}

func TestScratchpadPanePresentsEditorAndConflictReview(t *testing.T) {
	t.Parallel()
	theme := ui.DefaultTheme()
	initial := testScratchpad(1, "shared line")
	editor := scratchpadEditorState{}
	editor.reset(&initial)
	editor.edit("mine line")
	editor.reconcile(testScratchpad(2, "new shared line"))

	normal := uitest.New(ui.Provider[ui.Theme]{Value: theme, Child: workspaceScratchpadPane{
		Presentation: workspacePanePresentation{Active: true, Focused: true}, Editor: editor,
	}})
	normal.Pump(72, 9)
	visible := strings.Join(paintedRows(normal, 72, 9), "\n")
	for _, want := range []string{"Shared scratchpad changed. Autosave is paused.", "Review changes", "mine line", "Conflict"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("editor missing %q:\n%s", want, visible)
		}
	}

	editor.Review = true
	review := uitest.New(ui.Provider[ui.Theme]{Value: theme, Child: workspaceScratchpadPane{Editor: editor}})
	review.Pump(80, 12)
	visible = strings.Join(paintedRows(review, 80, 12), "\n")
	for _, want := range []string{"--- shared", "+++ mine", "- new shared line", "+ mine line", "Keep editing", "Use shared", "Replace shared with mine"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("review missing %q:\n%s", want, visible)
		}
	}
}

type scratchpadTestSession struct {
	fakeSession
	mu      sync.Mutex
	updates []protocol.UpdateScratchpadInput
	result  protocol.Scratchpad
	err     error
	started chan struct{}
	release <-chan struct{}
	handle  func(protocol.UpdateScratchpadInput) (protocol.Scratchpad, error)
}

func (s *scratchpadTestSession) Scratchpad(context.Context) (protocol.Scratchpad, error) {
	return s.result, s.err
}

func (s *scratchpadTestSession) UpdateScratchpad(_ context.Context, input protocol.UpdateScratchpadInput) (protocol.Scratchpad, error) {
	s.mu.Lock()
	s.updates = append(s.updates, input)
	result, err, handle := s.result, s.err, s.handle
	started, release := s.started, s.release
	s.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	if handle != nil {
		return handle(input)
	}
	return result, err
}

func (s *scratchpadTestSession) inputs() []protocol.UpdateScratchpadInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.UpdateScratchpadInput(nil), s.updates...)
}

type scratchpadAutosaveHarness struct {
	state   **scratchpadAutosaveHarnessState
	session *scratchpadTestSession
	initial protocol.Scratchpad
}

func (w scratchpadAutosaveHarness) CreateState() ui.State {
	state := &scratchpadAutosaveHarnessState{service: w.session, initial: w.initial}
	*w.state = state
	return state
}

type scratchpadAutosaveHarnessState struct {
	appState
	service  *scratchpadTestSession
	initial  protocol.Scratchpad
	dispatch chan func()
}

func (s *scratchpadAutosaveHarnessState) InitState() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.attachmentCtx, s.attachmentCancel = context.WithCancel(s.ctx)
	s.bound = s.service
	s.appState.session.ID = "session_bound"
	s.dispatch = make(chan func(), 8)
	s.scratchpadDispatch = func(fn func()) { s.dispatch <- fn }
	s.scratchpad.reset(&s.initial)
}

func (s *scratchpadAutosaveHarnessState) Build(ui.BuildContext) ui.Widget { return ui.SizedBox{} }

func (s *scratchpadAutosaveHarnessState) flush() {
	for {
		select {
		case fn := <-s.dispatch:
			fn()
		default:
			return
		}
	}
}

func (s *scratchpadAutosaveHarnessState) Dispose() {
	s.cancelScratchpadDebounce()
	if s.attachmentCancel != nil {
		s.attachmentCancel()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func TestScratchpadAutosaveDebouncesAndUsesRevisionGuard(t *testing.T) {
	initial := testScratchpad(7, "before")
	committed := testScratchpad(8, "second draft")
	session := &scratchpadTestSession{fakeSession: fakeSession{id: "session_bound"}, result: committed}
	var state *scratchpadAutosaveHarnessState
	application := uitest.New(scratchpadAutosaveHarness{state: &state, session: session, initial: initial})
	application.Pump(10, 2)

	state.changeScratchpad("first draft")
	time.Sleep(100 * time.Millisecond)
	state.changeScratchpad("second draft")
	time.Sleep(300 * time.Millisecond)
	state.flush()
	deadline := time.Now().Add(time.Second)
	for len(session.inputs()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	for len(state.dispatch) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	state.flush()
	inputs := session.inputs()
	if len(inputs) != 1 || inputs[0].ExpectedRevision != 7 || inputs[0].Content != "second draft" {
		t.Fatalf("autosave inputs = %+v", inputs)
	}
	application.Pump(10, 2)
	if state.scratchpad.State != scratchpadSaved || state.scratchpad.Authoritative != committed {
		t.Fatalf("committed editor = %+v", state.scratchpad)
	}
}

func TestScratchpadTypingDuringSaveQueuesNextAutosaveWithoutConflict(t *testing.T) {
	initial := testScratchpad(10, "before")
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	session := &scratchpadTestSession{
		fakeSession: fakeSession{id: "session_bound"}, started: started, release: release,
		handle: func(input protocol.UpdateScratchpadInput) (protocol.Scratchpad, error) {
			return testScratchpad(input.ExpectedRevision+1, input.Content), nil
		},
	}
	var state *scratchpadAutosaveHarnessState
	application := uitest.New(scratchpadAutosaveHarness{state: &state, session: session, initial: initial})
	application.Pump(10, 2)
	state.changeScratchpad("first")
	state.cancelScratchpadDebounce()
	state.saveScratchpad(false, nil)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first save did not start")
	}
	state.changeScratchpad("second")
	close(release)
	deadline := time.Now().Add(time.Second)
	for len(state.dispatch) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	state.flush()
	if state.scratchpad.State == scratchpadConflict {
		t.Fatalf("own intermediate save created conflict: %+v", state.scratchpad)
	}
	time.Sleep(300 * time.Millisecond)
	state.flush()
	deadline = time.Now().Add(time.Second)
	for len(session.inputs()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	for len(state.dispatch) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	state.flush()
	inputs := session.inputs()
	if len(inputs) != 2 || inputs[0].Content != "first" || inputs[1].Content != "second" || inputs[1].ExpectedRevision != 11 {
		t.Fatalf("serialized autosaves = %+v", inputs)
	}
}

func TestScratchpadCommittedEventWinsOverLostSaveResponse(t *testing.T) {
	initial := testScratchpad(20, "before")
	committed := testScratchpad(21, "mine")
	var state *scratchpadAutosaveHarnessState
	session := &scratchpadTestSession{fakeSession: fakeSession{id: "session_bound"}}
	session.handle = func(protocol.UpdateScratchpadInput) (protocol.Scratchpad, error) {
		state.scratchpadDispatch(func() { state.reconcileScratchpad(&committed) })
		return protocol.Scratchpad{}, context.DeadlineExceeded
	}
	application := uitest.New(scratchpadAutosaveHarness{state: &state, session: session, initial: initial})
	application.Pump(10, 2)
	state.scratchpad.edit("mine")
	state.saveScratchpad(false, nil)
	deadline := time.Now().Add(time.Second)
	for len(state.dispatch) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	state.flush()
	if state.scratchpad.State != scratchpadSaved || state.scratchpad.Authoritative != committed || state.scratchpad.Failure != "" {
		t.Fatalf("lost response overrode committed event: %+v", state.scratchpad)
	}
}

func TestScratchpadConflictWithSubmittedContentIsSatisfied(t *testing.T) {
	initial := testScratchpad(30, "before")
	committed := testScratchpad(31, "mine")
	session := &scratchpadTestSession{
		fakeSession: fakeSession{id: "session_bound"},
		err:         &protocol.ScratchpadError{Code: protocol.ScratchpadRevisionConflict, Message: "scratchpad changed", Current: &committed},
	}
	var state *scratchpadAutosaveHarnessState
	application := uitest.New(scratchpadAutosaveHarness{state: &state, session: session, initial: initial})
	application.Pump(10, 2)
	state.scratchpad.edit("mine")
	state.saveScratchpad(false, nil)
	deadline := time.Now().Add(time.Second)
	for len(state.dispatch) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	state.flush()
	if state.scratchpad.State != scratchpadSaved || state.scratchpad.Authoritative != committed || state.scratchpad.Conflict != nil {
		t.Fatalf("matching conflict was not satisfied: %+v", state.scratchpad)
	}
}

func TestScratchpadGuardedReplaceRefreshesAdvancedConflict(t *testing.T) {
	initial := testScratchpad(1, "before")
	advanced := testScratchpad(3, "advanced shared")
	session := &scratchpadTestSession{
		fakeSession: fakeSession{id: "session_bound"},
		err:         &protocol.ScratchpadError{Code: protocol.ScratchpadRevisionConflict, Message: "scratchpad changed", Current: &advanced},
	}
	var state *scratchpadAutosaveHarnessState
	application := uitest.New(scratchpadAutosaveHarness{state: &state, session: session, initial: initial})
	application.Pump(10, 2)
	state.scratchpad.edit("mine")
	shared := testScratchpad(2, "shared")
	state.scratchpad.reconcile(shared)
	state.scratchpad.Review = true
	state.replaceSharedScratchpad()
	if state.scratchpad.State != scratchpadSaving || state.scratchpad.Review {
		t.Fatalf("replacement did not enter non-interactive saving state: %+v", state.scratchpad)
	}
	deadline := time.Now().Add(time.Second)
	for len(session.inputs()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	for len(state.dispatch) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	state.flush()
	inputs := session.inputs()
	if len(inputs) != 1 || inputs[0].ExpectedRevision != shared.Revision || inputs[0].Content != "mine" {
		t.Fatalf("guarded replacement = %+v", inputs)
	}
	if state.scratchpad.State != scratchpadConflict || !state.scratchpad.Review || state.scratchpad.Draft != "mine" || state.scratchpad.Conflict == nil || *state.scratchpad.Conflict != advanced {
		t.Fatalf("refreshed replacement conflict = %+v", state.scratchpad)
	}
}

func TestScratchpadDraftCacheDoesNotResurrectResolvedDraft(t *testing.T) {
	initial := testScratchpad(1, "before")
	shared := testScratchpad(2, "shared")
	state := appState{scratchpadDrafts: make(map[string]scratchpadEditorState)}
	state.scratchpad.reset(&initial)
	state.scratchpad.edit("mine")
	state.scratchpad.reconcile(shared)
	state.cacheScratchpadDraft("session_a")
	state.restoreScratchpadDraft("session_a", &shared)
	if state.scratchpad.State != scratchpadConflict {
		t.Fatalf("restored state = %+v", state.scratchpad)
	}
	state.scratchpad.useShared()
	state.cacheScratchpadDraft("session_a")
	state.restoreScratchpadDraft("session_a", &shared)
	if state.scratchpad.State != scratchpadSaved || state.scratchpad.Draft != shared.Content || len(state.scratchpadDrafts) != 0 {
		t.Fatalf("resolved draft resurrected: editor=%+v cache=%+v", state.scratchpad, state.scratchpadDrafts)
	}
}

func TestScratchpadBestEffortWaitsForPendingWriteBeforeFollowUp(t *testing.T) {
	initial := testScratchpad(40, "before")
	intermediate := testScratchpad(41, "first")
	final := testScratchpad(42, "second")
	for _, test := range []struct {
		name          string
		outcome       scratchpadWriteOutcome
		authoritative protocol.Scratchpad
	}{
		{name: "successful response", outcome: scratchpadWriteOutcome{record: intermediate}, authoritative: initial},
		{name: "lost response after event", outcome: scratchpadWriteOutcome{err: context.DeadlineExceeded}, authoritative: intermediate},
		{name: "matching conflict", outcome: scratchpadWriteOutcome{err: &protocol.ScratchpadError{Code: protocol.ScratchpadRevisionConflict, Message: "changed", Current: &intermediate}}, authoritative: initial},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &scratchpadTestSession{fakeSession: fakeSession{id: "session_bound"}, result: final}
			done := make(chan scratchpadWriteOutcome, 1)
			done <- test.outcome
			state := appState{bound: session, scratchpadWritePending: true, scratchpadWriteContent: "first", scratchpadWriteDone: done}
			state.scratchpad.reset(&test.authoritative)
			state.scratchpad.Draft = "second"
			state.bestEffortSaveScratchpad(time.Second)
			inputs := session.inputs()
			if len(inputs) != 1 || inputs[0].ExpectedRevision != intermediate.Revision || inputs[0].Content != "second" {
				t.Fatalf("best-effort follow-up = %+v", inputs)
			}
		})
	}
}

func TestScratchpadCommandIsCapabilityContribution(t *testing.T) {
	t.Parallel()
	if paletteCommandExists(paletteCommandScratchpad) {
		t.Fatal("scratchpad command is present without the persistent-session capability contribution")
	}
	if !paletteCommandExists(paletteCommandScratchpad, []paletteCommand{scratchpadPaletteCommand()}) {
		t.Fatal("scratchpad command is absent with the persistent-session capability contribution")
	}
}

func TestScratchpadMetadataEventPreservesDirtyDraft(t *testing.T) {
	t.Parallel()
	initial := testScratchpad(1, "before")
	state := appState{session: protocol.SessionInfo{ID: "session_bound"}, metadataStreamID: "stream", metadataSequence: 1}
	state.scratchpad.reset(&initial)
	state.scratchpad.edit("mine")
	remote := testScratchpad(2, "theirs")
	state.applySessionMetadataEvents([]protocol.SessionEvent{{
		Kind: protocol.SessionEventScratchpadChanged, SessionID: "session_bound", StreamID: "stream", Sequence: 2, Scratchpad: &remote,
	}})
	if state.scratchpad.State != scratchpadConflict || state.scratchpad.Draft != "mine" || state.scratchpad.Conflict == nil || *state.scratchpad.Conflict != remote {
		t.Fatalf("event reconcile = %+v", state.scratchpad)
	}
}
