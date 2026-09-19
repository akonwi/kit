package tui

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func documentFixture(paths ...string) fakeWorkingTreeDiff {
	backend := fakeWorkingTreeDiff{pages: map[string]protocol.FileDiffPage{}}
	for _, path := range paths {
		backend.observation.Files = append(backend.observation.Files, textDiffFile(path, 1, 0))
	}
	backend.observation.Observation = testDiffObservation().Observation
	for _, file := range backend.observation.Files {
		line := 1
		backend.pages[file.Path] = protocol.FileDiffPage{Observation: backend.observation.Observation, File: file,
			Computation: protocol.DiffComputation{State: "complete"}, Hunks: []protocol.DiffHunk{{NewStart: 1, NewCount: 1, Lines: []protocol.DiffLine{
				{Kind: "addition", NewLine: &line, Content: "content of " + file.Path, HasTerminatingLF: true},
			}}}}
	}
	return backend
}

// uitest.Pump does not run the runner's post-layout frame callbacks.
func pumpDocument(t *testing.T, app *uitest.App, dispatch *queuedDiffDispatch, state *workspaceDiffPaneState, width, height int, ready func() bool) []string {
	t.Helper()
	for range 200 {
		dispatch.flush()
		app.Pump(width, height)
		state.TickFrame(time.Now())
		app.Pump(width, height)
		if ready() {
			return paintedRows(app, width, height)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("document did not settle:\n%s", strings.Join(paintedRows(app, width, height), "\n"))
	return nil
}

func TestWorkspaceDiffContinuousDocumentRowsAndNavigation(t *testing.T) {
	for _, width := range []int{80, 140} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			state := &workspaceDiffPaneState{}
			dispatch := &queuedDiffDispatch{}
			app := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: documentFixture("first.go", "second.go"),
				Dispatch: dispatch.dispatch, testState: state, Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
			rows := pumpDocument(t, app, dispatch, state, width, 18, func() bool { return len(state.sections) == 2 && state.sections[0].loaded && state.sections[1].loaded })
			// Both files and both hunks are visible without any file-navigation command.
			for row, wanted := range map[int]string{2: "first.go  +1 −0", 4: "◆ -0,0 +1,1", 5: "content of first.go", 7: "second.go  +1 −0", 9: "◆ -0,0 +1,1", 10: "content of second.go"} {
				if !strings.Contains(rows[row], wanted) {
					t.Fatalf("row %d = %q, want %q", row, rows[row], wanted)
				}
			}
			app.Key("j")
			app.Pump(width, 18)
			if state.selectedFile != 1 || state.active.cursorRow != 1 {
				t.Fatalf("down across files = file %d, row %d", state.selectedFile, state.active.cursorRow)
			}
			app.Key("k")
			app.Pump(width, 18)
			if state.selectedFile != 0 || state.active.cursorRow != 1 {
				t.Fatalf("up across files = file %d, row %d", state.selectedFile, state.active.cursorRow)
			}
			app.Key("}")
			app.Pump(width, 18)
			if state.selectedFile != 1 {
				t.Fatalf("next hunk stayed in file %d", state.selectedFile)
			}
			app.Key("[")
			pumpDocument(t, app, dispatch, state, width, 18, func() bool { return !state.cursorRevealPending })
			if state.selectedFile != 0 || !state.sections[1].loaded {
				t.Fatal("file jump discarded the other section")
			}
			state.Dispose()
		})
	}
}

func TestWorkspaceDiffDocumentCommentsUseClickedFile(t *testing.T) {
	state := &workspaceDiffPaneState{}
	dispatch := &queuedDiffDispatch{}
	backend := documentFixture("first.go", "second.go")
	var saved protocol.AnnotationAnchor
	app := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend,
		Dispatch: dispatch.dispatch, testState: state, Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true},
		OnCreateAnnotation: func(anchor protocol.AnnotationAnchor, _ string, done func(error)) { saved = anchor; done(nil) },
	})
	pumpDocument(t, app, dispatch, state, 80, 24, func() bool { return len(state.sections) == 2 && state.sections[1].loaded })
	col, row := findRenderedDiffText(t, app, 80, 24, "content of second.go")
	app.Send(vaxis.Mouse{Col: col, Row: row, EventType: vaxis.EventMotion})
	app.Pump(80, 24)
	app.Key("c")
	app.Pump(80, 24)
	if !state.active.commenting || state.active.commentAnchor.Path != "second.go" {
		t.Fatalf("editor anchor = %+v", state.active.commentAnchor)
	}
	// Hovering the other file must not transfer the editor or its range.
	col, row = findRenderedDiffText(t, app, 80, 24, "content of first.go")
	app.Send(vaxis.Mouse{Col: col, Row: row, EventType: vaxis.EventMotion})
	app.Pump(80, 24)
	if state.selectedFile != 1 {
		t.Fatal("hover transferred the active editor to another file")
	}
	state.active.submitComment(state.Widget().(workspaceDiffPane), "second file note")
	app.Pump(80, 24)
	if saved.WorkingTreeDiff == nil || saved.WorkingTreeDiff.Path != "second.go" || saved.WorkingTreeDiff.TargetRevision != testDiffRevision || saved.WorkingTreeDiff.StartLine != 1 {
		t.Fatalf("saved anchor = %+v", saved)
	}
	state.Dispose()
}

