package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/highlight"
	"github.com/akonwi/kit/internal/protocol"
	kittheme "github.com/akonwi/kit/internal/theme"
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
	catalog              protocol.DiffTargetCatalog
	catalogFn            func(context.Context, protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error)
	catalogErr           error
	catalogWait          <-chan struct{}
	catalogEmpty         bool
	observation          protocol.WorkingTreePage
	observationsByTarget map[string]protocol.WorkingTreePage
	observeFn            func(context.Context, protocol.ObserveDiffInput) (protocol.DiffPage, error)
	readFn               func(context.Context, protocol.ReadFileDiffInput) (protocol.FileDiffPage, error)
	observationPages     map[string]protocol.WorkingTreePage
	pages                map[string]protocol.FileDiffPage
}

func (f fakeWorkingTreeDiff) ListDiffTargets(ctx context.Context, input protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error) {
	if f.catalogFn != nil {
		return f.catalogFn(ctx, input)
	}
	if f.catalogWait != nil {
		select {
		case <-f.catalogWait:
		case <-ctx.Done():
			return protocol.DiffTargetCatalog{}, ctx.Err()
		}
	}
	if f.catalogErr != nil {
		return protocol.DiffTargetCatalog{}, f.catalogErr
	}
	if f.catalogEmpty {
		return protocol.DiffTargetCatalog{Targets: []protocol.DiffTargetEntry{}}, nil
	}
	if len(f.catalog.Targets) > 0 {
		return f.catalog, nil
	}
	return testDiffCatalog(), nil
}

func (f fakeWorkingTreeDiff) ObserveDiff(ctx context.Context, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
	if f.observeFn != nil {
		return f.observeFn(ctx, input)
	}
	if page, ok := f.observationsByTarget[input.TargetReference]; ok {
		return page, nil
	}
	if page, ok := f.observationPages[input.Cursor]; ok {
		return page, nil
	}
	return f.observation, nil
}

func (f fakeWorkingTreeDiff) ReadFileDiff(ctx context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
	if f.readFn != nil {
		return f.readFn(ctx, input)
	}
	if page, ok := f.pages[input.TargetID+"\x00"+input.Path+"\x00"+input.Cursor]; ok {
		return page, nil
	}
	if page, ok := f.pages[input.TargetID+"\x00"+input.Path]; ok {
		return page, nil
	}
	if page, ok := f.pages[input.Path+"\x00"+input.Cursor]; ok {
		return page, nil
	}
	return f.pages[input.Path], nil
}

func testDiffCatalog() protocol.DiffTargetCatalog {
	return protocol.DiffTargetCatalog{SessionID: "session_test", WorkspaceID: testDiffWorkspace, Targets: []protocol.DiffTargetEntry{{
		Reference: "test-working-tree", TargetID: testDiffTarget, Kind: protocol.DiffTargetWorkingTree,
		Metadata: protocol.DiffTargetMetadata{Label: "Working tree"},
	}}}
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

type pollingWorkingTreeDiff struct {
	mu          sync.RWMutex
	observation protocol.WorkingTreePage
	pages       map[string]protocol.FileDiffPage
	calls       int
}

func (f *pollingWorkingTreeDiff) ListDiffTargets(context.Context, protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error) {
	return testDiffCatalog(), nil
}

func (f *pollingWorkingTreeDiff) ObserveDiff(_ context.Context, _ protocol.ObserveDiffInput) (protocol.DiffPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.observation, nil
}

func (f *pollingWorkingTreeDiff) ReadFileDiff(_ context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.pages[input.TargetRevision], nil
}

func (f *pollingWorkingTreeDiff) set(page protocol.WorkingTreePage, file protocol.FileDiffPage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observation = page
	f.pages[page.Observation.Revision] = file
}

func (f *pollingWorkingTreeDiff) observeCalls() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.calls
}

type diffPanePresentationHarness struct{ model *diffPanePresentationModel }

type diffPanePresentationModel struct {
	state  *diffPanePresentationState
	pane   workspaceDiffPane
	active bool
}

type diffPanePresentationState struct{ ui.StateBase }

func (w diffPanePresentationHarness) CreateState() ui.State {
	state := &diffPanePresentationState{}
	w.model.state = state
	return state
}

func (s *diffPanePresentationState) Build(ui.BuildContext) ui.Widget {
	model := s.Widget().(diffPanePresentationHarness).model
	pane := model.pane
	pane.Presentation = workspacePanePresentation{Active: model.active, Visible: model.active, Focused: model.active}
	return pane
}

func (m *diffPanePresentationModel) setActive(active bool) {
	m.state.SetState(func() { m.active = active })
}

