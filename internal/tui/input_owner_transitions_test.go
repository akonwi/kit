package tui

import (
	"context"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestRetainedComposerStillAcceptsMouseFocus(t *testing.T) {
	t.Parallel()

	state := newInputOwnerTransitionState()
	application := uitest.New(inputOwnerTransitionHarness{state: state})
	application.Pump(80, 24)

	if got := state.workspace.FocusOwner(); got != workspaceFocusContent {
		t.Fatalf("initial logical focus = %v, want content", got)
	}
	column, row := findTextCell(t, paintedRows(application, 80, 24), "Ask kit to do something…")
	application.Click(column+1, row)
	if got := state.workspace.FocusOwner(); got != workspaceFocusComposer {
		t.Fatalf("logical focus after composer click = %v, want composer", got)
	}
	application.Pump(80, 24)
	application.Key("x")
	application.Pump(80, 24)

	if state.composer != "x" {
		t.Fatalf("composer value after shell click = %q, want %q", state.composer, "x")
	}
}

func TestInputOwnerConfigurationEscapeCancelsContextEditFirst(t *testing.T) {
	t.Parallel()

	state := newInputOwnerTransitionState()
	state.configurationPicker = configurationPickerController{
		Mode:           configurationPickerModel,
		Selection:      "test/model",
		Models:         []protocol.ModelCapability{{ID: "test/model", ContextWindow: 128000}},
		EditingContext: true,
		EditModel:      "test/model",
		EditValue:      "64000",
	}
	application := uitest.New(inputOwnerTransitionHarness{state: state})
	application.Pump(100, 30)

	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc, Text: "\x1b", EventType: vaxis.EventPress})
	application.Pump(100, 30)

	if state.configurationPicker.Mode != configurationPickerModel {
		t.Fatalf("configuration picker mode = %v, want model picker to remain open", state.configurationPicker.Mode)
	}
	if state.configurationPicker.EditingContext || state.configurationPicker.EditModel != "" || state.configurationPicker.EditValue != "" {
		t.Fatalf("context editor remained open: %+v", state.configurationPicker.Snapshot())
	}
}

func TestInputOwnerDiscardsCoalescedPasteAfterOwnerChange(t *testing.T) {
	t.Parallel()

	state := newInputOwnerTransitionState()
	application := uitest.New(inputOwnerTransitionHarness{state: state})
	application.Pump(100, 30)

	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "discarded", Keycode: 'd', EventType: vaxis.EventPaste})
	state.openPalette()
	application.Pump(100, 30)
	application.Send(vaxis.PasteEndEvent{})
	application.Pump(100, 30)

	if state.composer != "" || state.palette.Query != "" {
		t.Fatalf("paste crossed owner transition: composer=%q palette=%q", state.composer, state.palette.Query)
	}
	if state.pastes != 0 || state.changes != 0 {
		t.Fatalf("discarded paste invoked callbacks: pastes=%d changes=%d", state.pastes, state.changes)
	}
}

func TestInputOwnerDiscardsCoalescedPasteAfterSameOwnerReopen(t *testing.T) {
	t.Parallel()

	state := newInputOwnerTransitionState()
	state.palette.OpenFor(false)
	application := uitest.New(inputOwnerTransitionHarness{state: state})
	application.Pump(100, 30)

	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "stale", Keycode: 's', EventType: vaxis.EventPaste})
	state.SetState(func() { state.palette.Close() })
	state.openPalette()
	application.Pump(100, 30)
	application.Send(vaxis.PasteEndEvent{})
	application.Pump(100, 30)

	if !state.palette.Open || state.palette.Query != "" {
		t.Fatalf("paste entered reopened palette: open=%t query=%q", state.palette.Open, state.palette.Query)
	}
}

func TestDiscardedPasteDoesNotPoisonNextComposerEdit(t *testing.T) {
	t.Parallel()

	state := newInputOwnerTransitionState()
	application := uitest.New(inputOwnerTransitionHarness{state: state})
	application.Pump(100, 30)

	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "discarded", Keycode: 'd', EventType: vaxis.EventPaste})
	state.openPalette()
	application.Pump(100, 30)
	application.Send(vaxis.PasteEndEvent{})
	state.SetState(func() { state.palette.Close() })
	application.Pump(100, 30)
	application.Send(ui.Key{Text: "x", Keycode: 'x'})
	application.Pump(100, 30)

	if state.composer != "x" {
		t.Fatalf("composer = %q, want ordinary edit %q", state.composer, "x")
	}
	if state.pastes != 0 || state.changes != 1 {
		t.Fatalf("callbacks after discarded paste: pastes=%d changes=%d, want normal change only", state.pastes, state.changes)
	}
}

