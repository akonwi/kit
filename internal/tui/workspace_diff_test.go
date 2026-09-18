package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/highlight"
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
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
	observation      protocol.WorkingTreePage
	observationPages map[string]protocol.WorkingTreePage
	pages            map[string]protocol.FileDiffPage
}

func (f fakeWorkingTreeDiff) ObserveWorkingTree(_ context.Context, input protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
	if page, ok := f.observationPages[input.Cursor]; ok {
		return page, nil
	}
	return f.observation, nil
}

func (f fakeWorkingTreeDiff) ReadFileDiff(_ context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
	if page, ok := f.pages[input.Path+"\x00"+input.Cursor]; ok {
		return page, nil
	}
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

func findRenderedDiffText(t *testing.T, application *uitest.App, width, height int, wanted string) (int, int) {
	t.Helper()
	wantedRunes := []rune(wanted)
	for row := 0; row < height; row++ {
		for column := 0; column+len(wantedRunes) <= width; column++ {
			matched := true
			for offset, expected := range wantedRunes {
				if application.Cell(column+offset, row).Grapheme != string(expected) {
					matched = false
					break
				}
			}
			if matched {
				return column, row
			}
		}
	}
	t.Fatalf("rendered text %q not found", wanted)
	return 0, 0
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

func TestWorkspaceDiffSyntaxLinesPreserveSemanticRoles(t *testing.T) {
	keyword := vaxis.IndexColor(123)
	result := highlight.Result{Source: "const value\n", Spans: []highlight.Span{{Start: 0, End: 5, Role: highlight.Keyword}}}
	rows := workspaceDiffSyntaxLines(result, SemanticTheme{SyntaxPalette: map[string]ui.Color{string(highlight.Keyword): keyword}})
	if len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("syntax rows = %+v", rows)
	}
	if rows[0][0].Text != "const" || rows[0][0].Style.Foreground != keyword || rows[0][1].Text != " value" {
		t.Fatalf("syntax row = %+v", rows[0])
	}
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
	for _, expected := range []string{"internal/app.go", "1 of 1", "+2 −2", "◆ -2,2 +2,2", "+ old value", "− old next", "+ new value", "+ next value", "[ ] files", "{ } hunks", "r refresh"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("diff missing %q:\n%s", expected, text)
		}
	}
	selectedColumn, selectedRow := findTextCell(t, rows, "old value")
	otherColumn, otherRow := findTextCell(t, rows, "old next")
	if application.Cell(selectedColumn, selectedRow).Style.Background != application.Cell(otherColumn, otherRow).Style.Background {
		t.Fatal("active-line cursor replaced the removed-line diff background")
	}
	buttonColumn, buttonRow := findTextCell(t, rows, "+ old value")
	if application.Cell(buttonColumn, buttonRow).Style.Background != ui.DefaultTheme().Primary {
		t.Fatalf("active-line gutter button background = %v, want primary", application.Cell(buttonColumn, buttonRow).Style.Background)
	}
	oldNextColumn, oldNextRow := findTextCell(t, rows, "old next")
	markerColumn, markerRow := findTextCell(t, rows, "− old next")
	if application.Cell(markerColumn, markerRow).UnderlineStyle != ui.UnderlineOff {
		t.Fatal("inactive gutter action rendered an underline artifact")
	}
	application.Send(ui.Mouse{Col: oldNextColumn, Row: oldNextRow, EventType: ui.EventMotion})
	application.Pump(80, 12)
	hoveredRows := paintedRows(application, 80, 12)
	hoverButtonColumn, hoverButtonRow := findTextCell(t, hoveredRows, "+ old next")
	if strings.Contains(hoveredRows[selectedRow], "+ old value") {
		t.Fatalf("mouse movement left the prior gutter cursor visible:\n%s", strings.Join(hoveredRows, "\n"))
	}
	if application.Cell(hoverButtonColumn, hoverButtonRow).Style.Background != ui.DefaultTheme().Primary {
		t.Fatalf("hover gutter button background = %v, want primary", application.Cell(hoverButtonColumn, hoverButtonRow).Style.Background)
	}
	application.Send(ui.Mouse{Col: 0, Row: 0, EventType: ui.EventMotion})
	application.Pump(80, 12)
	movedOutRows := paintedRows(application, 80, 12)
	if !strings.Contains(movedOutRows[oldNextRow], "+ old next") {
		t.Fatalf("mouse cursor snapped back after leaving diff viewport:\n%s", strings.Join(movedOutRows, "\n"))
	}
}