func (m *diffPanePresentationModel) setPane(pane workspaceDiffPane) {
	m.state.SetState(func() { m.pane = pane })
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

func TestWorkspaceDiffFailedRefreshWarnsOutsideBuildAndKeepsStaleView(t *testing.T) {
	file := textDiffFile("changed.go", 1, 0)
	observation := testDiffObservation(file)
	line := 1
	var callsMu sync.Mutex
	calls := 0
	backend := fakeWorkingTreeDiff{
		observeFn: func(context.Context, protocol.ObserveDiffInput) (protocol.DiffPage, error) {
			callsMu.Lock()
			defer callsMu.Unlock()
			calls++
			if calls > 1 {
				return protocol.DiffPage{}, errors.New("refresh failed")
			}
			return observation, nil
		},
		pages: map[string]protocol.FileDiffPage{"changed.go": {
			Observation: observation.Observation,
			File:        file,
			Computation: protocol.DiffComputation{State: "complete"},
			Hunks: []protocol.DiffHunk{{Lines: []protocol.DiffLine{{
				Kind: "addition", NewLine: &line, Content: "stale evidence", HasTerminatingLF: true,
			}}}},
		}},
	}
	dispatch := &queuedDiffDispatch{}
	warnings := 0
	model := &diffPanePresentationModel{active: true, pane: workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace),
		Diff:       backend,
		Dispatch:   dispatch.dispatch,
	}}
	model.pane.OnWarning = func(string) {
		model.state.SetState(func() { warnings++ })
	}
	application := uitest.New(diffPanePresentationHarness{model: model})
	pumpDiffUntil(t, application, dispatch, 80, 12, "stale evidence")

	application.Send(vaxis.Key{Text: "r", Keycode: 'r'})
	for range 100 {
		dispatch.flush()
		application.Pump(80, 12)
		if warnings == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if warnings != 1 {
		t.Fatalf("warnings = %d, want 1", warnings)
	}
	if text := application.Text(); !strings.Contains(text, "stale evidence") {
		t.Fatalf("failed refresh did not preserve stale diff:\n%s", text)
	}
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
	for _, expected := range []string{"internal/app.go", "1 changed file", "+2 −2", "◆ -2,2 +2,2", "+ old value", "− old next", "+ new value", "+ next value", "[ ] files", "{ } hunks"} {
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

func TestWorkspaceDiffChangedLinesPreserveSyntaxForeground(t *testing.T) {
	lineNumber := 1
	syntaxColor := vaxis.IndexColor(123)
	theme := ui.DefaultTheme()
	semantic := semanticFallback(theme)
	syntax := []ui.TextSpan{{Text: "comment", Style: ui.Style{Foreground: syntaxColor}}}
	unified := uitest.New(workspaceDiffLineWidget(
		protocol.DiffLine{Kind: "addition", NewLine: &lineNumber, Content: "comment", HasTerminatingLF: true},
		syntax, false, false, false, theme, semantic, nil, nil, nil,
	))
	unified.Pump(40, 2)
	if got := unified.Cell(14, 0).Style.Foreground; got != syntaxColor {
		t.Fatalf("unified changed-line foreground = %v, want syntax foreground %v", got, syntaxColor)
	}
	if got := unified.Cell(10, 0).Style.Foreground; got != theme.Foreground {
		t.Fatalf("unified changed-line number foreground = %v, want readable foreground %v", got, theme.Foreground)
	}
	if got := unified.Cell(39, 0).Style.Background; got != semantic.Token(kittheme.TokenDiffAddedContentBackground) {
		t.Fatalf("unified trailing background = %v, want full-width diff background", got)
	}
	item := &workspaceDiffRenderedLine{line: protocol.DiffLine{Kind: "addition", NewLine: &lineNumber, Content: "comment", HasTerminatingLF: true}, syntax: syntax}
	split := uitest.New(workspaceDiffSideWidget(item, false, false, true, 1, false, 0, theme, semantic, nil, nil, nil))
	split.Pump(40, 2)
	if got := split.Cell(8, 0).Style.Foreground; got != syntaxColor {
		t.Fatalf("split changed-line foreground = %v, want syntax foreground %v", got, syntaxColor)
	}
	if got := split.Cell(4, 0).Style.Foreground; got != theme.Foreground {
		t.Fatalf("split changed-line number foreground = %v, want readable foreground %v", got, theme.Foreground)
	}
	if got := split.Cell(39, 0).Style.Background; got != semantic.Token(kittheme.TokenDiffAddedContentBackground) {
		t.Fatalf("split trailing background = %v, want full-width diff background", got)
	}
}

func TestWorkspaceDiffUnifiedRangeStylesOnlyAnchoredSide(t *testing.T) {
	oldLine, newLine := 12, 12
	application := uitest.New(workspaceDiffLineWidget(
		protocol.DiffLine{Kind: "context", OldLine: &oldLine, NewLine: &newLine, Content: "same", HasTerminatingLF: true},
		nil, false, true, false, ui.DefaultTheme(), semanticFallback(ui.DefaultTheme()), nil, nil, nil,
	))
	application.Pump(40, 2)
	if application.Cell(4, 0).Style.Foreground != ui.DefaultTheme().Foreground {
		t.Fatalf("old range line number foreground = %v, want readable foreground", application.Cell(4, 0).Style.Foreground)
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
	state := &workspaceDiffFileState{
		pane: &workspaceDiffPaneState{viewportWidth: 120},
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

func TestWorkspaceDiffPollingRequiresVisibleLivePane(t *testing.T) {
	state := &workspaceDiffPaneState{activeTarget: testDiffCatalog().Targets[0]}
	live := workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: fakeWorkingTreeDiff{},
		Presentation: workspacePanePresentation{Active: true, Visible: true},
	}
	if !state.pollEligible(live) {
		t.Fatal("active visible live diff is not polling eligible")
	}
	hidden := live
	hidden.Presentation.Visible = false
	if state.pollEligible(hidden) {
		t.Fatal("hidden diff is polling eligible")
	}
	pinned := live
	pinned.Descriptor.DiffTargetID = testDiffTarget
	state.pinnedEvidence = true
	if state.pollEligible(pinned) {
		t.Fatal("revision-pinned diff is polling eligible")
	}
	state.pinnedEvidence = false
	state.activeTarget = protocol.DiffTargetEntry{Reference: "commit", Kind: protocol.DiffTargetCommit}
	if state.pollEligible(live) {
		t.Fatal("committed target is polling eligible")
	}
}

func TestWorkspaceDiffPanePollsAndDefersRefreshWhileCommenting(t *testing.T) {
	line := 1
	fileA := textDiffFile("live.go", 1, 0)
	observationA := testDiffObservation(fileA)
	pageA := protocol.FileDiffPage{
		Observation: observationA.Observation, File: fileA, Computation: protocol.DiffComputation{State: "complete"},
		Hunks: []protocol.DiffHunk{{OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 1, Lines: []protocol.DiffLine{{Kind: "addition", NewLine: &line, Content: "first revision", HasTerminatingLF: true}}}},
	}
	fileB := fileA
	fileB.FileRevision = "diff_file_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	observationB := testDiffObservation(fileB)
	observationB.Observation.Revision = "diffrev_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	pageB := protocol.FileDiffPage{
		Observation: observationB.Observation, File: fileB, Computation: protocol.DiffComputation{State: "complete"},
		Hunks: []protocol.DiffHunk{{OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 1, Lines: []protocol.DiffLine{{Kind: "addition", NewLine: &line, Content: "second revision", HasTerminatingLF: true}}}},
	}
	backend := &pollingWorkingTreeDiff{observation: observationA, pages: map[string]protocol.FileDiffPage{observationA.Observation.Revision: pageA}}
	dispatch := &queuedDiffDispatch{}
	var saveDone func(error)
	model := &diffPanePresentationModel{active: true, pane: workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch,
		RefreshInterval:    5 * time.Millisecond,
		OnCreateAnnotation: func(_ protocol.AnnotationAnchor, _ string, done func(error)) { saveDone = done },
	}}
	application := uitest.New(diffPanePresentationHarness{model: model})
	pumpDiffUntil(t, application, dispatch, 160, 14, "first revision")
	application.Send(vaxis.Key{Text: "c", Keycode: 'c'})
	application.Pump(160, 14)
	backend.set(observationB, pageB)
	rows := pumpDiffUntil(t, application, dispatch, 160, 14, "Changes available")
	if !strings.Contains(strings.Join(rows, "\n"), "first revision") || strings.Contains(strings.Join(rows, "\n"), "second revision") {
		t.Fatalf("poll replaced diff while comment editor was active:\n%s", strings.Join(rows, "\n"))
	}
	calls := backend.observeCalls()
	for range 20 {
		time.Sleep(time.Millisecond)
		dispatch.flush()
		application.Pump(160, 14)
	}
	if got := backend.observeCalls(); got != calls {
		t.Fatalf("polls while changed revision was deferred = %d, want %d", got, calls)
	}
	application.Send(vaxis.Key{Text: "x", Keycode: 'x'})
	application.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	application.Pump(160, 14)
	if saveDone == nil {
		t.Fatal("annotation save did not start")
	}
	model.setActive(false)
	application.Pump(160, 14)
	saveDone(nil)
	application.Pump(160, 14)
	if !strings.Contains(application.Text(), "first revision") {
		t.Fatalf("inactive pane consumed deferred refresh:\n%s", application.Text())
	}
	model.setActive(true)
	pumpDiffUntil(t, application, dispatch, 160, 14, "second revision")
}

func TestWorkspaceDiffSplitNavigationVisitsLogicalLinesOnce(t *testing.T) {
	state := workspaceDiffFileState{
		pane: &workspaceDiffPaneState{viewportWidth: workspaceDiffSplitBreakpoint},
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
	state := workspaceDiffFileState{
		pane:  &workspaceDiffPaneState{viewportWidth: workspaceDiffSplitBreakpoint},
		hunks: []protocol.DiffHunk{{Lines: []protocol.DiffLine{{Kind: "addition"}, {Kind: "deletion"}}}},
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
	rows := pumpDiffUntil(t, application, dispatch, 80, 13, "range note")
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
			if body != "Gwalk" {
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
	application.Send(vaxis.Key{Keycode: 'g', Text: "G", Modifiers: vaxis.ModShift})
	application.Pump(120, 14)
	if application.Contains("Select diff target") {
		t.Fatalf("typing G in the annotation editor opened the target picker:\n%s", application.Text())
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

func TestWorkspaceDiffPaneLoadsAllChangedFilePagesIntoOneDocument(t *testing.T) {
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
	rows := pumpDiffUntil(t, application, dispatch, 80, 12, "second.go")
	if !strings.Contains(strings.Join(rows, "\n"), "first.go") {
		t.Fatalf("first changed file was not selected:\n%s", strings.Join(rows, "\n"))
	}
	application.Send(vaxis.Key{Text: "]", Keycode: ']'})
	rows = pumpDiffUntil(t, application, dispatch, 80, 12, "second.go")
	if !strings.Contains(strings.Join(rows, "\n"), "2 changed files") {
		t.Fatalf("second changed-file page was not available to cycling:\n%s", strings.Join(rows, "\n"))
	}
}

func TestWorkspaceDiffTargetPickerPresentsSelectionDraftsAndFiltering(t *testing.T) {
	catalog := testDiffCatalog()
	catalog.Targets = append(catalog.Targets, protocol.DiffTargetEntry{
		Reference: "commit-head", TargetID: "difftarget_commit", Kind: protocol.DiffTargetCommit,
		Head:     protocol.DiffEndpoint{Kind: "commit", OID: strings.Repeat("a", 40)},
		Metadata: protocol.DiffTargetMetadata{Label: "a1b2c3d  Fix parser bounds", Subject: "Fix parser bounds", Abbreviated: "a1b2c3d"},
	})
	backend := fakeWorkingTreeDiff{catalog: catalog, observation: testDiffObservation()}
	dispatch := &queuedDiffDispatch{}
	annotation := func(id uint64) protocol.AnnotationSummary {
		return protocol.AnnotationSummary{ID: id, Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &protocol.WorkingTreeDiffAnnotationAnchor{TargetID: "difftarget_commit"}}}
	}
	application := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}, Annotations: []protocol.AnnotationSummary{annotation(1), annotation(2)}})
	pumpDiffUntil(t, application, dispatch, 100, 26, "No changes for Working tree")
	application.Send(vaxis.Key{Text: "G", Keycode: 'g', Modifiers: vaxis.ModShift})
	application.Pump(100, 26)
	text := application.Text()
	for _, expected := range []string{"Select diff target", "✓ Working tree", "a1b2c3d  Fix parser bounds", glyphCircleFilled + " 2", "Filter branch, subject, or object ID", "↑↓ move · enter select · esc close"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("target picker missing %q:\n%s", expected, text)
		}
	}
	application.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	application.Pump(100, 26)
	if strings.Contains(application.Text(), "Select diff target") {
		t.Fatal("selecting the active target did not close the picker")
	}
	application.Send(vaxis.Key{Text: "G", Keycode: 'g', Modifiers: vaxis.ModShift})
	application.Pump(100, 26)
	for _, character := range "parser" {
		application.Send(vaxis.Key{Text: string(character), Keycode: character})
	}
	application.Pump(100, 26)
	text = application.Text()
	if !strings.Contains(text, "Fix parser bounds") || strings.Contains(text, "✓ Working tree") {
		t.Fatalf("filtered picker presentation is incoherent:\n%s", text)
	}
}

func TestWorkspaceDiffLoadingMessageIsCentered(t *testing.T) {
	application := uitest.New(centeredWorkspaceDiffLoading(90, "Loading diff…", ui.Style{}))
	application.Pump(90, 25)
	rows := paintedRows(application, 90, 25)
	column, row := findTextCell(t, rows, "Loading diff…")
	if want := (90-len([]rune("⠋ Loading diff…")))/2 + 2; column != want {
		t.Fatalf("loading label column = %d, want %d:\n%s", column, want, strings.Join(rows, "\n"))
	}
	if row != 12 {
		t.Fatalf("loading label row = %d, want 12", row)
	}
}

func TestWorkspaceDiffTargetPickerShowsLoadingErrorAndEmptyStates(t *testing.T) {
	t.Run("loading", func(t *testing.T) {
		wait := make(chan struct{})
		dispatch := &queuedDiffDispatch{}
		application := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: fakeWorkingTreeDiff{catalogWait: wait}, Dispatch: dispatch.dispatch,
			Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
		application.Pump(90, 25)
		application.Send(vaxis.Key{Text: "G", Keycode: 'g', Modifiers: vaxis.ModShift})
		application.Pump(90, 25)
		if text := application.Text(); !strings.Contains(text, "Loading diff targets") {
			t.Fatalf("loading picker:\n%s", text)
		}
		close(wait)
	})
	t.Run("error", func(t *testing.T) {
		dispatch := &queuedDiffDispatch{}
		application := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: fakeWorkingTreeDiff{catalogErr: errors.New("catalog unavailable")}, Dispatch: dispatch.dispatch,
			Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
		pumpDiffUntil(t, application, dispatch, 90, 25, "Could not list diff targets")
		application.Send(vaxis.Key{Text: "G", Keycode: 'g', Modifiers: vaxis.ModShift})
		text := strings.Join(pumpDiffUntil(t, application, dispatch, 90, 25, glyphCross+" Could not load diff"), "\n")
		if !strings.Contains(text, glyphCross+" Could not load diff") {
			t.Fatalf("error picker:\n%s", text)
		}
	})
	t.Run("empty", func(t *testing.T) {
		dispatch := &queuedDiffDispatch{}
		application := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: fakeWorkingTreeDiff{catalogEmpty: true}, Dispatch: dispatch.dispatch,
			Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
		pumpDiffUntil(t, application, dispatch, 90, 25, "No diff targets available")
		application.Send(vaxis.Key{Text: "G", Keycode: 'g', Modifiers: vaxis.ModShift})
		text := strings.Join(pumpDiffUntil(t, application, dispatch, 90, 25, "No matching targets"), "\n")
		if !strings.Contains(text, "No matching targets") {
			t.Fatalf("empty picker:\n%s", text)
		}
	})
}

func TestWorkspaceDiffToggleSwitchesInPlaceAndPreservesPath(t *testing.T) {
	oid := strings.Repeat("a", 40)
	catalog := testDiffCatalog()
	catalog.Targets[0].Head = protocol.DiffEndpoint{Kind: "commit", OID: oid}
	commitTarget := protocol.DiffTargetEntry{Reference: "commit-head", TargetID: "difftarget_commit", Kind: protocol.DiffTargetCommit,
		Head: protocol.DiffEndpoint{Kind: "commit", OID: oid}, Metadata: protocol.DiffTargetMetadata{Label: "a1b2c3d  Fix parser bounds", Subject: "Fix parser bounds", Abbreviated: "a1b2c3d"}}
	catalog.Targets = append(catalog.Targets, commitTarget)
	file := textDiffFile("shared.go", 1, 0)
	working := testDiffObservation(file)
	commit := testDiffObservation(file)
	commit.Observation.Target.ID, commit.Observation.Target.Kind, commit.Observation.Revision = commitTarget.TargetID, protocol.DiffTargetCommit, "diffrev_commit"
	line := 1
	workingFile := protocol.FileDiffPage{Observation: working.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"}, Hunks: []protocol.DiffHunk{{Lines: []protocol.DiffLine{{Kind: "addition", NewLine: &line, Content: "working evidence", HasTerminatingLF: true}}}}}
	commitFile := workingFile
	commitFile.Observation = commit.Observation
	commitFile.Hunks = []protocol.DiffHunk{{Lines: []protocol.DiffLine{{Kind: "addition", NewLine: &line, Content: "committed evidence", HasTerminatingLF: true}}}}
	releaseCommit, commitStarted := make(chan struct{}), make(chan struct{})
	backend := fakeWorkingTreeDiff{catalog: catalog, observation: working, observeFn: func(ctx context.Context, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
		if input.TargetReference != commitTarget.Reference {
			return working, nil
		}
		close(commitStarted)
		select {
		case <-releaseCommit:
			return commit, nil
		case <-ctx.Done():
			return protocol.DiffPage{}, ctx.Err()
		}
	}, pages: map[string]protocol.FileDiffPage{
		testDiffTarget + "\x00shared.go": workingFile, commitTarget.TargetID + "\x00shared.go": commitFile,
	}}
	dispatch := &queuedDiffDispatch{}
	notice := ""
	var followCWD []bool
	pane := workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}, OnNotice: func(message string) { notice = message },
		OnFollowCWDChanged: func(follow bool) { followCWD = append(followCWD, follow) }}
	theme := ui.DefaultTheme()
	application := uitest.New(ui.Provider[ui.Theme]{Value: theme, Child: pane})
	pumpDiffUntil(t, application, dispatch, 100, 14, "working evidence")
	if len(followCWD) == 0 || !followCWD[len(followCWD)-1] {
		t.Fatalf("working-tree follow state = %v, want live", followCWD)
	}
	application.Send(vaxis.Key{Text: "g", Keycode: 'g'})
	select {
	case <-commitStarted:
	case <-time.After(time.Second):
		t.Fatal("committed target observation did not start")
	}
	application.Pump(100, 14)
	if len(followCWD) == 0 || followCWD[len(followCWD)-1] {
		t.Fatalf("committed-target follow state = %v, want pinned", followCWD)
	}
	if notice != "Working-tree changes are not included in this committed target" {
		t.Fatalf("notice = %q", notice)
	}
	if text := application.Text(); !strings.Contains(text, "a1b2c3d  Fix parser bounds") || !strings.Contains(text, "Switching target…") || !strings.Contains(text, "Loading diff…") || strings.Contains(text, "working evidence") {
		t.Fatalf("switch did not enter its loading state immediately:\n%s", text)
	}
	close(releaseCommit)
	rows := pumpDiffUntil(t, application, dispatch, 100, 14, "committed evidence")
	text := strings.Join(rows, "\n")
	for _, expected := range []string{"shared.go", "a1b2c3d  Fix parser bounds", "committed evidence"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("switched target missing %q:\n%s", expected, text)
		}
	}
	header := rows[0]
	position, revision := strings.Index(header, "1 changed file"), strings.Index(header, "a1b2c3d  Fix parser bounds")
	if revision < 0 || position <= revision || !strings.HasPrefix(strings.TrimSpace(header), "a1b2c3d  Fix parser bounds") || !strings.HasSuffix(strings.TrimSpace(header), "Unified") {
		t.Fatalf("diff header hierarchy is incorrect: %q", header)
	}

	revisionColumn, headerRow := findRenderedDiffText(t, application, 100, 14, "a1b2c3d  Fix parser bounds")
	if revisionColumn != 1 || headerRow != 0 {
		t.Fatalf("revision position = (%d,%d), want (1,0)", revisionColumn, headerRow)
	}
	countColumn, _ := findRenderedDiffText(t, application, 100, 14, "1 changed file")
	layoutColumn, _ := findRenderedDiffText(t, application, 100, 14, "Unified")
	wantCountColumn := revisionColumn + len("a1b2c3d  Fix parser bounds") + 2
	if countColumn != wantCountColumn || layoutColumn != 92 {
		t.Fatalf("header count/layout columns = %d/%d, want %d/92", countColumn, layoutColumn, wantCountColumn)
	}

	pathColumn, pathRow := findRenderedDiffText(t, application, 100, 14, "shared.go")
	baseBackground := application.Cell(revisionColumn, headerRow).Style.Background
	application.Send(vaxis.Mouse{Col: revisionColumn, Row: headerRow, EventType: vaxis.EventMotion})
	application.Pump(100, 14)
	if application.Cell(revisionColumn, headerRow).Style.Background == baseBackground {
		t.Fatal("revision control did not show its hover surface")
	}
	application.Send(vaxis.Mouse{Col: pathColumn, Row: pathRow, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	application.Pump(100, 14)
	if strings.Contains(application.Text(), "Select diff target") {
		t.Fatal("clicking the file portion of the header opened the target picker")
	}
	application.Send(vaxis.Mouse{Col: revisionColumn, Row: headerRow, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	application.Pump(100, 14)
	if !strings.Contains(application.Text(), "Select diff target") {
		t.Fatal("clicking the revision control did not open the target picker")
	}
}

func TestWorkspaceDiffTargetSwitchPublishesFirstCoherentPage(t *testing.T) {
	commitTarget := protocol.DiffTargetEntry{Reference: "paged-target", TargetID: "difftarget_paged", Kind: protocol.DiffTargetCommit, Metadata: protocol.DiffTargetMetadata{Label: "ccccccc  Paged"}}
	oldFile, firstFile, secondFile := textDiffFile("old.go", 1, 0), textDiffFile("first.go", 1, 0), textDiffFile("second.go", 1, 0)
	working := testDiffObservation(oldFile)
	first := testDiffObservation(firstFile)
	first.Observation.Target.ID, first.Observation.Target.Kind, first.Observation.Revision, first.NextCursor = commitTarget.TargetID, protocol.DiffTargetCommit, "diffrev_paged", "next"
	second := protocol.WorkingTreePage{Observation: first.Observation, Files: []protocol.DiffFileSummary{secondFile}}
	line := 1
	releaseNext, nextStarted := make(chan struct{}), make(chan struct{})
	backend := fakeWorkingTreeDiff{observation: working, observeFn: func(_ context.Context, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
		if input.TargetReference != commitTarget.Reference {
			return working, nil
		}
		if input.Cursor == "" {
			return first, nil
		}
		if input.ExpectedTargetRevision != first.Observation.Revision {
			return protocol.DiffPage{}, fmt.Errorf("continuation revision = %q, want %q", input.ExpectedTargetRevision, first.Observation.Revision)
		}
		close(nextStarted)
		<-releaseNext
		return second, nil
	}, pages: map[string]protocol.FileDiffPage{
		"old.go":                               {Observation: working.Observation, File: oldFile, Computation: protocol.DiffComputation{State: "complete"}, Hunks: []protocol.DiffHunk{{Lines: []protocol.DiffLine{{Kind: "addition", NewLine: &line, Content: "old presentation", HasTerminatingLF: true}}}}},
		commitTarget.TargetID + "\x00first.go": {Observation: first.Observation, File: firstFile, Computation: protocol.DiffComputation{State: "complete"}},
	}}
	dispatch := &queuedDiffDispatch{}
	state := &workspaceDiffPaneState{}
	application := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch, testState: state,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
	pumpDiffUntil(t, application, dispatch, 100, 14, "old presentation")
	state.SetState(func() { state.switchTarget(commitTarget) })
	for {
		dispatch.flush()
		application.Pump(100, 14)
		select {
		case <-nextStarted:
			goto firstPageReady
		default:
			time.Sleep(time.Millisecond)
		}
	}
firstPageReady:
	text := application.Text()
	if !strings.Contains(text, "ccccccc  Paged") || !strings.Contains(text, "Scroll to load diff") || strings.Contains(text, "old presentation") {
		t.Fatalf("first coherent page was not published atomically:\n%s", text)
	}
	rows := paintedRows(application, 100, 14)
	_, fileRow := findTextCell(t, rows, "first.go")
	_, loadingRow := findTextCell(t, rows, "Scroll to load diff")
	if loadingRow != fileRow+2 {
		t.Fatalf("loading state should belong to its file section:\n%s", strings.Join(rows, "\n"))
	}

	close(releaseNext)
	pumpDiffUntil(t, application, dispatch, 100, 14, "first.go")
}

func TestWorkspaceDiffTargetSwitchIgnoresOutOfOrderCompletion(t *testing.T) {
	workingTarget := testDiffCatalog().Targets[0]
	targetA := protocol.DiffTargetEntry{Reference: "target-a", TargetID: "difftarget_a", Kind: protocol.DiffTargetCommit, Metadata: protocol.DiffTargetMetadata{Label: "aaaaaaa  Older"}}
	targetB := protocol.DiffTargetEntry{Reference: "target-b", TargetID: "difftarget_b", Kind: protocol.DiffTargetCommit, Metadata: protocol.DiffTargetMetadata{Label: "bbbbbbb  Winner"}}
	catalog := testDiffCatalog()
	catalog.Targets = append(catalog.Targets, targetA, targetB)
	fileA, fileB := textDiffFile("a.go", 1, 0), textDiffFile("b.go", 1, 0)
	pageA, pageB := testDiffObservation(fileA), testDiffObservation(fileB)
	pageA.Observation.Target.ID, pageA.Observation.Target.Kind, pageA.Observation.Revision = targetA.TargetID, protocol.DiffTargetCommit, "diffrev_a"
	pageB.Observation.Target.ID, pageB.Observation.Target.Kind, pageB.Observation.Revision = targetB.TargetID, protocol.DiffTargetCommit, "diffrev_b"
	startedA, releaseA := make(chan struct{}), make(chan struct{})
	backend := fakeWorkingTreeDiff{catalog: catalog, observation: testDiffObservation(), observeFn: func(_ context.Context, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
		switch input.TargetReference {
		case workingTarget.Reference:
			return testDiffObservation(), nil
		case targetA.Reference:
			close(startedA)
			<-releaseA
			return pageA, nil
		default:
			return pageB, nil
		}
	}, pages: map[string]protocol.FileDiffPage{targetB.TargetID + "\x00b.go": {Observation: pageB.Observation, File: fileB, Computation: protocol.DiffComputation{State: "complete"}}}}
	dispatch := &queuedDiffDispatch{}
	state := &workspaceDiffPaneState{}
	application := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch, testState: state,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
	pumpDiffUntil(t, application, dispatch, 90, 14, "No changes for Working tree")
	state.SetState(func() { state.switchTarget(targetA) })
	<-startedA
	state.SetState(func() { state.switchTarget(targetB) })
	pumpDiffUntil(t, application, dispatch, 90, 14, "bbbbbbb  Winner")
	close(releaseA)
	time.Sleep(time.Millisecond)
	dispatch.flush()
	application.Pump(90, 14)
	text := application.Text()
	if !strings.Contains(text, "bbbbbbb  Winner") || strings.Contains(text, "aaaaaaa  Older") || strings.Contains(text, "a.go") {
		t.Fatalf("stale target completion replaced or mixed presentation:\n%s", text)
	}
}

func TestWorkspaceDiffAnnotationActivationReplacesPriorTargetExactly(t *testing.T) {
	oid := strings.Repeat("b", 40)
	catalog := testDiffCatalog()
	commitTarget := protocol.DiffTargetEntry{Reference: "commit-pinned", TargetID: "difftarget_pinned", Kind: protocol.DiffTargetCommit,
		Head: protocol.DiffEndpoint{Kind: "commit", OID: oid}, Metadata: protocol.DiffTargetMetadata{Label: "b1b2b3b  Pinned review", Subject: "Pinned review", Abbreviated: "b1b2b3b"}}
	catalog.Targets = append(catalog.Targets, commitTarget)
	file := textDiffFile("pinned.go", 1, 0)
	line := 1
	working := testDiffObservation(file)
	workingPage := protocol.FileDiffPage{Observation: working.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"}, Hunks: []protocol.DiffHunk{{Lines: []protocol.DiffLine{{Kind: "addition", NewLine: &line, Content: "live evidence", HasTerminatingLF: true}}}}}
	commitObservation := working.Observation
	commitObservation.Target.ID = commitTarget.TargetID
	commitObservation.Target.Kind = protocol.DiffTargetCommit
	commitObservation.Revision = "diffrev_pinned"
	commitPage := workingPage
	commitPage.Observation = commitObservation
	commitPage.Hunks = []protocol.DiffHunk{{Lines: []protocol.DiffLine{{Kind: "addition", NewLine: &line, Content: "exact pinned evidence", HasTerminatingLF: true}}}}
	pages := map[string]protocol.FileDiffPage{testDiffTarget + "\x00pinned.go": workingPage, commitTarget.TargetID + "\x00pinned.go": commitPage}
	var readMu sync.Mutex
	var lastRead protocol.ReadFileDiffInput
	backend := fakeWorkingTreeDiff{catalog: catalog, observation: working, pages: pages, readFn: func(_ context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
		readMu.Lock()
		lastRead = input
		readMu.Unlock()
		return pages[input.TargetID+"\x00"+input.Path], nil
	}}
	dispatch := &queuedDiffDispatch{}
	pane := workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch}
	model := &diffPanePresentationModel{pane: pane, active: true}
	application := uitest.New(diffPanePresentationHarness{model: model})
	pumpDiffUntil(t, application, dispatch, 100, 14, "live evidence")
	descriptor := workingTreeDiffWorkspacePane(testDiffWorkspace)
	descriptor.OpenGeneration = 2
	descriptor.Path = file.Path
	descriptor.DiffTargetID = commitTarget.TargetID
	descriptor.ExpectedRevision = commitObservation.Revision
	descriptor.ExpectedFileRevision = file.FileRevision
	descriptor.AnnotationID = 42
	pane.Descriptor = descriptor
	model.setPane(pane)
	rows := pumpDiffUntil(t, application, dispatch, 100, 14, "exact pinned evidence")
	if text := strings.Join(rows, "\n"); !strings.Contains(text, "b1b2b3b  Pinned review") {
		t.Fatalf("annotation target crumb was not restored:\n%s", text)
	}
	readMu.Lock()
	activationRead := lastRead
	readMu.Unlock()
	if activationRead.AnnotationID != 42 {
		t.Fatalf("activation annotation id = %d", activationRead.AnnotationID)
	}
	application.Send(vaxis.Key{Text: "G", Keycode: 'g', Modifiers: vaxis.ModShift})
	application.Pump(100, 25)
	if text := application.Text(); !strings.Contains(text, glyphCheck+" b1b2b3b  Pinned review") {
		t.Fatalf("annotation target was not selected in picker:\n%s", text)
	}
	application.Send(vaxis.Key{Keycode: vaxis.KeyUp})
	application.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	pumpDiffUntil(t, application, dispatch, 100, 14, "live evidence")
	if strings.Contains(application.Text(), "Select diff target") {
		t.Fatal("switching pinned evidence back to the live target left the picker open")
	}
}

func TestWorkspaceDiffTargetSwitchBlockedByRangeWithWarning(t *testing.T) {
	file := textDiffFile("range.go", 1, 0)
	observation := testDiffObservation(file)
	line := 1
	backend := fakeWorkingTreeDiff{observation: observation, pages: map[string]protocol.FileDiffPage{"range.go": {Observation: observation.Observation, File: file, Computation: protocol.DiffComputation{State: "complete"}, Hunks: []protocol.DiffHunk{{Lines: []protocol.DiffLine{{Kind: "addition", NewLine: &line, Content: "selected", HasTerminatingLF: true}}}}}}}
	dispatch := &queuedDiffDispatch{}
	warning := ""
	var followCWD []bool
	application := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}, OnCreateAnnotation: func(protocol.AnnotationAnchor, string, func(error)) {}, OnWarning: func(message string) { warning = message },
		OnFollowCWDChanged: func(follow bool) { followCWD = append(followCWD, follow) }})
	pumpDiffUntil(t, application, dispatch, 90, 14, "selected")
	application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	application.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	if len(followCWD) == 0 || followCWD[len(followCWD)-1] {
		t.Fatalf("range selection follow state = %v, want pinned", followCWD)
	}
	application.Send(vaxis.Key{Text: "G", Keycode: 'g', Modifiers: vaxis.ModShift})
	application.Pump(90, 14)
	if warning != "Finish the active range or comment before changing target" {
		t.Fatalf("warning = %q", warning)
	}
	application.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	if len(followCWD) == 0 || !followCWD[len(followCWD)-1] {
		t.Fatalf("cleared range follow state = %v, want live", followCWD)
	}
	if strings.Contains(application.Text(), "Select diff target") {
		t.Fatalf("blocked switch opened picker:\n%s", application.Text())
	}
}

func TestWorkspaceDiffTargetFilteringMatchesAllCatalogMetadata(t *testing.T) {
	state := workspaceDiffPaneState{catalog: []protocol.DiffTargetEntry{
		{Metadata: protocol.DiffTargetMetadata{Label: "Working tree"}},
		{Metadata: protocol.DiffTargetMetadata{RefName: "feature/review", BaseRefName: "main", Subject: "Parser bounds", Abbreviated: "abcdef1"}},
	}}
	for _, query := range []string{"feature", "MAIN", "parser", "abcdef1"} {
		state.targetQuery = query
		if got := state.filteredTargets(); len(got) != 1 || got[0].Metadata.RefName != "feature/review" {
			t.Fatalf("query %q = %+v", query, got)
		}
	}
	state.targetQuery = "missing"
	if got := state.filteredTargets(); len(got) != 0 {
		t.Fatalf("missing query = %+v", got)
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

func TestFrozenDiffCannotSwitchTargetsOrFollowOldCatalog(t *testing.T) {
	state := &workspaceDiffPaneState{}
	warning := ""
	var followed []bool
	pane := workspaceDiffPane{
		Descriptor: workingTreeDiffWorkspacePane("old"), CurrentWorkspaceID: "new", testState: state,
		Presentation:       workspacePanePresentation{Active: true, Visible: true, Focused: true},
		OnWarning:          func(message string) { warning = message },
		OnFollowCWDChanged: func(follow bool) { followed = append(followed, follow) },
	}
	application := uitest.New(pane)
	application.Pump(90, 20)
	state.switchTarget(protocol.DiffTargetEntry{Reference: "old-working-tree", TargetID: "old-target", Kind: protocol.DiffTargetWorkingTree})
	if warning != "This Diff belongs to a previous workspace; open Diff for the current workspace" || state.pendingTarget.Reference != "" || len(followed) != 0 {
		t.Fatalf("frozen target switch: warning=%q pending=%+v follow=%v", warning, state.pendingTarget, followed)
	}
}
