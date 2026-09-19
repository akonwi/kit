package tui

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type retainedDiffDispatch struct {
	mu        sync.Mutex
	callbacks []func()
}

func (d *retainedDiffDispatch) dispatch(callback func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.callbacks = append(d.callbacks, callback)
}

func (d *retainedDiffDispatch) flush() {
	d.mu.Lock()
	callbacks := d.callbacks
	d.callbacks = nil
	d.mu.Unlock()
	for _, callback := range callbacks {
		callback()
	}
}

type retainedDiffShell struct{ state *retainedDiffShellState }

func (w retainedDiffShell) CreateState() ui.State { return w.state }

type retainedDiffShellState struct {
	appState
	diff         fakeWorkingTreeDiff
	dispatch     retainedDiffDispatch
	pane         *workspaceDiffPaneState
	copied       string
	interactions []protocol.InteractionRequest
	response     protocol.InteractionResponse
}

func (s *retainedDiffShellState) InitState() {
	s.phase = phaseReady
	s.session = protocol.SessionInfo{ID: toolNavigationSessionID, Name: "Retention", Model: "test/echo", CWD: "/repo"}
	s.workspaceID = testDiffWorkspace
}
func (*retainedDiffShellState) Dispose() {}
func (s *retainedDiffShellState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}
func (s *retainedDiffShellState) Build(ui.BuildContext) ui.Widget {
	s.pendingInteractions = s.interactions
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	view := shellView{Snapshot: shellSnapshot{
		Phase: phaseReady,
		Session: protocol.SessionInfo{
			ID: toolNavigationSessionID, Name: "Retention", Model: "test/echo", CWD: "/repo",
		},
		CurrentWorkspaceID:  testDiffWorkspace,
		Workspace:           s.workspace.Snapshot(),
		Scroll:              &ui.ScrollController{},
		PendingInteractions: s.interactions,
	}, Callbacks: shellCallbacks{
		InputOwner: s.inputOwner,
		RespondInteraction: func(_ ui.EventContext, response protocol.InteractionResponse, done func(error)) {
			s.response = response
			done(nil)
		},
		MoveWorkspaceSelection: func(_ ui.EventContext, delta int) {
			s.SetState(func() { s.workspace.MoveSelection(delta) })
		},
		SelectWorkspacePane: func(_ ui.EventContext, descriptor workspacePaneDescriptor) {
			identity, err := workspacePaneIdentityFor(descriptor)
			if err == nil {
				s.SetState(func() { s.workspace.Select(identity) })
			}
		},
		MoveWorkspaceFocus: func(ui.EventContext) {
			s.SetState(func() { s.workspace.MoveFocus() })
		},
		FocusWorkspaceContent: func(ui.EventContext) {
			s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusContent) })
		},
		CreateAnnotation: func(protocol.AnnotationAnchor, string, func(error)) {},
	}, WorkspaceDispatch: s.dispatch.dispatch, Diff: s.diff}
	return keyShortcuts{Bindings: ui.ShortcutMap{"Alt+y": ui.CopySelectionTextIntent{OnCopied: func(text string) { s.copied = text }}}, Child: view}
}

func (s *retainedDiffShellState) reopenDiff(startLine, endLine int) error {
	descriptor := workingTreeDiffWorkspacePane(testDiffWorkspace)
	descriptor.Path = "retained.go"
	descriptor.DiffTargetID = testDiffTarget
	descriptor.ExpectedRevision = testDiffRevision
	descriptor.ExpectedFileRevision = testFileRevision
	descriptor.DiffSide = "new"
	descriptor.RevealStartLine = startLine
	descriptor.RevealEndLine = endLine
	var openErr error
	s.SetState(func() { _, _, openErr = s.workspace.Open(descriptor) })
	return openErr
}

func retainedDiffFixture() fakeWorkingTreeDiff {
	file := textDiffFile("retained.go", 1, 1)
	observation := testDiffObservation(file)
	lines := make([]protocol.DiffLine, 0, 31)
	for line := 1; line <= 30; line++ {
		line := line
		if line == 13 {
			lines = append(lines,
				protocol.DiffLine{Kind: "deletion", OldLine: &line, Content: "removed retained line 13", HasTerminatingLF: true},
				protocol.DiffLine{Kind: "addition", NewLine: &line, Content: "added retained line 13", HasTerminatingLF: true},
			)
			continue
		}
		lines = append(lines, protocol.DiffLine{
			Kind: "context", OldLine: &line, NewLine: &line,
			Content: fmt.Sprintf("retained line %02d", line), HasTerminatingLF: true,
		})
	}
	return fakeWorkingTreeDiff{
		observation: observation,
		pages: map[string]protocol.FileDiffPage{"retained.go": {
			Observation: observation.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"},
			Hunks: []protocol.DiffHunk{{OldStart: 1, OldCount: 30, NewStart: 1, NewCount: 30, Lines: lines}},
		}},
	}
}

