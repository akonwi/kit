package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestPaneInputOwnerRequiresLiveSelectedPaneIncarnation(t *testing.T) {
	t.Parallel()

	descriptor := workingTreeDiffWorkspacePane("workspace")
	descriptor.OpenGeneration = 7
	identity, err := workspacePaneIdentityFor(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	owner := paneInputOwner{
		Kind: paneInputDiffTarget, SessionID: "session", WorkspaceID: "workspace",
		Pane: identity, Generation: descriptor.OpenGeneration,
	}
	base := shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{ID: "session"}, CurrentWorkspaceID: "workspace",
		Workspace: workspaceControllerSnapshot{Panes: []workspacePaneDescriptor{descriptor}, Selected: identity, FocusOwner: workspaceFocusContent},
		PaneInput: owner,
	}
	if !owner.active(base) || base.inputOwner() != inputPane {
		t.Fatalf("live pane owner was not active: active=%t owner=%v", owner.active(base), base.inputOwner())
	}

	for _, test := range []struct {
		name   string
		mutate func(*shellSnapshot)
	}{
		{name: "phase", mutate: func(snapshot *shellSnapshot) { snapshot.Phase = phaseAuthSelect }},
		{name: "session", mutate: func(snapshot *shellSnapshot) { snapshot.Session.ID = "replacement" }},
		{name: "workspace", mutate: func(snapshot *shellSnapshot) { snapshot.CurrentWorkspaceID = "replacement" }},
		{name: "selection", mutate: func(snapshot *shellSnapshot) { snapshot.Workspace.Selected = workspaceAgentIdentity }},
		{name: "generation", mutate: func(snapshot *shellSnapshot) { snapshot.Workspace.Panes[0].OpenGeneration++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := base
			snapshot.Workspace.Panes = append([]workspacePaneDescriptor(nil), base.Workspace.Panes...)
			test.mutate(&snapshot)
			if owner.active(snapshot) || snapshot.inputOwner() == inputPane {
				t.Fatalf("stale pane owner remained active: %+v", snapshot)
			}
		})
	}
}

func TestAppReconcilesStalePaneInputOwner(t *testing.T) {
	t.Parallel()

	newState := func(t *testing.T) (*appState, workspacePaneDescriptor) {
		t.Helper()
		state := &appState{phase: phaseReady, workspaceID: "workspace", session: protocol.SessionInfo{ID: "session"}}
		if _, _, err := state.workspace.Open(workingTreeDiffWorkspacePane("workspace")); err != nil {
			t.Fatal(err)
		}
		descriptor, ok := state.workspace.SelectedPane()
		if !ok {
			t.Fatal("diff pane was not selected")
		}
		identity, err := workspacePaneIdentityFor(descriptor)
		if err != nil {
			t.Fatal(err)
		}
		state.paneInput = paneInputOwner{Kind: paneInputDiffTarget, SessionID: state.session.ID, WorkspaceID: state.workspaceID, Pane: identity, Generation: descriptor.OpenGeneration}
		state.previousInputOwner = inputPane
		return state, descriptor
	}

	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *appState, workspacePaneDescriptor)
	}{
		{name: "pane removal", mutate: func(_ *testing.T, state *appState, descriptor workspacePaneDescriptor) {
			state.workspace.Close(mustWorkspacePaneIdentity(descriptor))
		}},
		{name: "session replacement", mutate: func(_ *testing.T, state *appState, _ workspacePaneDescriptor) { state.session.ID = "replacement" }},
		{name: "pane reincarnation", mutate: func(t *testing.T, state *appState, descriptor workspacePaneDescriptor) {
			if _, _, err := state.workspace.Open(descriptor); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, descriptor := newState(t)
			test.mutate(t, state, descriptor)
			state.reconcileInputOwner()
			if state.paneInput.Kind != paneInputNone || state.inputOwner() == inputPane {
				t.Fatalf("stale pane owner survived reconciliation: %+v", state.paneInput)
			}
		})
	}
}

func mustWorkspacePaneIdentity(descriptor workspacePaneDescriptor) workspacePaneIdentity {
	identity, err := workspacePaneIdentityFor(descriptor)
	if err != nil {
		panic(err)
	}
	return identity
}