func TestWorkspaceDiffUnifiedRangeStylesOnlyAnchoredSide(t *testing.T) {
	oldLine, newLine := 12, 12
	application := uitest.New(workspaceDiffLineWidget(
		protocol.DiffLine{Kind: "context", OldLine: &oldLine, NewLine: &newLine, Content: "same", HasTerminatingLF: true},
		nil, false, true, false, ui.DefaultTheme(), semanticFallback(ui.DefaultTheme()), nil, nil, nil,
	))
	application.Pump(40, 2)
	if application.Cell(4, 0).Style.Foreground != ui.DefaultTheme().Primary {
		t.Fatalf("old range line number foreground = %v, want primary", application.Cell(4, 0).Style.Foreground)
	}
	if application.Cell(10, 0).Style.Foreground != ui.DefaultTheme().MutedForeground {
		t.Fatalf("new unselected line number foreground = %v, want muted", application.Cell(10, 0).Style.Foreground)
	}
}

func TestWorkspaceDiffSplitHorizontalSlicePreservesStyledGraphemes(t *testing.T) {
	first := ui.Style{Foreground: vaxis.IndexColor(1)}
	second := ui.Style{Foreground: vaxis.IndexColor(2)}
	spans := workspaceDiffSliceSpans([]ui.TextSpan{{Text: "abc", Style: first}, {Text: "世界", Style: second}}, 4)
	if len(spans) != 1 || spans[0].Text != "界" || spans[0].Style != second {
		t.Fatalf("sliced spans = %+v", spans)
	}
}

func TestWorkspaceDiffSplitLayoutMeasurementsAreCached(t *testing.T) {
	lineNumber := 1
	state := &workspaceDiffPaneState{
		viewportWidth: 120,
		hunks: []protocol.DiffHunk{{OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 1, Lines: []protocol.DiffLine{{
			Kind: "addition", NewLine: &lineNumber, Content: strings.Repeat("wide ", 40), HasTerminatingLF: true,
		}}}},
	}
	state.rebuildHunkRows()
	first := state.splitTargets()
	if len(first) != 1 || !state.splitTargetsValid {
		t.Fatalf("initial split measurements = %+v", first)
	}
	first[0].height = 99
	if got := state.splitTargets()[0].height; got != 99 {
		t.Fatalf("cached split height = %d, want reused measurement", got)
	}
	firstNavigation := state.splitNavigationLines()
	firstNavigation[0].row = 42
	if got := state.splitNavigationLines()[0].row; got != 42 {
		t.Fatalf("cached navigation row = %d, want reused index", got)
	}
	state.invalidateSplitTargets()
	if state.splitTargetsValid || state.splitNavigationValid {
		t.Fatal("split caches remained valid after invalidation")
	}
}

func TestWorkspaceDiffSplitWrapHeightIsConfigurable(t *testing.T) {
	lineNumber := 100_000
	line := protocol.DiffLine{Kind: "addition", NewLine: &lineNumber, Content: strings.Repeat("wide ", 40), HasTerminatingLF: true}
	if height := workspaceDiffSplitPairHeight(nil, &line, 120, false); height != 1 {
		t.Fatalf("clipped height = %d, want 1", height)
	}
	if height := workspaceDiffSplitPairHeight(nil, &line, 120, true); height <= 1 {
		t.Fatalf("wrapped height = %d, want multiple rows", height)
	}
	oldWidth, newWidth := workspaceDiffSplitWidths(120)
	if oldWidth+newWidth+1 != 120 || newWidth-oldWidth > 1 {
		t.Fatalf("split widths = %d/%d, want stable 50/50", oldWidth, newWidth)
	}
}