func mountRetainedDiffShell(t *testing.T) (*uitest.App, *retainedDiffShellState) {
	t.Helper()
	state := &retainedDiffShellState{diff: retainedDiffFixture(), pane: &workspaceDiffPaneState{}}
	if _, _, err := state.workspace.Open(workingTreeDiffWorkspacePane(testDiffWorkspace)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := state.workspace.Open(fileWorkspacePane(testDiffWorkspace, "other.go")); err != nil {
		t.Fatal(err)
	}
	diffIdentity, err := workspacePaneIdentityFor(workingTreeDiffWorkspacePane(testDiffWorkspace))
	if err != nil || !state.workspace.Select(diffIdentity) {
		t.Fatalf("select Diff pane: identity=%q err=%v", diffIdentity, err)
	}

	// uitest does not drive frameTicker callbacks. Keep this test sequential and
	// temporarily expose the production registry pane's state for deterministic
	// TickFrame calls; cleanup restores the shared definition.
	definition := workspacePaneDefinitions[workspacePaneDiff]
	mountedDefinition := definition
	mountedDefinition.Build = func(view shellView, theme ui.Theme, descriptor workspacePaneDescriptor, presentation workspacePanePresentation) ui.Widget {
		pane := definition.Build(view, theme, descriptor, presentation).(workspaceDiffPane)
		pane.testState = state.pane
		return pane
	}
	workspacePaneDefinitions[workspacePaneDiff] = mountedDefinition
	t.Cleanup(func() {
		state.pane.Dispose()
		workspacePaneDefinitions[workspacePaneDiff] = definition
	})

	application := uitest.New(retainedDiffShell{state: state})
	pumpRetainedDiffShell(t, application, state, 140, 18, "retained line 01")
	return application, state
}

func pumpRetainedDiffShell(t *testing.T, application *uitest.App, state *retainedDiffShellState, width, height int, wanted string) []string {
	t.Helper()
	for range 200 {
		state.dispatch.flush()
		application.Pump(width, height)
		state.pane.TickFrame(time.Now())
		application.Pump(width, height)
		rows := paintedRows(application, width, height)
		if strings.Contains(strings.Join(rows, "\n"), wanted) {
			return rows
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("shell Diff pane did not render %q:\n%s", wanted, strings.Join(paintedRows(application, width, height), "\n"))
	return nil
}

func retainedDiffCursor(application *uitest.App, width, height int, content string) (int, bool) {
	wanted := []rune(content)
	for row := 0; row < height; row++ {
		found := false
		for column := 0; column+len(wanted) <= width; column++ {
			matched := true
			for offset, expected := range wanted {
				if application.Cell(column+offset, row).Grapheme != string(expected) {
					matched = false
					break
				}
			}
			found = found || matched
		}
		if !found {
			continue
		}
		for column := 0; column < width; column++ {
			cell := application.Cell(column, row)
			if cell.Grapheme == "+" && cell.Style.Background == ui.DefaultTheme().Primary {
				return row, true
			}
		}
	}
	return -1, false
}

func retainedDiffCursorRow(t *testing.T, application *uitest.App, width, height int, content string) int {
	t.Helper()
	if row, ok := retainedDiffCursor(application, width, height, content); ok {
		return row
	}
	t.Fatalf("focused Diff cursor styling missing from %q row", content)
	return -1
}

func pumpRetainedDiffCursor(t *testing.T, application *uitest.App, state *retainedDiffShellState, width, height int, content string) []string {
	t.Helper()
	for range 200 {
		state.dispatch.flush()
		application.Pump(width, height)
		state.pane.TickFrame(time.Now())
		application.Pump(width, height)
		if _, ok := retainedDiffCursor(application, width, height, content); ok {
			return paintedRows(application, width, height)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("shell Diff cursor did not reach %q:\n%s", content, strings.Join(paintedRows(application, width, height), "\n"))
	return nil
}

func firstVisibleRetainedDiffLine(rows []string) string {
	for _, row := range rows {
		for line := 1; line <= 30; line++ {
			candidate := fmt.Sprintf("retained line %02d", line)
			if strings.Contains(row, candidate) {
				return candidate
			}
		}
	}
	return ""
}

func rowContainsSplitSeparator(application *uitest.App, width, row int) bool {
	for column := 0; column < width; column++ {
		if application.Cell(column, row).Grapheme == glyphTableSeparator {
			return true
		}
	}
	return false
}

func TestShellDiffPaneRetainsCursorRangeScrollAndFocusAcrossTabsAndResize(t *testing.T) {
	application, state := mountRetainedDiffShell(t)
	const wideWidth, narrowWidth, height = 140, 80, 18

	application.Key("v")
	for range 14 {
		application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
		state.pane.TickFrame(time.Now())
		application.Pump(wideWidth, height)
	}
	wideRows := pumpRetainedDiffShell(t, application, state, wideWidth, height, "new L1–15")
	cursorRow := retainedDiffCursorRow(t, application, wideWidth, height, "retained line 15")
	if !rowContainsSplitSeparator(application, wideWidth, cursorRow) {
		t.Fatalf("wide Diff cursor row is not split:\n%s", strings.Join(wideRows, "\n"))
	}
	_, removedRow := findRenderedDiffText(t, application, wideWidth, height, "removed retained line 13")
	_, addedRow := findRenderedDiffText(t, application, wideWidth, height, "added retained line 13")
	if removedRow != addedRow || !rowContainsSplitSeparator(application, wideWidth, removedRow) {
		t.Fatalf("wide replacement is not paired on split row %d/%d:\n%s", removedRow, addedRow, strings.Join(wideRows, "\n"))
	}
	firstVisible := firstVisibleRetainedDiffLine(wideRows)
	if firstVisible != "retained line 12" {
		t.Fatalf("scrolled Diff starts at %q, want retained line 12\n%s", firstVisible, strings.Join(wideRows, "\n"))
	}

	application.Send(vaxis.Key{Keycode: '[', Modifiers: vaxis.ModCtrl})
	application.Pump(wideWidth, height)
	agentRows := paintedRows(application, wideWidth, height)
	agentText := strings.Join(agentRows, "\n")
	if !strings.Contains(agentText, "k i t") || !strings.Contains(agentText, "━━━━━━━━━━━") || !strings.Contains(agentText, "Ask a question or give a task.") {
		t.Fatalf("Agent tab content is incomplete:\n%s", agentText)
	}
	application.Send(vaxis.Key{Keycode: ']', Modifiers: vaxis.ModCtrl})
	wideRows = pumpRetainedDiffShell(t, application, state, wideWidth, height, "new L1–15")
	retainedDiffCursorRow(t, application, wideWidth, height, "retained line 15")
	if got := firstVisibleRetainedDiffLine(wideRows); got != firstVisible {
		t.Fatalf("restored Diff scroll starts at %q, want %q\n%s", got, firstVisible, strings.Join(wideRows, "\n"))
	}

	application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	wideRows = pumpRetainedDiffShell(t, application, state, wideWidth, height, "new L1–16")
	retainedDiffCursorRow(t, application, wideWidth, height, "retained line 16")
	resizedFirstVisible := firstVisibleRetainedDiffLine(wideRows)
	if resizedFirstVisible != "retained line 13" {
		t.Fatalf("advanced Diff starts at %q, want retained line 13", resizedFirstVisible)
	}

	narrowRows := pumpRetainedDiffShell(t, application, state, narrowWidth, height, "new L1–16")
	removedColumn, narrowRemovedRow := findRenderedDiffText(t, application, narrowWidth, height, "removed retained line 13")
	addedColumn, narrowAddedRow := findRenderedDiffText(t, application, narrowWidth, height, "added retained line 13")
	if got := firstVisibleRetainedDiffLine(narrowRows); got != "retained line 13" || narrowAddedRow != narrowRemovedRow+1 || removedColumn != 14 || addedColumn != 14 ||
		application.Cell(3, narrowRemovedRow).Grapheme != "1" || application.Cell(4, narrowRemovedRow).Grapheme != "3" || application.Cell(12, narrowRemovedRow).Grapheme != "−" ||
		application.Cell(9, narrowAddedRow).Grapheme != "1" || application.Cell(10, narrowAddedRow).Grapheme != "3" || application.Cell(12, narrowAddedRow).Grapheme != "+" {
		t.Fatalf("narrow unified replacement geometry/top = first:%q old:(%d,%d) new:(%d,%d):\n%s", got, removedColumn, narrowRemovedRow, addedColumn, narrowAddedRow, strings.Join(narrowRows, "\n"))
	}

	wideRows = pumpRetainedDiffShell(t, application, state, wideWidth, height, "new L1–16")
	cursorRow = retainedDiffCursorRow(t, application, wideWidth, height, "retained line 16")
	if got := firstVisibleRetainedDiffLine(wideRows); !rowContainsSplitSeparator(application, wideWidth, cursorRow) || got != resizedFirstVisible {
		t.Fatalf("restored wide Diff scroll starts at %q, want %q:\n%s", got, resizedFirstVisible, strings.Join(wideRows, "\n"))
	}
}

func TestShellDiffRepeatedAnchoredOpenReusesTabWithoutReordering(t *testing.T) {
	application, state := mountRetainedDiffShell(t)
	const width, height = 140, 18

	if err := state.reopenDiff(4, 4); err != nil {
		t.Fatal(err)
	}
	rows := pumpRetainedDiffCursor(t, application, state, width, height, "retained line 04")
	panes := state.workspace.Panes()
	if len(panes) != 2 || panes[0].Kind != workspacePaneDiff || panes[1].Kind != workspacePaneFile {
		t.Fatalf("first anchored open reordered panes: %+v", panes)
	}
	diffColumn, tabRow := findRenderedDiffText(t, application, width, height, "Diff")
	fileColumn, fileRow := findRenderedDiffText(t, application, width, height, "other.go")
	if tabRow != fileRow || diffColumn >= fileColumn {
		t.Fatalf("visible tab order is not Diff then other.go:\n%s", strings.Join(rows, "\n"))
	}

	if err := state.reopenDiff(8, 9); err != nil {
		t.Fatal(err)
	}
	rows = pumpRetainedDiffCursor(t, application, state, width, height, "retained line 08")
	panes = state.workspace.Panes()
	if len(panes) != 2 || panes[0].Kind != workspacePaneDiff || panes[1].Path != "other.go" {
		t.Fatalf("repeated anchored open duplicated or reordered panes: %+v", panes)
	}
	if !strings.Contains(strings.Join(rows, "\n"), "retained line 08") {
		t.Fatalf("repeated anchored cursor was not visibly revealed:\n%s", strings.Join(rows, "\n"))
	}
}

func clickRetainedDiffText(t *testing.T, application *uitest.App, width, height int, text string) (int, int) {
	t.Helper()
	column, row := findRenderedDiffText(t, application, width, height, text)
	application.Send(vaxis.Mouse{Col: column, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	application.Send(vaxis.Mouse{Col: column, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})
	return column, row
}

func TestShellDiffBodySelectionFocusRemainsInsidePaneKeyboardRoute(t *testing.T) {
	const width, height = 140, 18
	t.Run("plain click then keyboard", func(t *testing.T) {
		application, state := mountRetainedDiffShell(t)
		clickRetainedDiffText(t, application, width, height, "◆ -1,30 +1,30")
		application.Pump(width, height)
		application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
		pumpRetainedDiffCursor(t, application, state, width, height, "retained line 02")
	})

	t.Run("drag selection then keyboard", func(t *testing.T) {
		application, state := mountRetainedDiffShell(t)
		column, row := findRenderedDiffText(t, application, width, height, "◆ -1,30 +1,30")
		application.Send(vaxis.Mouse{Col: column, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
		application.Send(vaxis.Mouse{Col: column + 8, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion})
		application.Send(vaxis.Mouse{Col: column + 8, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})
		application.Pump(width, height)
		selectedCells := 0
		for offset := 0; offset <= 8; offset++ {
			if application.Cell(column+offset, row).Style.Background == ui.DefaultTheme().Selection {
				selectedCells++
			}
		}
		if selectedCells != 8 {
			t.Fatalf("Diff drag selection styled %d cells, want 8", selectedCells)
		}
		application.Send(vaxis.Key{Keycode: 'y', Modifiers: vaxis.ModAlt})
		if state.copied != "◆ -1,30 " {
			t.Fatalf("Diff copied selection = %q, want %q", state.copied, "◆ -1,30 ")
		}
		application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
		pumpRetainedDiffCursor(t, application, state, width, height, "retained line 02")
	})
}

func TestShellDiffBackgroundSelectionDefersInputToInteractionDockFrame(t *testing.T) {
	const width, height = 140, 30
	application, state := mountRetainedDiffShell(t)
	pumpRetainedDiffCursor(t, application, state, width, height, "retained line 01")
	state.SetState(func() {
		state.interactions = []protocol.InteractionRequest{{ID: "diff-input", Kind: protocol.InteractionInput, Title: "Answer diff question"}}
	})
	pumpRetainedDiffShell(t, application, state, width, height, "Answer diff question")

	clickRetainedDiffText(t, application, width, height, "◆ -1,30 +1,30")
	application.Key("x")
	application.Enter()
	if state.response.Value != nil {
		t.Fatalf("coalesced background input submitted response %+v", state.response)
	}

	application.Pump(width, height)
	application.Key("answer")
	application.Enter()
	application.Pump(width, height)
	if state.response.Value == nil || *state.response.Value != "answer" {
		t.Fatalf("post-frame dock response = %+v, want answer", state.response)
	}
	retainedDiffCursorRow(t, application, width, height, "retained line 01")
}
