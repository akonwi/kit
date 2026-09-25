package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestControlFocusRestoresExactEditorAndCursor(t *testing.T) {
	t.Parallel()
	s := &controlFocusHarnessState{first: "first", second: "second"}
	a := uitest.New(controlFocusHarness{s})
	a.Pump(60, 12)
	x, y := findTextCell(t, paintedRows(a, 60, 12), "second")
	a.Click(x+3, y)
	a.Send(ui.Key{Keycode: 'x', Text: "x"})
	a.Pump(60, 12)
	if s.second != "secxond" {
		t.Fatalf("clicked editor=%q, want secxond", s.second)
	}
	saved := s.focus
	s.SetState(func() { s.modal = true })
	a.Pump(60, 12)
	a.Key("modal")
	s.SetState(func() { s.modal = false; s.restore = saved })
	a.Pump(60, 12)
	a.Key("!")
	a.Pump(60, 12)
	if s.first != "first" || s.second != "secx!ond" {
		t.Fatalf("restored first=%q second=%q", s.first, s.second)
	}
}

func TestRemovedControlFocusCannotRestore(t *testing.T) {
	t.Parallel()
	s := &controlFocusHarnessState{first: "first", second: "second"}
	a := uitest.New(controlFocusHarness{s})
	a.Pump(60, 12)
	x, y := findTextCell(t, paintedRows(a, 60, 12), "second")
	a.Click(x, y)
	a.Key("x")
	saved := s.focus
	s.SetState(func() { s.removeSecond = true })
	a.Pump(60, 12)
	if saved == nil || saved.mounted {
		t.Fatal("removed control must invalidate its focus return address")
	}
}

type controlFocusHarness struct{ state *controlFocusHarnessState }

func (w controlFocusHarness) CreateState() ui.State { return w.state }

type controlFocusHarnessState struct {
	extra ui.Widget
	ui.StateBase
	first, second       string
	modal, removeSecond bool
	focus, restore      *controlFocusState
}

func (s *controlFocusHarnessState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() == ui.CapturePhase && !s.modal {
		ctx.Invoke(captureControlFocusIntent{Report: func(c *controlFocusState) { s.focus = c }})
	}
	return ui.EventIgnored
}
func (s *controlFocusHarnessState) Build(ui.BuildContext) ui.Widget {
	controls := []ui.Widget{messageComposer{Value: s.first, OnChanged: func(_ ui.EventContext, v string) { s.SetState(func() { s.first = v }) }}}
	if s.extra != nil {
		controls[0] = ui.SizedBox{Height: 14, Child: s.extra}
	}
	if !s.removeSecond {
		controls = append(controls, messageComposer{Value: s.second, OnChanged: func(_ ui.EventContext, v string) { s.SetState(func() { s.second = v }) }})
	}
	var entries []ui.OverlayEntry
	if s.modal {
		entries = []ui.OverlayEntry{{Modal: true, Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: messageComposer{Value: "modal"}}}}
	}
	return ui.Provider[restoreControlFocus]{Value: restoreControlFocus{Target: s.restore}, Child: ui.Overlay{Child: ui.Flex{Axis: ui.Vertical, Children: controls}, Entries: entries}}
}

func TestControlReturnKeepsOriginalSessionAndPaneAddress(t *testing.T) {
	t.Parallel()
	pane := fileWorkspacePane("workspace", "file.go")
	pane.OpenGeneration = 1
	identity, err := workspacePaneIdentityFor(pane)
	if err != nil {
		t.Fatal(err)
	}
	view := shellView{Snapshot: shellSnapshot{Session: protocol.SessionInfo{ID: "session"}, CurrentWorkspaceID: "workspace", Workspace: workspaceControllerSnapshot{Selected: identity, Panes: []workspacePaneDescriptor{pane}, FocusOwner: workspaceFocusContent}}}
	original := captureShellFocus(view)
	control := &controlFocusState{mounted: true, region: original}
	if !control.valid(view) || !original.valid(view) {
		t.Fatal("live control and return address must both be valid")
	}
	view.Snapshot.Session.ID = "replacement"
	// Reconciliation can reuse a control while projecting its new session.
	control.region.sessionID = "replacement"
	if !control.valid(view) {
		t.Fatal("reused control belongs to its newly rendered session")
	}
	if original.valid(view) {
		t.Fatal("a reused control must not revive its old-session return address")
	}
	view.Snapshot.Session.ID = "session"
	view.Snapshot.Workspace.Panes[0].OpenGeneration++
	if original.valid(view) {
		t.Fatal("a new pane incarnation must not inherit the old return address")
	}
}

func TestControlFocusRestoresInlineFileEditorWithoutMovingCursor(t *testing.T) {
	files := &fileViewerSession{results: []protocol.WorkspaceFileRead{fileViewerRead("workspace", "main.go", "revision", "one\ntwo\nthree\n")}}
	model := &filePaneHarnessModel{descriptor: fileWorkspacePane("workspace", "main.go"), workspace: "workspace", active: true, show: true, files: files}
	state := &controlFocusHarnessState{extra: filePaneHarness{model: model}, second: "composer"}
	application := uitest.New(controlFocusHarness{state})
	pumpUntil(t, application, model, 60, 18, "three")
	col, row := findTextCell(t, paintedRows(application, 60, 18), "2 │")
	application.Send(ui.Mouse{Col: col, Row: row, Button: ui.MouseLeftButton, EventType: ui.EventPress})
	application.Send(ui.Mouse{Col: col, Row: row, Button: ui.MouseLeftButton, EventType: ui.EventRelease})
	application.Pump(60, 18)
	application.Key("draft")
	application.Send(ui.Key{Keycode: vaxis.KeyLeft})
	application.Send(ui.Key{Keycode: vaxis.KeyLeft})
	application.Pump(60, 18)
	saved := state.focus
	state.SetState(func() { state.modal = true })
	application.Pump(60, 18)
	application.Key("modal")
	state.SetState(func() { state.modal = false; state.restore = saved })
	application.Pump(60, 18)
	application.Key("!")
	application.Pump(60, 18)
	if !application.Contains("dra!ft") || state.second != "composer" {
		t.Fatalf("inline editor restoration:\n%s", application.Text())
	}
}