func TestWorkspaceDiffDocumentBoundsConcurrentReadsAndIgnoresCanceledResults(t *testing.T) {
	backend := documentFixture("a.go", "b.go", "c.go", "d.go")
	releases := make(chan struct{})
	var running, peak, calls atomic.Int32
	backend.readFn = func(ctx context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
		count := running.Add(1)
		defer running.Add(-1)
		for old := peak.Load(); count > old && !peak.CompareAndSwap(old, count); old = peak.Load() {
		}
		calls.Add(1)
		<-releases // deliberately ignore cancellation to exercise the completion guard
		return backend.pages[input.Path], nil
	}
	state := &workspaceDiffPaneState{}
	dispatch := &queuedDiffDispatch{}
	model := &diffPanePresentationModel{active: true, pane: workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch, testState: state}}
	app := uitest.New(diffPanePresentationHarness{model: model})
	pumpDocument(t, app, dispatch, state, 80, 30, func() bool { return calls.Load() == workspaceDiffConcurrentWork })
	if peak.Load() != workspaceDiffConcurrentWork {
		t.Fatalf("concurrent requests = %d", peak.Load())
	}
	model.setActive(false)
	app.Pump(80, 30)
	close(releases)
	pumpDocument(t, app, dispatch, state, 80, 30, func() bool { return state.readWork.running == 0 })
	if calls.Load() != workspaceDiffConcurrentWork {
		t.Fatalf("hidden document started queued reads: %d", calls.Load())
	}
	for _, section := range state.sections {
		if section.loaded {
			t.Fatal("canceled completion published content")
		}
	}
	model.setActive(true)
	rows := pumpDocument(t, app, dispatch, state, 80, 30, func() bool { return state.sections[3].loaded })
	for _, path := range []string{"a.go", "b.go", "c.go", "d.go"} {
		if !strings.Contains(strings.Join(rows, "\n"), "content of "+path) {
			t.Fatalf("resumed document missing %s", path)
		}
	}
	if peak.Load() > workspaceDiffConcurrentWork {
		t.Fatalf("concurrency exceeded bound: %d", peak.Load())
	}
	state.Dispose()
}

func TestWorkspaceDiffDocumentPreservesScrollWhenEarlierFileLoads(t *testing.T) {
	backend := documentFixture("first.go", "second.go", "third.go")
	release := make(chan struct{})
	first := backend.pages["first.go"]
	for line := 2; line < 12; line++ {
		first.Hunks[0].Lines = append(first.Hunks[0].Lines, protocol.DiffLine{Kind: "addition", NewLine: &line, Content: fmt.Sprintf("earlier line %d", line), HasTerminatingLF: true})
	}
	backend.readFn = func(ctx context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
		if input.Path == "first.go" {
			select {
			case <-release:
				return first, nil
			case <-ctx.Done():
				return protocol.FileDiffPage{}, ctx.Err()
			}
		}
		return backend.pages[input.Path], nil
	}
	state := &workspaceDiffPaneState{}
	dispatch := &queuedDiffDispatch{}
	app := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch, testState: state, Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
	pumpDocument(t, app, dispatch, state, 80, 10, func() bool { return len(state.sections) == 3 && state.sections[1].loaded && state.sections[2].loaded })
	state.scroll.ScrollTo(0, state.sections[1].offset)
	pumpDocument(t, app, dispatch, state, 80, 10, func() bool { return !state.cursorRevealPending })
	before := state.scroll.Metrics(ui.ScrollVertical).ScrollOffset - state.sections[1].offset
	close(release)
	pumpDocument(t, app, dispatch, state, 80, 10, func() bool { return state.sections[0].loaded && state.scrollCorrection == 0 })
	after := state.scroll.Metrics(ui.ScrollVertical).ScrollOffset - state.sections[1].offset
	if after != before {
		t.Fatalf("viewport moved within second file: before=%d after=%d", before, after)
	}
	_, row := findRenderedDiffText(t, app, 80, 10, "second.go")
	if row != 2 {
		t.Fatalf("second file header row = %d, want 2", row)
	}
	state.Dispose()
}