func TestWorkspaceDiffPaneTogglesSplitLineWrapping(t *testing.T) {
	newLine := 1
	file := textDiffFile("wrapped.go", 1, 0)
	observation := testDiffObservation(file)
	backend := fakeWorkingTreeDiff{
		observation: observation,
		pages: map[string]protocol.FileDiffPage{"wrapped.go": {
			Observation: observation.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"},
			Hunks: []protocol.DiffHunk{{OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 1, Lines: []protocol.DiffLine{{
				Kind: "addition", NewLine: &newLine, Content: "START-" + strings.Repeat("x", 154), HasTerminatingLF: true,
			}}}},
		}},
	}
	dispatch := &queuedDiffDispatch{}
	var persistedWrap bool
	application := uitest.New(workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch,
		Presentation:       workspacePanePresentation{Active: true, Visible: true, Focused: true},
		OnWrapLinesChanged: func(enabled bool) { persistedWrap = enabled },
	})
	pumpDiffUntil(t, application, dispatch, 120, 12, "START-")
	contentColumn, contentRow := findRenderedDiffText(t, application, 120, 12, "START-")
	application.Send(vaxis.Mouse{Col: contentColumn, Row: contentRow, Button: vaxis.MouseWheelRight, EventType: vaxis.EventPress})
	application.Pump(120, 12)
	if strings.Contains(strings.Join(paintedRows(application, 120, 12), "\n"), "START-") {
		t.Fatal("horizontal mouse wheel did not pan clipped split content")
	}
	application.Send(vaxis.Key{Text: "w", Keycode: 'w'})
	for range 3 {
		application.Pump(120, 12)
	}
	rows := paintedRows(application, 120, 12)
	wrappedRows := 0
	for _, row := range rows {
		if strings.Contains(row, "xxxxxxxx") {
			wrappedRows++
		}
	}
	if wrappedRows < 2 || !strings.Contains(strings.Join(rows, "\n"), "w clip") {
		t.Fatalf("split line did not wrap into multiple visual rows:\n%s", strings.Join(rows, "\n"))
	}
	if !persistedWrap {
		t.Fatal("wrap preference callback did not persist enabled state")
	}
	reopenedDispatch := &queuedDiffDispatch{}
	reopened := uitest.New(workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: reopenedDispatch.dispatch,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}, InitialWrapLines: persistedWrap,
	})
	reopenedRows := pumpDiffUntil(t, reopened, reopenedDispatch, 120, 12, "w clip")
	if !strings.Contains(strings.Join(reopenedRows, "\n"), "w clip") {
		t.Fatalf("reopened diff did not restore wrap preference:\n%s", strings.Join(reopenedRows, "\n"))
	}
}

func TestWorkspaceDiffSplitNavigationVisitsLogicalLinesOnce(t *testing.T) {
	state := workspaceDiffPaneState{
		viewportWidth: workspaceDiffSplitBreakpoint,
		hunks: []protocol.DiffHunk{{Lines: []protocol.DiffLine{
			{Kind: "context"}, {Kind: "deletion"}, {Kind: "context"}, {Kind: "addition"},
		}}},
		cursorRow: 1, cursorSide: workspaceDiffSideOld,
	}
	state.rebuildHunkRows()
	state.moveSplitLine(1)
	if state.cursorRow != 2 || state.cursorSide != workspaceDiffSideOld {
		t.Fatalf("deletion target = row:%d side:%d, want old side", state.cursorRow, state.cursorSide)
	}
	state.moveSplitLine(1)
	if state.cursorRow != 3 || state.cursorSide != workspaceDiffSideOld {
		t.Fatalf("unchanged target = row:%d side:%d, want preserved old side", state.cursorRow, state.cursorSide)
	}
	state.moveSplitLine(1)
	if state.cursorRow != 4 || state.cursorSide != workspaceDiffSideNew {
		t.Fatalf("addition target = row:%d side:%d, want natural new side", state.cursorRow, state.cursorSide)
	}
	state.moveSplitLine(-1)
	if state.cursorRow != 3 || state.cursorSide != workspaceDiffSideNew {
		t.Fatalf("reverse unchanged target = row:%d side:%d, want preserved new side", state.cursorRow, state.cursorSide)
	}

	state.hunks = []protocol.DiffHunk{{Lines: []protocol.DiffLine{
		{Kind: "deletion"}, {Kind: "deletion"}, {Kind: "addition"}, {Kind: "addition"},
	}}}
	state.cursorRow = 1
	state.cursorSide = workspaceDiffSideOld
	state.invalidateSplitTargets()
	state.moveSplitLine(1)
	if state.cursorRow != 2 || state.cursorSide != workspaceDiffSideOld {
		t.Fatalf("replacement deletion target = row:%d side:%d", state.cursorRow, state.cursorSide)
	}
	state.moveSplitLine(1)
	if state.cursorRow != 3 || state.cursorSide != workspaceDiffSideNew {
		t.Fatalf("replacement addition target = row:%d side:%d", state.cursorRow, state.cursorSide)
	}
}

