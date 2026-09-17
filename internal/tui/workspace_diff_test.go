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

const (
	testDiffWorkspace = "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testDiffTarget    = "difftarget_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testDiffRevision  = "diffrev_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testFileRevision  = "diff_file_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

type fakeWorkingTreeDiff struct {
	observation protocol.WorkingTreePage
	pages       map[string]protocol.FileDiffPage
}

func (f fakeWorkingTreeDiff) ObserveWorkingTree(context.Context, protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
	return f.observation, nil
}

func (f fakeWorkingTreeDiff) ReadFileDiff(_ context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
	return f.pages[input.Path], nil
}

func testDiffObservation(files ...protocol.DiffFileSummary) protocol.WorkingTreePage {
	return protocol.WorkingTreePage{Observation: protocol.DiffObservation{
		SessionID: "session_test", Target: protocol.DiffTarget{ID: testDiffTarget, WorkspaceID: testDiffWorkspace, Kind: "working_tree"},
		Revision: testDiffRevision, Head: protocol.DiffHead{State: "unborn"}, IndexSummary: "clean", Complete: true, Omissions: []protocol.DiffOmission{},
	}, Files: files}
}

func textDiffFile(path string, additions, deletions int) protocol.DiffFileSummary {
	return protocol.DiffFileSummary{
		Path: path, FileRevision: testFileRevision, Change: "modified",
		Old: protocol.DiffSide{Kind: "regular", Mode: 0100644}, New: protocol.DiffSide{Kind: "regular", Mode: 0100644},
		ContentState: "text", Additions: &additions, Deletions: &deletions,
	}
}

type queuedDiffDispatch struct {
	mu        sync.Mutex
	callbacks []func()
}

func (d *queuedDiffDispatch) dispatch(callback func()) {
	d.mu.Lock()
	d.callbacks = append(d.callbacks, callback)
	d.mu.Unlock()
}

func (d *queuedDiffDispatch) flush() {
	d.mu.Lock()
	callbacks := d.callbacks
	d.callbacks = nil
	d.mu.Unlock()
	for _, callback := range callbacks {
		callback()
	}
}

func pumpDiffUntil(t *testing.T, application *uitest.App, dispatch *queuedDiffDispatch, width, height int, wanted string) []string {
	t.Helper()
	for range 100 {
		dispatch.flush()
		application.Pump(width, height)
		rows := paintedRows(application, width, height)
		if strings.Contains(strings.Join(rows, "\n"), wanted) {
			return rows
		}
		time.Sleep(time.Millisecond)
	}
	rows := paintedRows(application, width, height)
	t.Fatalf("diff never rendered %q:\n%s", wanted, strings.Join(rows, "\n"))
	return nil
}

func TestWorkspaceDiffPaneRendersSemanticUnifiedRows(t *testing.T) {
	oldLine, oldNextLine, newLine, nextLine := 2, 3, 2, 3
	file := textDiffFile("internal/app.go", 2, 2)
	backend := fakeWorkingTreeDiff{
		observation: testDiffObservation(file),
		pages: map[string]protocol.FileDiffPage{"internal/app.go": {
			Observation: testDiffObservation().Observation, File: file, Computation: protocol.DiffComputation{State: "complete"},
			Hunks: []protocol.DiffHunk{{OldStart: 2, OldCount: 2, NewStart: 2, NewCount: 2, Lines: []protocol.DiffLine{
				{Kind: "deletion", OldLine: &oldLine, Content: "old value", HasTerminatingLF: true},
				{Kind: "deletion", OldLine: &oldNextLine, Content: "old next", HasTerminatingLF: true},
				{Kind: "addition", NewLine: &newLine, Content: "new value", HasTerminatingLF: true},
				{Kind: "addition", NewLine: &nextLine, Content: "next value", HasTerminatingLF: true},
			}}},
		}},
	}
	dispatch := &queuedDiffDispatch{}
	application := uitest.New(workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true},
	})
	rows := pumpDiffUntil(t, application, dispatch, 80, 12, "next value")
	text := strings.Join(rows, "\n")
	for _, expected := range []string{"internal/app.go", "1 of 1", "+2 −2", "◆ -2,2 +2,2", " + − old value", "− old next", "+ new value", "+ next value", "[ ] files", "{ } hunks", "r refresh"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("diff missing %q:\n%s", expected, text)
		}
	}
	selectedColumn, selectedRow := findTextCell(t, rows, "old value")
	otherColumn, otherRow := findTextCell(t, rows, "old next")
	if application.Cell(selectedColumn, selectedRow).Style.Background != application.Cell(otherColumn, otherRow).Style.Background {
		t.Fatal("active-line cursor replaced the removed-line diff background")
	}
	buttonColumn, buttonRow := findTextCell(t, rows, "+ − old value")
	if application.Cell(buttonColumn, buttonRow).Style.Background != ui.DefaultTheme().Primary {
		t.Fatalf("active-line gutter button background = %v, want primary", application.Cell(buttonColumn, buttonRow).Style.Background)
	}
	oldNextColumn, oldNextRow := findTextCell(t, rows, "old next")
	application.Send(ui.Mouse{Col: oldNextColumn, Row: oldNextRow, EventType: ui.EventMotion})
	application.Pump(80, 12)
	hoveredRows := paintedRows(application, 80, 12)
	hoverButtonColumn, hoverButtonRow := findTextCell(t, hoveredRows, "+ − old next")
	if strings.Contains(hoveredRows[selectedRow], "+ − old value") {
		t.Fatalf("mouse movement left the prior gutter cursor visible:\n%s", strings.Join(hoveredRows, "\n"))
	}
	if application.Cell(hoverButtonColumn, hoverButtonRow).Style.Background != ui.DefaultTheme().Primary {
		t.Fatalf("hover gutter button background = %v, want primary", application.Cell(hoverButtonColumn, hoverButtonRow).Style.Background)
	}
	application.Send(ui.Mouse{Col: 0, Row: 0, EventType: ui.EventMotion})
	application.Pump(80, 12)
	movedOutRows := paintedRows(application, 80, 12)
	if !strings.Contains(movedOutRows[oldNextRow], "+ − old next") {
		t.Fatalf("mouse cursor snapped back after leaving diff viewport:\n%s", strings.Join(movedOutRows, "\n"))
	}
}

func TestWorkingTreeDiffDescriptorDeduplicatesByWorkspace(t *testing.T) {
	var controller workspaceController
	first, added, err := controller.Open(workingTreeDiffWorkspacePane(testDiffWorkspace))
	if err != nil || !added {
		t.Fatalf("first open = identity:%q added:%v err:%v", first, added, err)
	}
	second, added, err := controller.Open(workingTreeDiffWorkspacePane(testDiffWorkspace))
	if err != nil || added || second != first || len(controller.Panes()) != 1 {
		t.Fatalf("second open = identity:%q added:%v err:%v panes:%+v", second, added, err, controller.Panes())
	}
	if controller.Panes()[0].OpenGeneration <= 1 {
		t.Fatalf("reopen generation = %d", controller.Panes()[0].OpenGeneration)
	}
}