func TestWorkspaceDiffDocumentContinuationErrorKeepsEvidenceAndRetriesOnce(t *testing.T) {
	backend := documentFixture("first.go", "second.go")
	first := backend.pages["first.go"]
	first.NextCursor = "more"
	retryRelease := make(chan struct{})
	var continuations atomic.Int32
	backend.readFn = func(ctx context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
		if input.Path == "first.go" && input.Cursor == "" {
			return first, nil
		}
		if input.Cursor == "more" {
			if continuations.Add(1) == 1 {
				return protocol.FileDiffPage{}, &protocol.DiffError{Code: "unavailable", Message: "Continuation unavailable"}
			}
			select {
			case <-retryRelease:
			case <-ctx.Done():
				return protocol.FileDiffPage{}, ctx.Err()
			}
			page := first
			page.NextCursor, page.Hunks = "", nil
			return page, nil
		}
		return backend.pages[input.Path], nil
	}
	state := &workspaceDiffPaneState{}
	dispatch := &queuedDiffDispatch{}
	app := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch, testState: state, Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
	rows := pumpDocument(t, app, dispatch, state, 80, 20, func() bool {
		return len(state.sections) == 2 && state.sections[0].errorText != "" && state.sections[1].loaded
	})
	for _, want := range []string{"content of first.go", "content of second.go", "Continuation unavailable · retry"} {
		if !strings.Contains(strings.Join(rows, "\n"), want) {
			t.Fatalf("local failure lost %q:\n%s", want, strings.Join(rows, "\n"))
		}
	}
	col, row := findRenderedDiffText(t, app, 80, 20, "Continuation unavailable")
	app.Click(col, row)
	app.Pump(80, 20)
	app.Click(col, row)
	pumpDocument(t, app, dispatch, state, 80, 20, func() bool { return continuations.Load() == 2 })
	if state.sections[0].errorText != "" || !state.sections[0].loadingMore {
		t.Fatal("retry did not enter loading state")
	}
	close(retryRelease)
	pumpDocument(t, app, dispatch, state, 80, 20, func() bool { return !state.sections[0].loadingMore })
	if continuations.Load() != 2 {
		t.Fatalf("repeated click queued %d continuations", continuations.Load())
	}
	if state.TickFrame(time.Now()) {
		t.Fatal("settled diff kept requesting idle frames")
	}
	state.Dispose()
}

func TestWorkspaceDiffDocumentRefreshBookmarkWaitsForMatchingFile(t *testing.T) {
	backend := documentFixture("first.go", "second.go")
	initial := backend.observation
	nextPage := make(chan struct{})
	var refreshing atomic.Bool
	backend.observeFn = func(ctx context.Context, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
		if !refreshing.Load() {
			return initial, nil
		}
		page := initial
		page.Observation.Revision = "new-revision"
		if input.Cursor == "" {
			page.Files, page.NextCursor = page.Files[:1], "next"
			return page, nil
		}
		select {
		case <-nextPage:
		case <-ctx.Done():
			return protocol.DiffPage{}, ctx.Err()
		}
		page.Files = page.Files[1:]
		return page, nil
	}
	backend.readFn = func(_ context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
		page := backend.pages[input.Path]
		page.Observation.Revision = input.TargetRevision
		line := 42
		page.Hunks[0].Lines = []protocol.DiffLine{{Kind: "addition", NewLine: &line, Content: "line 42 of " + input.Path, HasTerminatingLF: true}}
		return page, nil
	}
	state := &workspaceDiffPaneState{}
	dispatch := &queuedDiffDispatch{}
	app := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch, testState: state, Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
	pumpDocument(t, app, dispatch, state, 80, 18, func() bool { return len(state.sections) == 2 && state.sections[1].loaded })
	state.SetState(func() { state.activateSection(1) })
	refreshing.Store(true)
	state.SetState(func() { state.changesAvailable = true; state.applyAvailableRefresh() })
	pumpDocument(t, app, dispatch, state, 80, 18, func() bool { return state.observation.Revision == "new-revision" && state.sections[0].loaded })
	if state.refreshPath != "second.go" || state.refreshLine != 42 {
		t.Fatalf("first file consumed bookmark: %s:%d", state.refreshPath, state.refreshLine)
	}
	close(nextPage)
	pumpDocument(t, app, dispatch, state, 80, 18, func() bool { return len(state.sections) == 2 && state.sections[1].loaded && state.refreshLine == 0 })
	anchor, ok := state.active.currentDiffAnchor()
	if !ok || anchor.Path != "second.go" || anchor.StartLine != 42 || anchor.TargetRevision != "new-revision" {
		t.Fatalf("refreshed cursor = %+v, %v", anchor, ok)
	}
	state.Dispose()
}