func TestWorkspaceDiffSplitCursorMappingSupportsAdditionFirstRuns(t *testing.T) {
	state := workspaceDiffPaneState{
		viewportWidth: workspaceDiffSplitBreakpoint,
		hunks:         []protocol.DiffHunk{{Lines: []protocol.DiffLine{{Kind: "addition"}, {Kind: "deletion"}}}},
	}
	for _, target := range []struct {
		cursor int
		side   workspaceDiffSide
	}{{1, workspaceDiffSideNew}, {2, workspaceDiffSideOld}} {
		state.cursorRow = target.cursor
		state.cursorSide = target.side
		if row := state.cursorVisualRow(); row != 1 {
			t.Fatalf("cursor %d rendered row = %d, want paired row 1", target.cursor, row)
		}
	}
}

func TestWorkspaceDiffPaneUsesSideBySideLayoutAtWideBreakpoint(t *testing.T) {
	oldLine, newLine := 7, 7
	file := textDiffFile("wide.go", 1, 1)
	observation := testDiffObservation(file)
	backend := fakeWorkingTreeDiff{
		observation: observation,
		pages: map[string]protocol.FileDiffPage{"wide.go": {
			Observation: observation.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"},
			Hunks: []protocol.DiffHunk{{OldStart: 7, OldCount: 1, NewStart: 7, NewCount: 1, Lines: []protocol.DiffLine{
				{Kind: "deletion", OldLine: &oldLine, Content: "removed side", HasTerminatingLF: true},
				{Kind: "addition", NewLine: &newLine, Content: "added side", HasTerminatingLF: true},
			}}},
		}},
	}
	dispatch := &queuedDiffDispatch{}
	application := uitest.New(workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true},
	})
	rows := pumpDiffUntil(t, application, dispatch, 140, 12, "removed side")
	for range 3 {
		dispatch.flush()
		application.Pump(140, 12)
	}
	rows = paintedRows(application, 140, 12)
	text := strings.Join(rows, "\n")
	_, oldRow := findRenderedDiffText(t, application, 140, 12, "removed side")
	newColumn, newRow := findRenderedDiffText(t, application, 140, 12, "added side")
	if oldRow != newRow {
		t.Fatalf("paired replacement rows differ: old=%d new=%d\n%s", oldRow, newRow, text)
	}
	separator := -1
	for column := 0; column < 140; column++ {
		if application.Cell(column, oldRow).Grapheme == glyphTableSeparator {
			separator = column
			break
		}
	}
	if separator < 68 || separator > 70 {
		t.Fatalf("split separator column = %d, want centered 50/50\n%s", separator, text)
	}
	if !strings.Contains(rows[oldRow], "+ removed side") {
		t.Fatalf("old-side cursor button is not visible:\n%s", text)
	}
	application.Send(ui.Mouse{Col: newColumn, Row: newRow, EventType: ui.EventMotion})
	application.Pump(140, 12)
	hovered := paintedRows(application, 140, 12)
	if strings.Contains(hovered[oldRow], "+ removed side") || !strings.Contains(hovered[newRow], "+ added side") {
		t.Fatalf("cursor button did not move from old to new side:\n%s", strings.Join(hovered, "\n"))
	}
}

func TestWorkspaceDiffPaneOpensPinnedAnnotationObservation(t *testing.T) {
	firstLine, oldLine := 1, 2
	file := textDiffFile("pinned.go", 0, 2)
	observation := testDiffObservation(file)
	backend := fakeWorkingTreeDiff{pages: map[string]protocol.FileDiffPage{
		"pinned.go": {
			Observation: observation.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"}, NextCursor: "next",
			Hunks: []protocol.DiffHunk{{OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 0, Lines: []protocol.DiffLine{{Kind: "deletion", OldLine: &firstLine, Content: "first page", HasTerminatingLF: true}}}},
		},
		"pinned.go\x00next": {
			Observation: observation.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"},
			Hunks: []protocol.DiffHunk{{OldStart: 2, OldCount: 1, NewStart: 2, NewCount: 0, Lines: []protocol.DiffLine{{Kind: "deletion", OldLine: &oldLine, Content: "pinned evidence", HasTerminatingLF: true}}}},
		},
	}}
	descriptor := workingTreeDiffWorkspacePane(testDiffWorkspace)
	descriptor.Path = file.Path
	descriptor.DiffTargetID = observation.Observation.Target.ID
	descriptor.ExpectedRevision = observation.Observation.Revision
	descriptor.ExpectedFileRevision = file.FileRevision
	descriptor.DiffSide = "old"
	descriptor.RevealStartLine = 1
	descriptor.RevealEndLine = 2
	anchor := protocol.WorkingTreeDiffAnnotationAnchor{
		TargetID: observation.Observation.Target.ID, TargetRevision: observation.Observation.Revision, Path: file.Path,
		FileRevision: file.FileRevision, Side: "old", StartLine: 1, EndLine: 2,
	}
	dispatch := &queuedDiffDispatch{}
	application := uitest.New(workspaceDiffPane{
		Descriptor: descriptor, Diff: backend, Dispatch: dispatch.dispatch,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true},
		Annotations:  []protocol.AnnotationSummary{{ID: 4, Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &anchor}, BodyPreview: "range note", Preview: "first page\npinned evidence"}},
	})
	rows := pumpDiffUntil(t, application, dispatch, 80, 10, "range note")
	if !strings.Contains(strings.Join(rows, "\n"), "+ first page") || !strings.Contains(strings.Join(rows, "\n"), "pinned evidence") {
		t.Fatalf("complete pinned range was not loaded and revealed:\n%s", strings.Join(rows, "\n"))
	}
}

