package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

// Mount the production shell and registry, rather than a stand-in retained pane.
type fileRetentionShell struct{ state *fileRetentionShellState }

func (w fileRetentionShell) CreateState() ui.State { return w.state }

type fileRetentionShellState struct {
	ui.StateBase
	workspace workspaceController
	files     *fileViewerSession
	dispatch  toolNavigationDispatch
	scroll    ui.ScrollController
	composer  string
}

func (s *fileRetentionShellState) Build(ui.BuildContext) ui.Widget {
	return shellView{
		WorkspaceFiles: s.files, WorkspaceDispatch: s.dispatch.dispatch,
		Snapshot: shellSnapshot{Phase: phaseReady, Session: protocol.SessionInfo{ID: "session_1", Name: "Retention", Model: "test/model"}, CurrentWorkspaceID: "workspace_a", Workspace: s.workspace.Snapshot(), Scroll: &s.scroll, Composer: s.composer},
		Callbacks: shellCallbacks{
			SelectWorkspacePane: func(_ ui.EventContext, pane workspacePaneDescriptor) {
				s.SetState(func() { id, _ := workspacePaneIdentityFor(pane); s.workspace.Select(id) })
			},
			ShowTranscript:         func(ui.EventContext) { s.SetState(s.workspace.SelectAgent) },
			FocusWorkspaceContent:  func(ui.EventContext) { s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusContent) }) },
			FocusWorkspaceComposer: func(ui.EventContext) { s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusComposer) }) },
			MoveWorkspaceFocus:     func(ui.EventContext) { s.SetState(func() { s.workspace.MoveFocus() }) },
			ComposerChanged:        func(_ ui.EventContext, text string) { s.SetState(func() { s.composer = text }) },
		},
	}
}
func (s *fileRetentionShellState) pump(app *uitest.App, width, height int) {
	s.dispatch.flush()
	for range 3 {
		app.Pump(width, height)
	}
}
func (s *fileRetentionShellState) until(t *testing.T, app *uitest.App, width, height int, text string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.pump(app, width, height)
		if app.Contains(text) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waiting for %q:\n%s", text, strings.Join(paintedRows(app, width, height), "\n"))
}
func clickFileRetentionText(t *testing.T, app *uitest.App, width, height int, text string) {
	t.Helper()
	x, y := findTextCell(t, paintedRows(app, width, height), text)
	app.Click(x, y)
	app.Send(vaxis.Mouse{Col: x, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})
}
func assertFileRetentionRange(t *testing.T, app *uitest.App, width, height, start, end int) {
	t.Helper()
	for line := start; line <= end; line++ {
		x, y := findTextCell(t, paintedRows(app, width, height), fmt.Sprintf("%02d payload", line))
		if got := app.Cell(x, y).Style.Background; got != ui.DefaultTheme().SurfaceHovered {
			t.Fatalf("line %d selection background = %v, want %v", line, got, ui.DefaultTheme().SurfaceHovered)
		}
	}
}
func TestFileShellRetainsPositionSelectionAndFocusAcrossTabsAndResize(t *testing.T) {
	var lines []string
	for i := 1; i <= 80; i++ {
		lines = append(lines, fmt.Sprintf("%02d payload %s END", i, strings.Repeat("x", 150)))
	}
	files := &fileViewerSession{results: []protocol.WorkspaceFileRead{
		fileViewerRead("workspace_a", "first.txt", "file_first", strings.Join(lines, "\n")),
		fileViewerRead("workspace_a", "second.txt", "file_second", "second evidence\nsecond tail"),
	}}
	state := &fileRetentionShellState{files: files}
	first := fileWorkspacePane("workspace_a", "first.txt")
	if _, _, err := state.workspace.Open(first); err != nil {
		t.Fatal(err)
	}
	app := uitest.New(fileRetentionShell{state})
	state.until(t, app, 100, 24, "01 payload")
	for range 29 {
		app.Key("j")
		state.pump(app, 100, 24)
	}
	app.Key("v")
	for range 2 {
		app.Key("j")
		state.pump(app, 100, 24)
	}
	if !app.Contains("Ln 32 · 80 lines") {
		t.Fatalf("cursor: %s", app.Text())
	}
	assertFileRetentionRange(t, app, 100, 24, 30, 32)
	before := paintedRows(app, 100, 24)
	var openErr error
	state.SetState(func() { _, _, openErr = state.workspace.Open(fileWorkspacePane("workspace_a", "second.txt")) })
	if openErr != nil {
		t.Fatal(openErr)
	}
	state.until(t, app, 100, 24, "second evidence")
	// The other pane receives keyboard input; the retained first pane stays put.
	app.Key("j")
	state.pump(app, 100, 24)
	if !app.Contains("Ln 2 · 2 lines") {
		t.Fatalf("second pane focus: %s", app.Text())
	}
	state.pump(app, 48, 18)
	state.pump(app, 120, 28)
	clickFileRetentionText(t, app, 120, 28, "first.txt")
	state.pump(app, 100, 24)
	// Exact body rows prove scroll position as well as content survived hidden resizing.
	after := paintedRows(app, 100, 24)
	if !reflect.DeepEqual(before[4:20], after[4:20]) {
		t.Fatalf("restored body:\nbefore=%q\nafter=%q", before[4:20], after[4:20])
	}
	assertFileRetentionRange(t, app, 100, 24, 30, 32)
	state.pump(app, 48, 18)
	// A shorter viewport keeps the same top line, even when the cursor is
	// below its bottom edge. Widening restores the selected range in view.
	for index, line := range []int{23, 24, 25, 26} {
		x, y := findTextCell(t, paintedRows(app, 48, 18), fmt.Sprintf("%d │ %02d payload", line, line))
		if x != 0 || y != 6+index {
			t.Fatalf("narrow line %d at (%d,%d), want (0,%d)", line, x, y, 6+index)
		}
	}
	state.pump(app, 100, 24)
	assertFileRetentionRange(t, app, 100, 24, 30, 32)
	app.Key("k")
	state.pump(app, 100, 24)
	if !app.Contains("Ln 31 · 80 lines") {
		t.Fatalf("restored pane focus: %s", app.Text())
	}
	assertFileRetentionRange(t, app, 100, 24, 30, 31)
	// Repeated anchored opens change navigation, not identity or tab ordering.
	order := state.workspace.Panes()
	for range 2 {
		clickFileRetentionText(t, app, 100, 24, "second.txt")
		state.pump(app, 100, 24)
		if !app.Contains("Ln 2 · 2 lines") {
			t.Fatalf("second pane position before anchored reopen: %s", app.Text())
		}
		first.RevealStartLine, first.RevealEndLine = 50, 52
		state.SetState(func() { _, _, openErr = state.workspace.Open(first) })
		if openErr != nil {
			t.Fatal(openErr)
		}
		state.pump(app, 100, 24)
		assertFileRetentionRange(t, app, 100, 24, 50, 50)
		if !app.Contains("Ln 50 · 80 lines") {
			t.Fatalf("anchored cursor: %s", app.Text())
		}
		state.pump(app, 100, 30)
		assertFileRetentionRange(t, app, 100, 30, 50, 52)
		state.pump(app, 100, 24)
		firstX, firstY := findTextCell(t, paintedRows(app, 100, 24), "first.txt")
		secondX, secondY := findTextCell(t, paintedRows(app, 100, 24), "second.txt")
		if firstY != 2 || secondY != 2 || firstX >= secondX {
			t.Fatalf("visible tab order: first=(%d,%d) second=(%d,%d)", firstX, firstY, secondX, secondY)
		}
		panes := state.workspace.Panes()
		if len(panes) != 2 || panes[0].Path != order[0].Path || panes[1].Path != order[1].Path {
			t.Fatalf("tab order: %+v", panes)
		}
	}
	app.Key("l")
	app.Key("l")
	state.pump(app, 100, 24)
	horizontal := paintedRows(app, 100, 24)
	if !strings.HasPrefix(horizontal[6], "payload ") {
		t.Fatalf("horizontal scroll: %q", horizontal[6])
	}
	clickFileRetentionText(t, app, 100, 24, "second.txt")
	state.pump(app, 48, 18)
	state.pump(app, 120, 28)
	clickFileRetentionText(t, app, 120, 28, "first.txt")
	state.pump(app, 100, 24)
	if got := paintedRows(app, 100, 24); !reflect.DeepEqual(horizontal[6:16], got[6:16]) {
		t.Fatalf("horizontal and vertical scroll restoration:\nbefore=%q\nafter=%q", horizontal[6:16], got[6:16])
	}
	if got := len(files.inputs()); got != 2 {
		t.Fatalf("retention should reuse loaded content: reads=%d", got)
	}
}