func TestWorkspaceDiffDocumentUnifiedWrapAndLayoutChoice(t *testing.T) {
	backend := documentFixture("first.go", "second.go")
	page := backend.pages["first.go"]
	page.Hunks[0].Lines[0].Content = strings.Repeat("word ", 20) + "TAIL"
	backend.pages["first.go"] = page
	state := &workspaceDiffPaneState{}
	dispatch := &queuedDiffDispatch{}
	app := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch, testState: state, Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
	pumpDocument(t, app, dispatch, state, 80, 24, func() bool { return len(state.sections) == 2 && state.sections[1].loaded })
	app.Key("w")
	pumpDocument(t, app, dispatch, state, 80, 24, func() bool { return !state.cursorRevealPending })
	_, tailRow := findRenderedDiffText(t, app, 80, 24, "TAIL")
	_, secondRow := findRenderedDiffText(t, app, 80, 24, "second.go")
	if tailRow != 6 || secondRow != 8 {
		t.Fatalf("wrapped tail/file rows = %d/%d, want 6/8", tailRow, secondRow)
	}
	pumpDocument(t, app, dispatch, state, 140, 24, func() bool { return !state.cursorRevealPending })
	if !state.splitLayout() {
		t.Fatal("wide viewport did not use split layout")
	}
	app.Key("s")
	pumpDocument(t, app, dispatch, state, 140, 24, func() bool { return !state.cursorRevealPending })
	if state.splitLayout() || !state.unifiedLayout {
		t.Fatal("explicit unified layout not selected")
	}
	_, firstRow := findRenderedDiffText(t, app, 140, 24, "first.go")
	_, secondRow = findRenderedDiffText(t, app, 140, 24, "second.go")
	if firstRow != 2 || secondRow != 7 {
		t.Fatalf("unified sections = rows %d/%d", firstRow, secondRow)
	}
	state.Dispose()
}

func TestWorkspaceDiffDocumentRetainsOffscreenCommentFocus(t *testing.T) {
	paths := make([]string, 12)
	for index := range paths {
		paths[index] = fmt.Sprintf("file-%02d.go", index)
	}
	state := &workspaceDiffPaneState{}
	dispatch := &queuedDiffDispatch{}
	app := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: documentFixture(paths...), Dispatch: dispatch.dispatch, testState: state,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}, OnCreateAnnotation: func(protocol.AnnotationAnchor, string, func(error)) {}})
	pumpDocument(t, app, dispatch, state, 80, 18, func() bool { return len(state.sections) == len(paths) && state.sections[0].loaded })
	app.Key("c")
	app.Pump(80, 18)
	app.Key("a")
	app.Pump(80, 18)
	if state.active.commentBody != "a" {
		t.Fatalf("initial draft = %q", state.active.commentBody)
	}
	state.scroll.ScrollTo(0, state.sections[10].offset)
	pumpDocument(t, app, dispatch, state, 80, 18, func() bool { return state.sections[10].loaded })
	app.Key("b")
	app.Pump(80, 18)
	if state.active.commentBody != "ab" || state.active.commentAnchor.Path != paths[0] {
		t.Fatalf("offscreen editor lost focus/draft: %q at %s", state.active.commentBody, state.active.commentAnchor.Path)
	}
	state.Dispose()
}

func TestWorkspaceDiffDocumentRefreshDefersWhileRangeActive(t *testing.T) {
	state := &workspaceDiffPaneState{}
	dispatch := &queuedDiffDispatch{}
	warning := ""
	app := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: documentFixture("first.go"), Dispatch: dispatch.dispatch, testState: state,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}, OnCreateAnnotation: func(protocol.AnnotationAnchor, string, func(error)) {}, OnWarning: func(text string) { warning = text }})
	pumpDocument(t, app, dispatch, state, 80, 18, func() bool { return len(state.sections) == 1 && state.sections[0].loaded })
	app.Key("v")
	app.Pump(80, 18)
	anchor := state.active.selectionAnchor
	app.Key("r")
	app.Pump(80, 18)
	if state.active.selectionAnchor != anchor || !state.changesAvailable || warning != "Clear the selection before refreshing the diff" {
		t.Fatalf("manual refresh lost range: %+v; pending=%v warning=%q", state.active.selectionAnchor, state.changesAvailable, warning)
	}
	state.Dispose()
}