func TestWorkspaceDiffPaneCreatesRevisionPinnedAnnotation(t *testing.T) {
	oldLine, oldNext, newLine, newNext, contextOld, contextNew := 2, 3, 2, 3, 4, 4
	file := textDiffFile("comment.go", 2, 2)
	observation := testDiffObservation(file)
	backend := fakeWorkingTreeDiff{observation: observation, pages: map[string]protocol.FileDiffPage{"comment.go": {
		Observation: observation.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"},
		Hunks: []protocol.DiffHunk{{OldStart: 2, OldCount: 3, NewStart: 2, NewCount: 3, Lines: []protocol.DiffLine{
			{Kind: "deletion", OldLine: &oldLine, Content: "old value", HasTerminatingLF: true},
			{Kind: "deletion", OldLine: &oldNext, Content: "old next", HasTerminatingLF: true},
			{Kind: "addition", NewLine: &newLine, Content: "new value", HasTerminatingLF: true},
			{Kind: "addition", NewLine: &newNext, Content: "new next", HasTerminatingLF: true},
			{Kind: "context", OldLine: &contextOld, NewLine: &contextNew, Content: "shared context", HasTerminatingLF: true},
		}}},
	}}}
	dispatch := &queuedDiffDispatch{}
	var created protocol.AnnotationAnchor
	var removed uint64
	existingAnchor := protocol.WorkingTreeDiffAnnotationAnchor{
		TargetID: observation.Observation.Target.ID, TargetRevision: observation.Observation.Revision,
		Path: file.Path, FileRevision: file.FileRevision, Side: "old", StartLine: 2, EndLine: 2,
	}
	application := uitest.New(workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch,
		Presentation:       workspacePanePresentation{Active: true, Visible: true, Focused: true},
		Annotations:        []protocol.AnnotationSummary{{ID: 9, Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &existingAnchor}, BodyPreview: "existing note", Preview: "old value"}},
		OnRemoveAnnotation: func(_ ui.EventContext, id uint64) { removed = id },
		OnCreateAnnotation: func(anchor protocol.AnnotationAnchor, body string, done func(error)) {
			created = anchor
			if body != "walk" {
				t.Errorf("annotation body = %q", body)
			}
			done(nil)
		},
	})
	pumpDiffUntil(t, application, dispatch, 120, 14, "old value")
	if !application.Contains("existing note") {
		t.Fatalf("saved diff annotation was not rendered inline:\n%s", application.Text())
	}
	application.Send(vaxis.Key{Keycode: 'd', Text: "d"})
	if removed != 9 {
		t.Fatalf("removed annotation = %d, want 9", removed)
	}
	rows := paintedRows(application, 120, 14)
	gutterColumn, firstRow := findTextCell(t, rows, "+ old value")
	_, secondRow := findTextCell(t, rows, "− old next")
	application.Send(vaxis.Mouse{Col: gutterColumn, Row: firstRow, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	application.Send(vaxis.Mouse{Col: gutterColumn, Row: secondRow, Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion})
	application.Send(vaxis.Mouse{Col: gutterColumn, Row: secondRow, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})
	application.Pump(120, 14)
	if !application.Contains("Write a comment") {
		t.Fatalf("comment editor did not open:\n%s", application.Text())
	}
	editorColumn, editorRow := findTextCell(t, paintedRows(application, 120, 14), "Write a comment")
	oldWidth, _ := workspaceDiffSplitWidths(120)
	if editorColumn >= oldWidth || application.Cell(oldWidth, editorRow).Grapheme != glyphTableSeparator {
		t.Fatalf("old-side comment editor crossed split boundary: column=%d divider=%q", editorColumn, application.Cell(oldWidth, editorRow).Grapheme)
	}
	for _, character := range "walk" {
		application.Send(vaxis.Key{Keycode: character, Text: string(character)})
	}
	application.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	application.Pump(120, 14)
	anchor := created.WorkingTreeDiff
	if anchor == nil || anchor.TargetID != observation.Observation.Target.ID || anchor.TargetRevision != observation.Observation.Revision || anchor.FileRevision != file.FileRevision || anchor.Path != file.Path || anchor.Side != "old" || anchor.StartLine != 2 || anchor.EndLine != 3 {
		t.Fatalf("created diff anchor = %+v", created)
	}
	application.Send(vaxis.Key{Keycode: 'v', Text: "v"})
	application.Send(vaxis.Key{Keycode: vaxis.KeyDown}) // Skip opposite-side additions to contiguous old context.
	application.Pump(120, 14)
	if !application.Contains("c old L3–4") {
		t.Fatalf("keyboard range did not skip opposite-side records:\n%s", application.Text())
	}
}

func TestWorkspaceDiffPaneLoadsMoreHunksNearLoadedBoundary(t *testing.T) {
	oldFirst, oldSecond := 1, 2
	file := textDiffFile("paged.go", 0, 2)
	observation := testDiffObservation(file)
	backend := fakeWorkingTreeDiff{
		observation: observation,
		pages: map[string]protocol.FileDiffPage{
			"paged.go": {
				Observation: observation.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"}, NextCursor: "next-hunks",
				Hunks: []protocol.DiffHunk{{OldStart: 1, OldCount: 2, NewStart: 0, NewCount: 0, ContinuedAfter: true, Lines: []protocol.DiffLine{{Kind: "deletion", OldLine: &oldFirst, Content: "first removed", HasTerminatingLF: true}}}},
			},
			"paged.go\x00next-hunks": {
				Observation: observation.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"},
				Hunks: []protocol.DiffHunk{{OldStart: 1, OldCount: 2, NewStart: 0, NewCount: 0, ContinuedBefore: true, Lines: []protocol.DiffLine{{Kind: "deletion", OldLine: &oldSecond, Content: "second removed", HasTerminatingLF: true}}}},
			},
		},
	}
	dispatch := &queuedDiffDispatch{}
	application := uitest.New(workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true},
	})
	pumpDiffUntil(t, application, dispatch, 80, 12, "first removed")
	application.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	rows := pumpDiffUntil(t, application, dispatch, 80, 12, "second removed")
	text := strings.Join(rows, "\n")
	if !strings.Contains(text, "− second removed") || !strings.Contains(text, "◆ -1,2 +0,0") {
		t.Fatalf("next hunk page was not merged with stable hunk counts:\n%s", text)
	}
}