func TestPaneInputOwnerBlocksShellGlobalsAndDock(t *testing.T) {
	t.Parallel()

	descriptor := workingTreeDiffWorkspacePane("workspace")
	descriptor.OpenGeneration = 3
	identity, err := workspacePaneIdentityFor(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	owner := paneInputOwner{Kind: paneInputDiffTarget, SessionID: "session", WorkspaceID: "workspace", Pane: identity, Generation: descriptor.OpenGeneration}
	openedPalette, openedFiles, movedFocus, movedPane, responses := 0, 0, 0, 0, 0
	application := uitest.New(shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Session: protocol.SessionInfo{ID: "session", Name: "Pane ownership"}, CurrentWorkspaceID: "workspace",
			Workspace: workspaceControllerSnapshot{Panes: []workspacePaneDescriptor{descriptor}, Selected: identity, FocusOwner: workspaceFocusContent},
			PaneInput: owner, Scroll: &ui.ScrollController{},
			PendingInteractions: []protocol.InteractionRequest{{ID: "interaction", Kind: protocol.InteractionInput, Title: "Deferred"}},
		},
		Callbacks: shellCallbacks{
			InputOwner:  func() inputOwner { return inputPane },
			OpenPalette: func(ui.EventContext) { openedPalette++ }, OpenWorkspaceFilePicker: func(ui.EventContext) { openedFiles++ },
			MoveWorkspaceFocus: func(ui.EventContext) { movedFocus++ }, MoveWorkspaceSelection: func(ui.EventContext, int) { movedPane++ },
			RespondInteraction: func(ui.EventContext, protocol.InteractionResponse, func(error)) { responses++ },
		},
	})
	application.Pump(100, 30)

	for _, key := range []ui.Key{
		{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl},
		{Text: "o", Keycode: 'o', Modifiers: vaxis.ModCtrl},
		{Keycode: vaxis.KeyTab},
		{Keycode: vaxis.KeyTab, Modifiers: vaxis.ModShift},
		{Text: "]", Keycode: ']', Modifiers: vaxis.ModCtrl},
	} {
		application.Send(key)
	}
	application.Key("answer")
	application.Enter()

	if openedPalette != 0 || openedFiles != 0 || movedFocus != 0 || movedPane != 0 || responses != 0 {
		t.Fatalf("pane owner leaked input: palette=%d files=%d focus=%d pane=%d responses=%d", openedPalette, openedFiles, movedFocus, movedPane, responses)
	}
}

func TestInteractionDockRejectsPaneTargetPickerAdmission(t *testing.T) {
	t.Parallel()

	state := newPaneInputHarnessState(t)
	state.pendingInteractions = []protocol.InteractionRequest{{ID: "interaction", Kind: protocol.InteractionConfirm, Title: "Continue?"}}
	application := uitest.New(paneInputHarness{state: state})
	application.Pump(100, 28)
	column, row := findTextCell(t, paintedRows(application, 100, 28), "Working tree")
	application.Click(column, row)
	application.Pump(100, 28)

	if state.paneInput.Kind != paneInputNone || state.inputOwner() != inputInteraction || application.Contains("Select diff target") {
		t.Fatalf("dock admitted pane target picker: pane=%+v owner=%v\n%s", state.paneInput, state.inputOwner(), application.Text())
	}
}

func TestWorkspaceDiffTargetPickerOwnsShellInput(t *testing.T) {
	t.Parallel()

	state := newPaneInputHarnessState(t)
	application := uitest.New(paneInputHarness{state: state})
	application.Pump(100, 28)
	column, row := findTextCell(t, paintedRows(application, 100, 28), "Working tree")
	application.Click(column, row)
	application.Pump(100, 28)
	if got := state.inputOwner(); got != inputPane {
		t.Fatalf("owner after opening target picker = %v, want pane; changes=%d stored=%+v workspace=%+v\n%s", got, state.paneChanges, state.paneInput, state.workspace.Snapshot(), application.Text())
	}
	composerColumn, composerRow := findTextCell(t, paintedRows(application, 100, 28), "Ask kit to do something…")
	application.Click(composerColumn+1, composerRow)
	if got := state.workspace.FocusOwner(); got != workspaceFocusContent {
		t.Fatalf("pane-local modal allowed composer pointer focus: %v", got)
	}

	application.Key("feature")
	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "paste", Keycode: 'p', EventType: vaxis.EventPaste})
	application.Send(vaxis.PasteEndEvent{})
	application.Pump(100, 28)
	if !application.Contains("featurepaste") {
		t.Fatalf("target query did not retain keyboard and paste ownership:\n%s", application.Text())
	}
	application.Send(ui.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	if state.paletteOpens != 0 {
		t.Fatalf("pane owner opened palette %d times", state.paletteOpens)
	}

	state.SetState(func() {
		state.pendingInteractions = []protocol.InteractionRequest{{ID: "deferred", Kind: protocol.InteractionInput, Title: "Deferred input"}}
	})
	application.Pump(100, 28)
	application.Key("x")
	application.Pump(100, 28)
	if !application.Contains("featurepastex") || state.interactionResponses != 0 {
		t.Fatalf("dock displaced pane owner: responses=%d\n%s", state.interactionResponses, application.Text())
	}
	application.Tab()
	if state.workspaceFocusMoves != 0 {
		t.Fatalf("pane-owner Tab moved workspace focus %d times", state.workspaceFocusMoves)
	}

	application.Send(ui.Key{Keycode: vaxis.KeyEsc})
	application.Pump(100, 28)
	if state.paneInput.Kind != paneInputNone || state.inputOwner() != inputInteraction {
		t.Fatalf("dismissed picker ownership = pane:%+v owner:%v", state.paneInput, state.inputOwner())
	}
}