func TestWorkspaceDiffDocumentRetriesChangedFilePagination(t *testing.T) {
	backend := documentFixture("first.go", "second.go")
	initial := backend.observation
	var continuationCalls atomic.Int32
	backend.observeFn = func(_ context.Context, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
		if input.Cursor == "" {
			page := initial
			page.Files, page.NextCursor = page.Files[:1], "next"
			return page, nil
		}
		if continuationCalls.Add(1) == 1 {
			return protocol.DiffPage{}, &protocol.DiffError{Code: "unavailable", Message: "File list unavailable"}
		}
		page := initial
		page.Files = page.Files[1:]
		return page, nil
	}
	state := &workspaceDiffPaneState{}
	dispatch := &queuedDiffDispatch{}
	app := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch, testState: state,
		Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}})
	rows := pumpDocument(t, app, dispatch, state, 80, 18, func() bool { return state.observationError != "" && state.sections[0].loaded })
	if !strings.Contains(strings.Join(rows, "\n"), "content of first.go") {
		t.Fatal("partial manifest error hid loaded evidence")
	}
	col, row := findRenderedDiffText(t, app, 80, 18, "retry changed files")
	app.Click(col, row)
	rows = pumpDocument(t, app, dispatch, state, 80, 18, func() bool { return len(state.sections) == 2 && state.sections[1].loaded })
	for _, want := range []string{"content of first.go", "content of second.go"} {
		if !strings.Contains(strings.Join(rows, "\n"), want) {
			t.Fatalf("retried document missing %q", want)
		}
	}
	if state.observationCursor != "" || state.observationError != "" || state.stagedObservation.Revision != "" {
		t.Fatal("successful continuation did not clear manifest retry state")
	}
	state.Dispose()
}

func TestWorkspaceDiffHeaderWrapButton(t *testing.T) {
	for _, width := range []int{80, 140} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			backend := documentFixture("first.go")
			page := backend.pages["first.go"]
			page.Hunks[0].Lines[0].Content = strings.Repeat("word ", 20) + "TAIL"
			backend.pages["first.go"] = page
			state := &workspaceDiffPaneState{}
			dispatch := &queuedDiffDispatch{}
			var changes []bool
			app := uitest.New(workspaceDiffPane{Descriptor: workingTreeDiffWorkspacePane(testDiffWorkspace), Diff: backend, Dispatch: dispatch.dispatch, testState: state,
				Presentation: workspacePanePresentation{Active: true, Visible: true, Focused: true}, OnWrapLinesChanged: func(value bool) { changes = append(changes, value) }})
			pumpDocument(t, app, dispatch, state, width, 18, func() bool { return len(state.sections) == 1 && state.sections[0].loaded })
			col, row := findRenderedDiffText(t, app, width, 18, "Wrap off")
			if row != 0 || col < width/2 {
				t.Fatalf("wrap button position = %d,%d", col, row)
			}
			base := app.Cell(col, row).Style.Background
			app.Send(vaxis.Mouse{Col: col, Row: row, EventType: vaxis.EventMotion})
			app.Pump(width, 18)
			if app.Cell(col, row).Style.Background == base {
				t.Fatal("wrap control did not show hover feedback")
			}
			app.Click(col, row)
			pumpDocument(t, app, dispatch, state, width, 18, func() bool { return !state.cursorRevealPending })
			findRenderedDiffText(t, app, width, 18, "Wrap on")
			_, tailRow := findRenderedDiffText(t, app, width, 18, "TAIL")
			if !state.wrapLines || tailRow <= 5 || len(changes) != 1 || !changes[0] {
				t.Fatalf("mouse wrap state=%v tail row=%d callbacks=%v", state.wrapLines, tailRow, changes)
			}
			col, row = findRenderedDiffText(t, app, width, 18, "Wrap on")
			app.Click(col, row)
			app.Pump(width, 18)
			findRenderedDiffText(t, app, width, 18, "Wrap off")
			if state.wrapLines || len(changes) != 2 || changes[1] {
				t.Fatalf("second click state=%v callbacks=%v", state.wrapLines, changes)
			}
			app.Key("w")
			app.Pump(width, 18)
			findRenderedDiffText(t, app, width, 18, "Wrap on")
			if len(changes) != 3 || !changes[2] {
				t.Fatalf("keyboard callback = %v", changes)
			}
			state.Dispose()
		})
	}
}