func TestWorkspaceDiffPaneLoadsAllChangedFilePagesForCycling(t *testing.T) {
	first := textDiffFile("first.go", 1, 0)
	second := textDiffFile("second.go", 0, 1)
	initial := testDiffObservation(first)
	initial.NextCursor = "next-files"
	backend := fakeWorkingTreeDiff{
		observation: initial,
		observationPages: map[string]protocol.WorkingTreePage{
			"next-files": testDiffObservation(second),
		},
		pages: map[string]protocol.FileDiffPage{
			"first.go":  {Observation: initial.Observation, File: first, Computation: protocol.DiffComputation{State: "complete"}, Hunks: []protocol.DiffHunk{}},
			"second.go": {Observation: initial.Observation, File: second, Computation: protocol.DiffComputation{State: "complete"}, Hunks: []protocol.DiffHunk{}},
		},
	}
	dispatch := &queuedDiffDispatch{}
	application := uitest.New(workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true},
	})
	rows := pumpDiffUntil(t, application, dispatch, 80, 12, "1 of 2")
	if !strings.Contains(strings.Join(rows, "\n"), "first.go") {
		t.Fatalf("first changed file was not selected:\n%s", strings.Join(rows, "\n"))
	}
	application.Send(vaxis.Key{Text: "]", Keycode: ']'})
	rows = pumpDiffUntil(t, application, dispatch, 80, 12, "second.go")
	if !strings.Contains(strings.Join(rows, "\n"), "2 of 2") {
		t.Fatalf("second changed-file page was not available to cycling:\n%s", strings.Join(rows, "\n"))
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