func TestWorkspaceDiffTargetPickerClosesWhenPaneHides(t *testing.T) {
	t.Parallel()

	state := &workspaceDiffPaneState{}
	model := &diffPanePresentationModel{active: true, pane: workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), testState: state,
		OnInputOwnerChanged: func(paneInputKind, bool) bool { return true },
	}}
	application := uitest.New(diffPanePresentationHarness{model: model})
	application.Pump(100, 24)
	application.Send(ui.Key{Text: "G", Keycode: 'g', Modifiers: vaxis.ModShift})
	application.Pump(100, 24)
	if !state.targetPickerOpen {
		t.Fatal("target picker did not open")
	}

	model.setActive(false)
	application.Pump(100, 24)
	if state.targetPickerOpen || state.targetQuery != "" {
		t.Fatalf("hidden pane retained target picker: open=%t query=%q", state.targetPickerOpen, state.targetQuery)
	}
}

func TestWorkspaceDiffTargetPickerPublishesPaneOwnership(t *testing.T) {
	t.Parallel()

	state := &workspaceDiffPaneState{}
	changes := []bool{}
	application := uitest.New(workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), testState: state,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true},
		OnInputOwnerChanged: func(kind paneInputKind, active bool) bool {
			if kind != paneInputDiffTarget {
				t.Fatalf("pane input kind = %v, want diff target", kind)
			}
			changes = append(changes, active)
			return true
		},
	})
	application.Pump(100, 24)
	application.Send(ui.Key{Text: "G", Keycode: 'g', Modifiers: vaxis.ModShift})
	application.Pump(100, 24)
	if !state.targetPickerOpen || len(changes) != 1 || !changes[0] {
		t.Fatalf("open ownership = picker:%t changes:%v", state.targetPickerOpen, changes)
	}

	application.Send(ui.Key{Keycode: vaxis.KeyEsc})
	application.Pump(100, 24)
	if state.targetPickerOpen || len(changes) != 2 || changes[1] {
		t.Fatalf("close ownership = picker:%t changes:%v", state.targetPickerOpen, changes)
	}
}

type paneInputHarness struct{ state *paneInputHarnessState }

func (w paneInputHarness) CreateState() ui.State { return w.state }

type paneInputHarnessState struct {
	appState
	scroll               ui.ScrollController
	paletteOpens         int
	workspaceFocusMoves  int
	interactionResponses int
	paneChanges          int
}

func newPaneInputHarnessState(t *testing.T) *paneInputHarnessState {
	t.Helper()
	state := &paneInputHarnessState{appState: appState{
		phase: phaseReady, workspaceID: "workspace",
		session: protocol.SessionInfo{ID: "session", Name: "Pane input", Model: "test/model"},
	}}
	if _, _, err := state.workspace.Open(workingTreeDiffWorkspacePane("workspace")); err != nil {
		t.Fatal(err)
	}
	return state
}

func (*paneInputHarnessState) InitState() {}
func (*paneInputHarnessState) Dispose()   {}
func (s *paneInputHarnessState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}
func (s *paneInputHarnessState) Build(ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	return shellView{
		Snapshot: shellSnapshot{
			Phase: s.phase, Session: s.session, CurrentWorkspaceID: s.workspaceID,
			Workspace: s.workspace.Snapshot(), PaneInput: s.paneInput, Scroll: &s.scroll,
			PendingInteractions: append([]protocol.InteractionRequest(nil), s.pendingInteractions...),
		},
		Callbacks: shellCallbacks{
			InputOwner: s.inputOwner, PaneInputChanged: func(descriptor workspacePaneDescriptor, kind paneInputKind, active bool) bool {
				s.paneChanges++
				return s.setPaneInputOwner(descriptor, kind, active)
			},
			OpenPalette:           func(ui.EventContext) { s.paletteOpens++ },
			MoveWorkspaceFocus:    func(ui.EventContext) { s.workspaceFocusMoves++ },
			FocusWorkspaceContent: func(ui.EventContext) {},
			FocusWorkspaceComposer: func(ui.EventContext) {
				s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusComposer) })
			},
			Dismiss: s.dismiss,
			RespondInteraction: func(_ ui.EventContext, _ protocol.InteractionResponse, done func(error)) {
				s.interactionResponses++
				done(nil)
			},
		},
	}
}