func TestPendingDockUnderPaletteReceivesNeitherFocusNorPaste(t *testing.T) {
	t.Parallel()

	state := newInputOwnerTransitionState()
	state.palette.OpenFor(false)
	state.pendingInteractions = []protocol.InteractionRequest{{
		ID: "interaction_1", Kind: protocol.InteractionInput, Title: "Background input",
	}}
	application := uitest.New(inputOwnerTransitionHarness{state: state})
	application.Pump(100, 30)

	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "palette only", Keycode: 'p', EventType: vaxis.EventPaste})
	application.Send(vaxis.PasteEndEvent{})
	application.Pump(100, 30)

	if state.palette.Query != "palette only" {
		t.Fatalf("palette query = %q, want pasted text", state.palette.Query)
	}
	if state.composer != "" || state.interactionResponses != 0 {
		t.Fatalf("background owner received input: composer=%q responses=%d", state.composer, state.interactionResponses)
	}
	if got := state.inputOwner(); got != inputPalette {
		t.Fatalf("owner = %v, want palette", got)
	}
}

func TestStandaloneSessionPickerCtrlCQuitsFromChildDialogs(t *testing.T) {
	t.Parallel()

	const sessionID = "session_0123456789abcdef0123456789abcdef"
	for _, test := range []struct {
		name    string
		openKey ui.Key
	}{
		{name: "rename", openKey: ui.Key{Text: "r", Keycode: 'r', Modifiers: vaxis.ModCtrl}},
		{name: "delete", openKey: ui.Key{Text: "d", Keycode: 'd', Modifiers: vaxis.ModCtrl}},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := uitest.New(sessionPicker{
				Options:         SessionPickerOptions{Context: context.Background(), Server: &fakeServer{}},
				Result:          &sessionPickerResult{},
				initialSet:      true,
				initialSessions: []protocol.SessionInfo{{ID: sessionID, Name: "Session"}},
			})
			application.Pump(80, sessionPickerMinHeight)
			application.Send(test.openKey)
			application.Pump(80, sessionPickerMinHeight)
			application.Send(ui.Key{Text: "c", Keycode: 'c', Modifiers: vaxis.ModCtrl})

			if !application.ShouldQuit() {
				t.Fatal("Ctrl+C did not quit from child dialog")
			}
		})
	}
}

type inputOwnerTransitionHarness struct{ state *inputOwnerTransitionState }

func (w inputOwnerTransitionHarness) CreateState() ui.State { return w.state }

type inputOwnerTransitionState struct {
	appState
	scroll               ui.ScrollController
	pastes               int
	changes              int
	interactionResponses int
}

func newInputOwnerTransitionState() *inputOwnerTransitionState {
	return &inputOwnerTransitionState{appState: appState{
		phase: phaseReady,
		session: protocol.SessionInfo{
			ID: "session_test", Name: "Ownership test", Model: "test/model",
		},
	}}
}

func (*inputOwnerTransitionState) InitState() {}
func (*inputOwnerTransitionState) Dispose()   {}

func (s *inputOwnerTransitionState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}

func (s *inputOwnerTransitionState) Build(ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	return shellView{
		Snapshot: shellSnapshot{
			Phase: s.phase, Composer: s.composer, Scroll: &s.scroll, Session: s.session, Workspace: s.workspace.Snapshot(),
			PaletteOpen: s.palette.Open, PaletteQuery: s.palette.Query,
			PaletteSelection: s.palette.Selection, PaletteCommands: s.palette.Contributions,
			ConfigurationPicker: s.configurationPicker.Snapshot(),
			PendingInteractions: append([]protocol.InteractionRequest(nil), s.pendingInteractions...),
		},
		Callbacks: shellCallbacks{
			InputOwner: s.inputOwner,
			Quit:       func(ctx ui.EventContext) { ctx.Quit() },
			Dismiss:    s.dismiss,
			FocusWorkspaceComposer: func(ui.EventContext) {
				s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusComposer) })
			},
			ComposerPasted: func(_ ui.EventContext, value string) {
				s.SetState(func() {
					s.composer = value
					s.pastes++
				})
			},
			ComposerChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() {
					s.composer = value
					s.changes++
				})
			},
			PaletteQueryChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.palette.SetQuery(s.hasActiveWork(), value) })
			},
			RespondInteraction: func(_ ui.EventContext, _ protocol.InteractionResponse, complete func(error)) {
				s.interactionResponses++
				complete(nil)
			},
		},
	}
}
