package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/highlight"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	kittheme "github.com/akonwi/kit/internal/theme"
	"github.com/rockorager/go-uucode"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

type workspaceDiffPhase uint8

type workspaceDiffSide uint8

const (
	workspaceDiffSideOld workspaceDiffSide = iota
	workspaceDiffSideNew
)

const workspaceDiffSplitBreakpoint = 120

const (
	workspaceDiffInitial workspaceDiffPhase = iota
	workspaceDiffLoading
	workspaceDiffReady
	workspaceDiffEmpty
	workspaceDiffError
)

type moveWorkspaceDiffIntent struct{ lines, columns int }

func (moveWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.move" }

type moveWorkspaceDiffFileIntent struct{ delta int }

func (moveWorkspaceDiffFileIntent) IntentType() ui.IntentType { return "kit.workspace-diff.file" }

type moveWorkspaceDiffHunkIntent struct{ delta int }

func (moveWorkspaceDiffHunkIntent) IntentType() ui.IntentType { return "kit.workspace-diff.hunk" }

type refreshWorkspaceDiffIntent struct{}

func (refreshWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.refresh" }

type loadMoreWorkspaceDiffIntent struct{}

func (loadMoreWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.load-more" }

type toggleWorkspaceDiffWrapIntent struct{}

func (toggleWorkspaceDiffWrapIntent) IntentType() ui.IntentType {
	return "kit.workspace-diff.toggle-wrap"
}

type panWorkspaceDiffIntent struct{ columns int }

func (panWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.pan" }

type workspaceDiffPane struct {
	Descriptor         workspacePaneDescriptor
	CurrentWorkspaceID string
	Diff               sessionclient.WorkingTreeDiffSession
	Highlighter        highlight.Highlighter
	Dispatch           func(func())
	Presentation       workspacePanePresentation
	OnFocusRequest     ui.VoidCallback
}

func (workspaceDiffPane) CreateState() ui.State { return &workspaceDiffPaneState{} }

type workspaceDiffObservationResult struct {
	generation uint64
	page       protocol.WorkingTreePage
	err        error
}

type workspaceDiffFileResult struct {
	generation uint64
	page       protocol.FileDiffPage
	append     bool
	err        error
}

type workspaceDiffHighlightResult struct {
	generation uint64
	old        highlight.Result
	new        highlight.Result
}

type workspaceDiffPaneState struct {
	ui.StateBase
	phase               workspaceDiffPhase
	generation          uint64
	cancel              context.CancelFunc
	observation         protocol.DiffObservation
	files               []protocol.DiffFileSummary
	selectedFile        int
	hunks               []protocol.DiffHunk
	cursorRow           int
	cursorSide          workspaceDiffSide
	wrapLines           bool
	splitColumn         int
	splitTargetsCached  []workspaceDiffSplitTarget
	splitTargetsWidth   int
	splitTargetsWrap    bool
	splitMaxColumn      int
	splitTargetsValid   bool
	lineRows            []int
	hunkRows            []int
	errorText           string
	loadingFile         bool
	loadingMore         bool
	fileNextCursor      string
	observationCursor   string
	scroll              ui.ScrollPaneController
	viewportWidth       int
	cursorRevealPending bool
	revealPendingLayout bool
	focus               ui.FocusNode
	appliedOpen         uint64
	resultMu            sync.Mutex
	pendingObservation  *workspaceDiffObservationResult
	pendingFile         *workspaceDiffFileResult
	highlightGeneration uint64
	highlightCancel     context.CancelFunc
	pendingHighlight    *workspaceDiffHighlightResult
	oldHighlighted      highlight.Result
	newHighlighted      highlight.Result
	highlightReady      bool
	disposed            bool
}

func (s *workspaceDiffPaneState) InitState() {
	w := s.Widget().(workspaceDiffPane)
	s.appliedOpen = w.Descriptor.OpenGeneration
	s.cursorSide = workspaceDiffSideNew
	if w.Presentation.Active && !s.isFrozen(w) {
		s.startObservation()
	}
}

func (s *workspaceDiffPaneState) DidUpdateWidget(old ui.Widget) {
	previous := old.(workspaceDiffPane)
	w := s.Widget().(workspaceDiffPane)
	if s.isFrozen(w) {
		s.stopWork()
		return
	}
	if previous.Presentation.Active && !w.Presentation.Active {
		s.stopWork()
		return
	}
	if w.Descriptor.OpenGeneration != s.appliedOpen {
		s.appliedOpen = w.Descriptor.OpenGeneration
		if w.Presentation.Active {
			s.startObservation()
		}
		return
	}
	if !previous.Presentation.Active && w.Presentation.Active {
		switch {
		case s.phase == workspaceDiffInitial || s.phase == workspaceDiffLoading:
			s.startObservation()
		case s.loadingFile:
			s.loadingFile = false
			s.startFileLoad()
		case s.loadingMore:
			s.loadingMore = false
			s.loadMoreFileDiff()
		case !s.highlightReady && len(s.hunks) > 0:
			s.startHighlight()
		}
	}
}

func (s *workspaceDiffPaneState) Dispose() {
	s.disposed = true
	s.stopWork()
}

func (s *workspaceDiffPaneState) isFrozen(w workspaceDiffPane) bool {
	return w.CurrentWorkspaceID != "" && w.CurrentWorkspaceID != w.Descriptor.WorkspaceID
}

func (s *workspaceDiffPaneState) stopWork() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.generation++
	}
	s.stopHighlight()
}

func (s *workspaceDiffPaneState) stopHighlight() {
	if s.highlightCancel != nil {
		s.highlightCancel()
		s.highlightCancel = nil
		s.highlightGeneration++
	}
}

func (s *workspaceDiffPaneState) startObservation() {
	w := s.Widget().(workspaceDiffPane)
	if !w.Presentation.Active || s.isFrozen(w) {
		return
	}
	s.stopWork()
	s.phase = workspaceDiffLoading
	s.errorText = ""
	s.files = nil
	s.hunks = nil
	s.invalidateSplitTargets()
	s.highlightReady = false
	s.oldHighlighted = highlight.Result{}
	s.newHighlighted = highlight.Result{}
	s.observation = protocol.DiffObservation{}
	s.observationCursor = ""
	s.cursorRow = 0
	s.lineRows = nil
	s.hunkRows = nil
	if w.Diff == nil {
		s.phase = workspaceDiffError
		s.errorText = "Working-tree diffs are unavailable"
		return
	}
	s.requestObservationPage("")
}

func (s *workspaceDiffPaneState) requestObservationPage(cursor string) {
	w := s.Widget().(workspaceDiffPane)
	if w.Diff == nil || !w.Presentation.Active || s.isFrozen(w) {
		return
	}
	s.stopWork()
	s.generation++
	generation := s.generation
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	input := protocol.ObserveWorkingTreeInput{WorkspaceID: w.Descriptor.WorkspaceID, Cursor: cursor}
	go func() {
		page, err := w.Diff.ObserveWorkingTree(ctx, input)
		if ctx.Err() != nil {
			return
		}
		s.resultMu.Lock()
		if s.pendingObservation == nil || generation >= s.pendingObservation.generation {
			s.pendingObservation = &workspaceDiffObservationResult{generation: generation, page: page, err: err}
		}
		s.resultMu.Unlock()
		dispatch(func() {
			if !s.disposed {
				s.SetState(func() {})
			}
		})
	}()
}

func (s *workspaceDiffPaneState) completeObservation(page protocol.WorkingTreePage, err error) {
	s.cancel = nil
	if err != nil {
		s.phase = workspaceDiffError
		s.errorText = workspaceDiffErrorText(err)
		return
	}
	if s.observation.Revision == "" {
		s.observation = page.Observation
	}
	s.files = append(s.files, page.Files...)
	s.observationCursor = page.NextCursor
	if page.NextCursor != "" {
		s.requestObservationPage(page.NextCursor)
		return
	}
	if len(s.files) == 0 {
		s.phase = workspaceDiffEmpty
		return
	}
	s.phase = workspaceDiffReady
	s.selectedFile = min(s.selectedFile, len(s.files)-1)
	s.startFileLoad()
}

func workspaceDiffErrorText(err error) string {
	var diffErr *protocol.DiffError
	if errors.As(err, &diffErr) && strings.TrimSpace(diffErr.Message) != "" {
		return diffErr.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The working-tree observation timed out"
	}
	return "Could not load working-tree changes"
}

func (s *workspaceDiffPaneState) startFileLoad() {
	s.hunks = nil
	s.invalidateSplitTargets()
	s.highlightReady = false
	s.oldHighlighted = highlight.Result{}
	s.newHighlighted = highlight.Result{}
	s.cursorRow = 0
	s.lineRows = nil
	s.hunkRows = nil
	s.fileNextCursor = ""
	s.requestFilePage("", false)
}

func (s *workspaceDiffPaneState) requestFilePage(cursor string, appendPage bool) {
	w := s.Widget().(workspaceDiffPane)
	if w.Diff == nil || !w.Presentation.Active || s.isFrozen(w) || s.selectedFile < 0 || s.selectedFile >= len(s.files) {
		return
	}
	s.stopWork()
	s.generation++
	generation := s.generation
	s.loadingFile = !appendPage
	s.loadingMore = appendPage
	file := s.files[s.selectedFile]
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	input := protocol.ReadFileDiffInput{
		TargetID: s.observation.Target.ID, TargetRevision: s.observation.Revision,
		Path: file.Path, ExpectedFileRevision: file.FileRevision, Cursor: cursor,
	}
	go func() {
		page, err := w.Diff.ReadFileDiff(ctx, input)
		if ctx.Err() != nil {
			return
		}
		s.resultMu.Lock()
		if s.pendingFile == nil || generation >= s.pendingFile.generation {
			s.pendingFile = &workspaceDiffFileResult{generation: generation, page: page, append: appendPage, err: err}
		}
		s.resultMu.Unlock()
		dispatch(func() {
			if !s.disposed {
				s.SetState(func() {})
			}
		})
	}()
}

func (s *workspaceDiffPaneState) completeFileLoad(page protocol.FileDiffPage, appendPage bool, err error) {
	s.cancel = nil
	s.loadingFile = false
	s.loadingMore = false
	if err != nil {
		s.errorText = workspaceDiffErrorText(err)
		return
	}
	s.errorText = ""
	if appendPage {
		s.appendHunks(page.Hunks)
	} else {
		s.hunks = append([]protocol.DiffHunk(nil), page.Hunks...)
	}
	s.invalidateSplitTargets()
	s.fileNextCursor = page.NextCursor
	s.rebuildHunkRows()
	if s.cursorRow == 0 && len(s.hunkRows) > 0 {
		s.setCursorRow(s.hunkRows[0])
	}
	s.startHighlight()
}

func (s *workspaceDiffPaneState) startHighlight() {
	w := s.Widget().(workspaceDiffPane)
	if !w.Presentation.Active || s.isFrozen(w) || len(s.hunks) == 0 || s.selectedFile < 0 || s.selectedFile >= len(s.files) {
		return
	}
	s.stopHighlight()
	s.highlightReady = false
	s.highlightGeneration++
	generation := s.highlightGeneration
	oldSource, newSource := workspaceDiffHighlightSources(s.hunks)
	file := s.files[s.selectedFile]
	ctx, cancel := context.WithCancel(context.Background())
	s.highlightCancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	highlighter := w.Highlighter
	if highlighter == nil {
		highlighter = highlight.Default
	}
	go func() {
		oldResult := highlight.Validate(highlighter.Highlight(ctx, highlight.Request{Path: file.Path, Source: oldSource, Revision: file.FileRevision + ":old"}))
		if ctx.Err() != nil {
			return
		}
		newResult := highlight.Validate(highlighter.Highlight(ctx, highlight.Request{Path: file.Path, Source: newSource, Revision: file.FileRevision + ":new"}))
		if ctx.Err() != nil {
			return
		}
		s.resultMu.Lock()
		if s.pendingHighlight == nil || generation >= s.pendingHighlight.generation {
			s.pendingHighlight = &workspaceDiffHighlightResult{generation: generation, old: oldResult, new: newResult}
		}
		s.resultMu.Unlock()
		dispatch(func() {
			if !s.disposed {
				s.SetState(func() {})
			}
		})
	}()
}

func workspaceDiffHighlightSources(hunks []protocol.DiffHunk) (string, string) {
	var oldSource, newSource strings.Builder
	for _, hunk := range hunks {
		for _, line := range hunk.Lines {
			if line.OldLine != nil {
				oldSource.WriteString(line.Content)
				oldSource.WriteByte('\n')
			}
			if line.NewLine != nil {
				newSource.WriteString(line.Content)
				newSource.WriteByte('\n')
			}
		}
	}
	return oldSource.String(), newSource.String()
}

func (s *workspaceDiffPaneState) appendHunks(next []protocol.DiffHunk) {
	if len(s.hunks) > 0 && len(next) > 0 && next[0].ContinuedBefore && s.hunks[len(s.hunks)-1].ContinuedAfter {
		last := &s.hunks[len(s.hunks)-1]
		last.Lines = append(last.Lines, next[0].Lines...)
		last.ContinuedAfter = next[0].ContinuedAfter
		next = next[1:]
	}
	s.hunks = append(s.hunks, next...)
}

func (s *workspaceDiffPaneState) rebuildHunkRows() {
	s.hunkRows = s.hunkRows[:0]
	s.lineRows = s.lineRows[:0]
	row := 0
	for _, hunk := range s.hunks {
		s.hunkRows = append(s.hunkRows, row+1)
		for line := range hunk.Lines {
			s.lineRows = append(s.lineRows, row+line+1)
		}
		row += len(hunk.Lines) + 1
	}
}

func (s *workspaceDiffPaneState) setCursorRow(row int) {
	s.cursorRow = row
	if line, ok := s.lineAtRow(row); ok {
		switch line.Kind {
		case "deletion":
			s.cursorSide = workspaceDiffSideOld
		case "addition":
			s.cursorSide = workspaceDiffSideNew
		}
	}
}

func (s *workspaceDiffPaneState) lineAtRow(wanted int) (protocol.DiffLine, bool) {
	row := 0
	for _, hunk := range s.hunks {
		row++
		for _, line := range hunk.Lines {
			if row == wanted {
				return line, true
			}
			row++
		}
	}
	return protocol.DiffLine{}, false
}

func (s *workspaceDiffPaneState) moveLine(delta int) {
	if s.viewportWidth >= workspaceDiffSplitBreakpoint {
		s.moveSplitLine(delta)
		return
	}
	if len(s.lineRows) == 0 || delta == 0 {
		return
	}
	selected := 0
	for index, row := range s.lineRows {
		if row <= s.cursorRow {
			selected = index
		}
	}
	selected = max(0, min(len(s.lineRows)-1, selected+delta))
	s.setCursorRow(s.lineRows[selected])
	s.revealCursor()
	if delta > 0 && len(s.lineRows)-selected <= 10 {
		s.loadMoreFileDiff()
	}
}

type workspaceDiffSplitTarget struct {
	oldRow, newRow int
	visualRow      int
	height         int
}

func workspaceDiffSplitWidths(viewport int) (int, int) {
	available := max(2, viewport-1)
	oldWidth := available / 2
	return oldWidth, available - oldWidth
}

func workspaceDiffSideGutterWidth(line protocol.DiffLine, newSide bool) int {
	number, marker := "", " "
	if newSide && line.NewLine != nil {
		number = fmt.Sprintf("%d", *line.NewLine)
	} else if !newSide && line.OldLine != nil {
		number = fmt.Sprintf("%d", *line.OldLine)
	}
	if line.Kind == "addition" {
		marker = "+"
	} else if line.Kind == "deletion" {
		marker = "−"
	}
	return uucode.StringWidth(fmt.Sprintf("%5s    %s ", number, marker))
}

func workspaceDiffWrappedLineHeight(line protocol.DiffLine, newSide bool, sideWidth int) int {
	contentWidth := max(1, sideWidth-workspaceDiffSideGutterWidth(line, newSide))
	layout := ui.LayoutText(
		[]ui.TextSpan{{Text: highlight.Sanitize(line.Content)}},
		ui.Constraints{MaxWidth: contentWidth, MaxHeight: ui.Unbounded},
		ui.TextLayoutOptions{SoftWrap: true},
	)
	return max(1, layout.Size.Height)
}

func workspaceDiffSplitPairHeight(oldLine, newLine *protocol.DiffLine, viewport int, wrap bool) int {
	if !wrap {
		return 1
	}
	oldWidth, newWidth := workspaceDiffSplitWidths(viewport)
	height := 1
	if oldLine != nil {
		height = max(height, workspaceDiffWrappedLineHeight(*oldLine, false, oldWidth))
	}
	if newLine != nil {
		height = max(height, workspaceDiffWrappedLineHeight(*newLine, true, newWidth))
	}
	return height
}

func (s *workspaceDiffPaneState) invalidateSplitTargets() {
	s.splitTargetsCached = nil
	s.splitMaxColumn = 0
	s.splitTargetsValid = false
}

func (s *workspaceDiffPaneState) splitTargets() []workspaceDiffSplitTarget {
	if s.splitTargetsValid && s.splitTargetsWidth == s.viewportWidth && s.splitTargetsWrap == s.wrapLines {
		return s.splitTargetsCached
	}
	type sourceLine struct {
		row  int
		line protocol.DiffLine
	}
	targets := make([]workspaceDiffSplitTarget, 0, len(s.lineRows))
	row, visualRow := 0, 0
	for _, hunk := range s.hunks {
		row++
		visualRow++
		for index := 0; index < len(hunk.Lines); {
			if hunk.Lines[index].Kind == "context" {
				line := hunk.Lines[index]
				height := workspaceDiffSplitPairHeight(&line, &line, s.viewportWidth, s.wrapLines)
				targets = append(targets, workspaceDiffSplitTarget{oldRow: row, newRow: row, visualRow: visualRow, height: height})
				row++
				visualRow += height
				index++
				continue
			}
			end := index
			deletions, additions := make([]sourceLine, 0), make([]sourceLine, 0)
			for end < len(hunk.Lines) && hunk.Lines[end].Kind != "context" {
				item := sourceLine{row: row, line: hunk.Lines[end]}
				if item.line.Kind == "deletion" {
					deletions = append(deletions, item)
				} else {
					additions = append(additions, item)
				}
				row++
				end++
			}
			for pair := 0; pair < max(len(deletions), len(additions)); pair++ {
				target := workspaceDiffSplitTarget{oldRow: -1, newRow: -1, visualRow: visualRow, height: 1}
				var oldLine, newLine *protocol.DiffLine
				if pair < len(deletions) {
					target.oldRow = deletions[pair].row
					oldLine = &deletions[pair].line
				}
				if pair < len(additions) {
					target.newRow = additions[pair].row
					newLine = &additions[pair].line
				}
				target.height = workspaceDiffSplitPairHeight(oldLine, newLine, s.viewportWidth, s.wrapLines)
				targets = append(targets, target)
				visualRow += target.height
			}
			index = end
		}
	}
	oldWidth, newWidth := workspaceDiffSplitWidths(s.viewportWidth)
	maximumColumn := 0
	for _, hunk := range s.hunks {
		for _, line := range hunk.Lines {
			contentCells := uucode.StringWidth(highlight.Sanitize(line.Content))
			if line.OldLine != nil {
				maximumColumn = max(maximumColumn, contentCells-max(1, oldWidth-workspaceDiffSideGutterWidth(line, false)))
			}
			if line.NewLine != nil {
				maximumColumn = max(maximumColumn, contentCells-max(1, newWidth-workspaceDiffSideGutterWidth(line, true)))
			}
		}
	}
	s.splitTargetsCached = targets
	s.splitMaxColumn = max(0, maximumColumn)
	s.splitTargetsWidth = s.viewportWidth
	s.splitTargetsWrap = s.wrapLines
	s.splitTargetsValid = true
	return targets
}

type workspaceDiffNavigationLine struct {
	row       int
	side      workspaceDiffSide
	forceSide bool
}

func (s *workspaceDiffPaneState) splitNavigationLines() []workspaceDiffNavigationLine {
	lines := make([]workspaceDiffNavigationLine, 0, len(s.lineRows))
	row := 0
	for _, hunk := range s.hunks {
		row++
		for _, line := range hunk.Lines {
			target := workspaceDiffNavigationLine{row: row}
			switch line.Kind {
			case "deletion":
				target.side = workspaceDiffSideOld
				target.forceSide = true
			case "addition":
				target.side = workspaceDiffSideNew
				target.forceSide = true
			}
			lines = append(lines, target)
			row++
		}
	}
	return lines
}

func (s *workspaceDiffPaneState) moveSplitLine(delta int) {
	lines := s.splitNavigationLines()
	if len(lines) == 0 || delta == 0 {
		return
	}
	selected := 0
	for index, line := range lines {
		if line.row == s.cursorRow {
			selected = index
			break
		}
	}
	selected = max(0, min(len(lines)-1, selected+delta))
	target := lines[selected]
	s.cursorRow = target.row
	if target.forceSide {
		s.cursorSide = target.side
	}
	s.revealCursor()
	if delta > 0 && len(lines)-selected <= 10 {
		s.loadMoreFileDiff()
	}
}

func (s *workspaceDiffPaneState) panSplit(columns int) {
	if s.wrapLines || columns == 0 {
		return
	}
	s.splitColumn = max(0, min(s.maxSplitColumn(), s.splitColumn+columns))
}

func (s *workspaceDiffPaneState) maxSplitColumn() int {
	s.splitTargets()
	return s.splitMaxColumn
}

func (s *workspaceDiffPaneState) loadMoreFileDiff() {
	if s.fileNextCursor != "" && !s.loadingMore {
		s.requestFilePage(s.fileNextCursor, true)
	}
}

func (s *workspaceDiffPaneState) cursorVisualRow() int {
	if s.viewportWidth < workspaceDiffSplitBreakpoint {
		return s.cursorRow
	}
	for _, target := range s.splitTargets() {
		if s.cursorSide == workspaceDiffSideOld && target.oldRow == s.cursorRow || s.cursorSide == workspaceDiffSideNew && target.newRow == s.cursorRow {
			return target.visualRow
		}
	}
	return 0
}

func (s *workspaceDiffPaneState) revealCursor() {
	if !s.scroll.Attached() {
		return
	}
	vertical := s.scroll.Metrics(ui.ScrollVertical)
	viewportHeight := max(1, vertical.ViewportHeight)
	row, target := s.cursorVisualRow(), vertical.ScrollOffset
	if row < vertical.ScrollOffset {
		target = row
	} else if row >= vertical.ScrollOffset+viewportHeight {
		target = row - viewportHeight + 1
	}
	horizontal := s.horizontalOffset()
	currentHorizontal := s.scroll.Metrics(ui.ScrollHorizontal).ScrollOffset
	if target != vertical.ScrollOffset || horizontal != currentHorizontal {
		s.scroll.ScrollTo(horizontal, max(0, target))
	}
}

func (s *workspaceDiffPaneState) TickFrame(time.Time) bool {
	if !s.cursorRevealPending {
		return false
	}
	if s.revealPendingLayout {
		s.revealPendingLayout = false
		return true
	}
	s.cursorRevealPending = false
	s.revealCursor()
	return false
}

func (s *workspaceDiffPaneState) horizontalOffset() int {
	if s.viewportWidth >= workspaceDiffSplitBreakpoint || !s.scroll.Attached() {
		return 0
	}
	return s.scroll.Metrics(ui.ScrollHorizontal).ScrollOffset
}

func (s *workspaceDiffPaneState) moveFile(delta int) {
	if len(s.files) == 0 || delta == 0 {
		return
	}
	next := (s.selectedFile + delta) % len(s.files)
	if next < 0 {
		next += len(s.files)
	}
	s.selectedFile = next
	s.scroll = ui.ScrollPaneController{}
	s.startFileLoad()
}

func (s *workspaceDiffPaneState) moveHunk(delta int) {
	if len(s.hunkRows) == 0 || delta == 0 {
		return
	}
	selected := 0
	for index, row := range s.hunkRows {
		if row <= s.cursorRow {
			selected = index
		}
	}
	selected = max(0, min(len(s.hunkRows)-1, selected+delta))
	s.setCursorRow(s.hunkRows[selected])
	s.revealCursor()
	if delta > 0 && selected == len(s.hunkRows)-1 {
		s.loadMoreFileDiff()
	}
}

func (s *workspaceDiffPaneState) applyPendingResults() {
	s.resultMu.Lock()
	observation := s.pendingObservation
	file := s.pendingFile
	highlightResult := s.pendingHighlight
	s.pendingObservation = nil
	s.pendingFile = nil
	s.pendingHighlight = nil
	s.resultMu.Unlock()
	if observation != nil && observation.generation == s.generation {
		s.completeObservation(observation.page, observation.err)
	}
	if file != nil && file.generation == s.generation {
		s.completeFileLoad(file.page, file.append, file.err)
	}
	if highlightResult != nil && highlightResult.generation == s.highlightGeneration {
		s.highlightCancel = nil
		s.oldHighlighted = highlightResult.old
		s.newHighlighted = highlightResult.new
		s.highlightReady = true
	}
}

func (s *workspaceDiffPaneState) Build(ctx ui.BuildContext) ui.Widget {
	s.applyPendingResults()
	w := s.Widget().(workspaceDiffPane)
	theme := ui.MustDepend[ui.Theme](ctx)
	semantic, ok := ui.Depend[SemanticTheme](ctx)
	if !ok {
		semantic = semanticFallback(theme)
	}
	left, right := "Working tree", ""
	if len(s.files) > 0 && s.selectedFile >= 0 && s.selectedFile < len(s.files) {
		file := s.files[s.selectedFile]
		left = file.Path
		right = fmt.Sprintf("%d of %d", s.selectedFile+1, len(s.files))
		if file.Additions != nil && file.Deletions != nil {
			right += fmt.Sprintf("  +%d −%d", *file.Additions, *file.Deletions)
		}
	}
	if s.isFrozen(w) {
		if right != "" {
			right += "  " + glyphMiddleDot + "  "
		}
		right += "Frozen"
	}
	header := workspacePanelHeader(theme, left, right)
	body := s.body(theme, semantic)
	footer := workspacePanelFooter(theme, s.footerText())
	bindings := map[ui.IntentType]ui.ActionFunc{}
	shortcuts := ui.ShortcutMap{}
	if w.Presentation.Active {
		bindings[moveWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			movement := intent.(moveWorkspaceDiffIntent)
			s.SetState(func() {
				if movement.lines != 0 {
					s.moveLine(movement.lines)
				}
				if movement.columns != 0 {
					if s.viewportWidth >= workspaceDiffSplitBreakpoint {
						s.panSplit(movement.columns)
					} else {
						s.scroll.ScrollBy(movement.columns, 0)
					}
				}
			})
			return ui.EventHandled
		}
		bindings[moveWorkspaceDiffHunkIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			s.SetState(func() { s.moveHunk(intent.(moveWorkspaceDiffHunkIntent).delta) })
			return ui.EventHandled
		}
		shortcuts["Up"] = moveWorkspaceDiffIntent{lines: -1}
		shortcuts["k"] = moveWorkspaceDiffIntent{lines: -1}
		shortcuts["Down"] = moveWorkspaceDiffIntent{lines: 1}
		shortcuts["j"] = moveWorkspaceDiffIntent{lines: 1}
		shortcuts["Left"] = moveWorkspaceDiffIntent{columns: -4}
		shortcuts["h"] = moveWorkspaceDiffIntent{columns: -4}
		shortcuts["Right"] = moveWorkspaceDiffIntent{columns: 4}
		shortcuts["l"] = moveWorkspaceDiffIntent{columns: 4}
		shortcuts["{"] = moveWorkspaceDiffHunkIntent{delta: -1}
		shortcuts["}"] = moveWorkspaceDiffHunkIntent{delta: 1}
		bindings[toggleWorkspaceDiffWrapIntent{}.IntentType()] = func(_ ui.EventContext, _ ui.Intent) ui.EventResult {
			s.SetState(func() {
				s.wrapLines = !s.wrapLines
				if s.wrapLines {
					s.splitColumn = 0
				}
				s.invalidateSplitTargets()
				s.cursorRevealPending = true
				s.revealPendingLayout = true
			})
			return ui.EventHandled
		}
		bindings[panWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			s.SetState(func() { s.panSplit(intent.(panWorkspaceDiffIntent).columns) })
			return ui.EventHandled
		}
		shortcuts["w"] = toggleWorkspaceDiffWrapIntent{}
		shortcuts["H"] = panWorkspaceDiffIntent{columns: -4}
		shortcuts["L"] = panWorkspaceDiffIntent{columns: 4}
		if !s.isFrozen(w) {
			bindings[moveWorkspaceDiffFileIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
				s.SetState(func() { s.moveFile(intent.(moveWorkspaceDiffFileIntent).delta) })
				return ui.EventHandled
			}
			bindings[refreshWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, _ ui.Intent) ui.EventResult {
				s.SetState(func() { s.startObservation() })
				return ui.EventHandled
			}
			shortcuts["["] = moveWorkspaceDiffFileIntent{delta: -1}
			shortcuts["]"] = moveWorkspaceDiffFileIntent{delta: 1}
			shortcuts["r"] = refreshWorkspaceDiffIntent{}
			if s.fileNextCursor != "" && !s.loadingMore {
				bindings[loadMoreWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, _ ui.Intent) ui.EventResult {
					s.SetState(func() { s.loadMoreFileDiff() })
					return ui.EventHandled
				}
				shortcuts["m"] = loadMoreWorkspaceDiffIntent{}
			}
		}
	}
	content := ui.Widget(workspacePanelLayout{Header: header, Body: body, Footer: footer})
	content = ui.Focus(&s.focus, content)
	content = ui.FocusScope{AutoFocus: w.Presentation.Active, Child: content}
	content = mouseActivator{Child: content, DefaultMouseShape: true, OnScroll: func(_ ui.EventContext, mouse ui.Mouse) ui.EventResult {
		if s.viewportWidth < workspaceDiffSplitBreakpoint || s.wrapLines {
			return ui.EventIgnored
		}
		delta := 0
		switch mouse.Button {
		case ui.MouseWheelLeft:
			delta = -4
		case ui.MouseWheelRight:
			delta = 4
		case ui.MouseWheelUp:
			if mouse.Modifiers&vaxis.ModShift != 0 {
				delta = -4
			}
		case ui.MouseWheelDown:
			if mouse.Modifiers&vaxis.ModShift != 0 {
				delta = 4
			}
		}
		if delta == 0 {
			return ui.EventIgnored
		}
		s.SetState(func() { s.panSplit(delta) })
		return ui.EventHandled
	}, OnPrimaryDownCapture: func(event ui.EventContext) {
		if w.OnFocusRequest != nil {
			w.OnFocusRequest(event)
		}
		s.focus.RequestFocus()
	}}
	return ui.Actions{Bindings: bindings, Child: keyShortcuts{Bindings: shortcuts, Child: content}}
}

func (s *workspaceDiffPaneState) body(theme ui.Theme, semantic SemanticTheme) ui.Widget {
	var child ui.Widget
	switch {
	case s.phase == workspaceDiffLoading:
		child = ui.Center(spinnerWithLabel("Loading working-tree changes…", ui.Style{Foreground: theme.MutedForeground}))
	case s.phase == workspaceDiffEmpty:
		child = ui.Center(ui.Text{Value: "No working-tree changes", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	case s.phase == workspaceDiffError:
		child = ui.Center(ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			ui.Text{Value: "Could not load diff", Style: ui.Style{Foreground: theme.DangerText}, MaxLines: 1},
			ui.Text{Value: s.errorText, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis},
		}})
	case s.loadingFile:
		child = ui.Center(spinnerWithLabel("Loading file diff…", ui.Style{Foreground: theme.MutedForeground}))
	case s.errorText != "":
		child = ui.Center(ui.Text{Value: s.errorText, Style: ui.Style{Foreground: theme.DangerText}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis})
	case len(s.hunks) == 0:
		message := "No textual changes"
		if len(s.files) > 0 {
			file := s.files[s.selectedFile]
			if file.ContentState != "text" {
				message = workspaceDiffContentStateText(file)
			}
		}
		child = ui.Center(ui.Text{Value: message, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	default:
		var rows []ui.Widget
		var contentWidth, contentHeight int
		if s.viewportWidth >= workspaceDiffSplitBreakpoint {
			rows, contentWidth, contentHeight = s.splitDiffRows(theme, semantic)
		} else {
			rows, contentWidth, contentHeight = s.diffRows(theme, semantic)
		}
		pane := ui.Widget(ui.ScrollPane{Controller: &s.scroll, Child: ui.SizedBox{
			Width: max(s.viewportWidth, contentWidth), Height: contentHeight,
			Child: ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows},
		}})
		pane = ui.Scrollbar{
			Child:      pane,
			ThumbStyle: ui.Style{Foreground: semantic.Token(kittheme.TokenScrollbarForeground)},
			TrackStyle: ui.Style{Foreground: semantic.Token(kittheme.TokenScrollbarBackground)},
		}
		child = widthProbe{WidthChanged: func(width int) {
			if width != s.viewportWidth {
				s.viewportWidth = width
				s.splitColumn = 0
				s.invalidateSplitTargets()
				s.cursorRevealPending = true
				s.revealPendingLayout = true
				s.MarkNeedsBuild()
			}
		}, Child: pane}
	}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{ui.Expanded(child)}}
}

func workspaceDiffContentStateText(file protocol.DiffFileSummary) string {
	switch file.ContentState {
	case "binary":
		return "Binary file changed"
	case "conflict":
		return "File has unresolved conflicts"
	case "intent_to_add":
		return "Intent-to-add file has no textual diff"
	case "unsupported_transform":
		return "File uses an unsupported content transform"
	case "unsupported_kind":
		return "File kind is not available for textual diff"
	case "too_large":
		return "File diff exceeds the configured limit"
	case "unavailable":
		return "File diff is unavailable"
	default:
		return "No textual changes"
	}
}

func workspaceDiffUnifiedLineWidth(line protocol.DiffLine) int {
	oldNumber, newNumber, marker := "", "", " "
	if line.OldLine != nil {
		oldNumber = fmt.Sprintf("%d", *line.OldLine)
	}
	if line.NewLine != nil {
		newNumber = fmt.Sprintf("%d", *line.NewLine)
	}
	if line.Kind == "addition" {
		marker = "+"
	} else if line.Kind == "deletion" {
		marker = "−"
	}
	gutter := fmt.Sprintf("%5s %5s    %s ", oldNumber, newNumber, marker)
	return uucode.StringWidth(gutter) + uucode.StringWidth(highlight.Sanitize(line.Content))
}

func (s *workspaceDiffPaneState) diffRows(theme ui.Theme, semantic SemanticTheme) ([]ui.Widget, int, int) {
	rows := make([]ui.Widget, 0)
	visualRow, contentWidth := 0, 1
	var oldSyntax, newSyntax [][]ui.TextSpan
	if s.highlightReady {
		oldSyntax = workspaceDiffSyntaxLines(s.oldHighlighted, semantic)
		newSyntax = workspaceDiffSyntaxLines(s.newHighlighted, semantic)
	}
	oldSyntaxLine, newSyntaxLine := 0, 0
	for _, hunk := range s.hunks {
		header := fmt.Sprintf("%s -%d,%d +%d,%d", glyphDiamond, hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
		contentWidth = max(contentWidth, uucode.StringWidth(header))
		rows = append(rows, ui.SizedBox{Height: 1, Child: ui.Text{Value: header, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}})
		visualRow++
		for _, line := range hunk.Lines {
			contentWidth = max(contentWidth, workspaceDiffUnifiedLineWidth(line))
			var syntaxSpans []ui.TextSpan
			switch line.Kind {
			case "deletion":
				if oldSyntaxLine < len(oldSyntax) {
					syntaxSpans = oldSyntax[oldSyntaxLine]
				}
			case "addition", "context":
				if newSyntaxLine < len(newSyntax) {
					syntaxSpans = newSyntax[newSyntaxLine]
				}
			}
			if line.OldLine != nil {
				oldSyntaxLine++
			}
			if line.NewLine != nil {
				newSyntaxLine++
			}
			row := visualRow
			moveCursor := func(ui.EventContext) {
				if s.cursorRow != row {
					s.SetState(func() { s.setCursorRow(row) })
				}
			}
			rows = append(rows, workspaceDiffLineWidget(line, syntaxSpans, row == s.cursorRow, theme, semantic, moveCursor, moveCursor))
			visualRow++
		}
	}
	return rows, contentWidth, visualRow
}

type workspaceDiffRenderedLine struct {
	line   protocol.DiffLine
	row    int
	syntax []ui.TextSpan
}

func (s *workspaceDiffPaneState) splitDiffRows(theme ui.Theme, semantic SemanticTheme) ([]ui.Widget, int, int) {
	rows := make([]ui.Widget, 0)
	visualRow, contentWidth, contentHeight := 0, max(1, s.viewportWidth), 0
	targets, targetIndex := s.splitTargets(), 0
	var oldSyntax, newSyntax [][]ui.TextSpan
	if s.highlightReady {
		oldSyntax = workspaceDiffSyntaxLines(s.oldHighlighted, semantic)
		newSyntax = workspaceDiffSyntaxLines(s.newHighlighted, semantic)
	}
	oldSyntaxLine, newSyntaxLine := 0, 0
	for _, hunk := range s.hunks {
		header := fmt.Sprintf("%s -%d,%d +%d,%d", glyphDiamond, hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
		rows = append(rows, ui.SizedBox{Height: 1, Child: ui.Text{Value: header, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}})
		contentHeight++
		visualRow++
		decorated := make([]workspaceDiffRenderedLine, 0, len(hunk.Lines))
		for _, line := range hunk.Lines {
			item := workspaceDiffRenderedLine{line: line, row: visualRow}
			switch line.Kind {
			case "deletion":
				if oldSyntaxLine < len(oldSyntax) {
					item.syntax = oldSyntax[oldSyntaxLine]
				}
			case "addition", "context":
				if newSyntaxLine < len(newSyntax) {
					item.syntax = newSyntax[newSyntaxLine]
				}
			}
			if line.OldLine != nil {
				oldSyntaxLine++
			}
			if line.NewLine != nil {
				newSyntaxLine++
			}
			decorated = append(decorated, item)
			visualRow++
		}
		for index := 0; index < len(decorated); {
			if decorated[index].line.Kind == "context" {
				item := decorated[index]
				rowHeight := targets[targetIndex].height
				targetIndex++
				rowWidget := s.workspaceDiffSplitRow(&item, &item,
					item.row == s.cursorRow && s.cursorSide == workspaceDiffSideOld,
					item.row == s.cursorRow && s.cursorSide == workspaceDiffSideNew,
					rowHeight, theme, semantic)
				rows = append(rows, rowWidget)
				contentHeight += rowHeight
				index++
				continue
			}
			end := index
			for end < len(decorated) && decorated[end].line.Kind != "context" {
				end++
			}
			deletions, additions := make([]workspaceDiffRenderedLine, 0), make([]workspaceDiffRenderedLine, 0)
			for _, item := range decorated[index:end] {
				if item.line.Kind == "deletion" {
					deletions = append(deletions, item)
				} else {
					additions = append(additions, item)
				}
			}
			for pair := 0; pair < max(len(deletions), len(additions)); pair++ {
				var oldLine, newLine *workspaceDiffRenderedLine
				if pair < len(deletions) {
					oldLine = &deletions[pair]
				}
				if pair < len(additions) {
					newLine = &additions[pair]
				}
				rowHeight := targets[targetIndex].height
				targetIndex++
				rowWidget := s.workspaceDiffSplitRow(oldLine, newLine,
					oldLine != nil && oldLine.row == s.cursorRow,
					newLine != nil && newLine.row == s.cursorRow,
					rowHeight, theme, semantic)
				rows = append(rows, rowWidget)
				contentHeight += rowHeight
			}
			index = end
		}
	}
	return rows, contentWidth, contentHeight
}

func (s *workspaceDiffPaneState) workspaceDiffSplitRow(oldLine, newLine *workspaceDiffRenderedLine, oldSelected, newSelected bool, height int, theme ui.Theme, semantic SemanticTheme) ui.Widget {
	oldWidget := workspaceDiffSideWidget(oldLine, oldSelected, false, height, s.wrapLines, s.splitColumn, theme, semantic, func(ui.EventContext) {
		if oldLine != nil && (s.cursorRow != oldLine.row || s.cursorSide != workspaceDiffSideOld) {
			s.SetState(func() {
				s.cursorRow = oldLine.row
				s.cursorSide = workspaceDiffSideOld
			})
		}
	})
	newWidget := workspaceDiffSideWidget(newLine, newSelected, true, height, s.wrapLines, s.splitColumn, theme, semantic, func(ui.EventContext) {
		if newLine != nil && (s.cursorRow != newLine.row || s.cursorSide != workspaceDiffSideNew) {
			s.SetState(func() {
				s.cursorRow = newLine.row
				s.cursorSide = workspaceDiffSideNew
			})
		}
	})
	oldWidth, newWidth := workspaceDiffSplitWidths(s.viewportWidth)
	row := ui.SizedBox{Width: oldWidth + newWidth + 1, Height: height, Child: ui.Flex{Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMax, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Width: oldWidth, Height: height, Child: oldWidget},
		ui.Text{Value: strings.TrimSuffix(strings.Repeat(glyphTableSeparator+"\n", height), "\n"), Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: height},
		ui.SizedBox{Width: newWidth, Height: height, Child: newWidget},
	}}}
	return row
}

func workspaceDiffSliceSpans(spans []ui.TextSpan, columns int) []ui.TextSpan {
	if columns <= 0 {
		return spans
	}
	result := make([]ui.TextSpan, 0, len(spans))
	remaining := columns
	for _, span := range spans {
		if remaining <= 0 {
			result = append(result, span)
			continue
		}
		iterator := uucode.NewGraphemeWidthIterator(span.Text)
		start := len(span.Text)
		for {
			grapheme, ok := iterator.Next()
			if !ok {
				break
			}
			if remaining < grapheme.Width {
				start = grapheme.End
				remaining = 0
				break
			}
			remaining -= grapheme.Width
			start = grapheme.End
			if remaining == 0 {
				break
			}
		}
		if start < len(span.Text) {
			span.Text = span.Text[start:]
			result = append(result, span)
		}
	}
	return result
}

func workspaceDiffSideWidget(item *workspaceDiffRenderedLine, selected, actionSide bool, height int, wrap bool, columnOffset int, theme ui.Theme, semantic SemanticTheme, moveCursor ui.VoidCallback) ui.Widget {
	if item == nil {
		return ui.SizedBox{Height: height, Child: ui.Text{Value: "", Style: ui.Style{Background: theme.Background}, MaxLines: 1}}
	}
	line := item.line
	number, marker := "", " "
	if actionSide && line.NewLine != nil {
		number = fmt.Sprintf("%d", *line.NewLine)
	} else if !actionSide && line.OldLine != nil {
		number = fmt.Sprintf("%d", *line.OldLine)
	}
	contentBackground, gutterBackground := theme.Background, theme.Background
	switch line.Kind {
	case "addition":
		marker = "+"
		contentBackground = semantic.Token(kittheme.TokenDiffAddedContentBackground)
		gutterBackground = semantic.Token(kittheme.TokenDiffAddedLineNumberBg)
	case "deletion":
		marker = "−"
		contentBackground = semantic.Token(kittheme.TokenDiffRemovedContentBg)
		gutterBackground = semantic.Token(kittheme.TokenDiffRemovedLineNumberBg)
	}
	gutterStyle := ui.Style{Foreground: theme.MutedForeground, Background: gutterBackground}
	indicator, indicatorStyle := "   ", gutterStyle
	if selected {
		indicator = " + "
		indicatorStyle.Foreground = theme.Background
		indicatorStyle.Background = theme.Primary
	}
	spans := append([]ui.TextSpan(nil), item.syntax...)
	if len(spans) == 0 {
		spans = []ui.TextSpan{{Text: highlight.Sanitize(line.Content), Style: ui.Style{Foreground: theme.Foreground, Background: contentBackground}}}
	} else {
		for index := range spans {
			spans[index].Style.Background = contentBackground
		}
	}
	if !wrap && columnOffset > 0 {
		spans = workspaceDiffSliceSpans(spans, columnOffset)
	}
	row := ui.Widget(ui.Flex{Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMax, CrossAxisAlignment: ui.CrossAxisStart, Children: []ui.Widget{
		ui.RichText{Spans: []ui.TextSpan{{Text: fmt.Sprintf("%5s ", number), Style: gutterStyle}, {Text: indicator, Style: indicatorStyle}, {Text: marker + " ", Style: gutterStyle}}, SoftWrap: false},
		ui.Expanded(ui.RichText{Spans: spans, SoftWrap: wrap}),
	}})
	return mouseActivator{Child: row, OnHover: moveCursor, OnPressed: moveCursor}
}

func workspaceDiffSyntaxLines(result highlight.Result, semantic SemanticTheme) [][]ui.TextSpan {
	lines := strings.Split(result.Source, "\n")
	if strings.HasSuffix(result.Source, "\n") {
		lines = lines[:len(lines)-1]
	}
	rows := make([][]ui.TextSpan, 0, len(lines))
	offset, captureIndex := 0, 0
	for _, line := range lines {
		lineEnd := offset + len(line)
		position := offset
		spans := make([]ui.TextSpan, 0, 4)
		for captureIndex < len(result.Spans) && result.Spans[captureIndex].End <= offset {
			captureIndex++
		}
		for next := captureIndex; next < len(result.Spans); next++ {
			capture := result.Spans[next]
			if capture.Start >= lineEnd {
				break
			}
			start := max(position, max(offset, capture.Start))
			end := min(lineEnd, capture.End)
			if start > position {
				spans = append(spans, syntaxTextSpan(result.Source[position:start], highlight.Text, semantic))
			}
			if end > start {
				spans = append(spans, syntaxTextSpan(result.Source[start:end], capture.Role, semantic))
				position = end
			}
		}
		if position < lineEnd {
			spans = append(spans, syntaxTextSpan(result.Source[position:lineEnd], highlight.Text, semantic))
		} else if line == "" {
			spans = append(spans, syntaxTextSpan("", highlight.Text, semantic))
		}
		rows = append(rows, spans)
		offset = lineEnd + 1
	}
	return rows
}

func workspaceDiffLineWidget(line protocol.DiffLine, syntaxSpans []ui.TextSpan, selected bool, theme ui.Theme, semantic SemanticTheme, onHover, onPressed ui.VoidCallback) ui.Widget {
	oldNumber, newNumber, marker := "", "", " "
	if line.OldLine != nil {
		oldNumber = fmt.Sprintf("%d", *line.OldLine)
	}
	if line.NewLine != nil {
		newNumber = fmt.Sprintf("%d", *line.NewLine)
	}
	contentBackground, gutterBackground := theme.Background, theme.Background
	switch line.Kind {
	case "addition":
		marker = "+"
		contentBackground = semantic.Token(kittheme.TokenDiffAddedContentBackground)
		gutterBackground = semantic.Token(kittheme.TokenDiffAddedLineNumberBg)
	case "deletion":
		marker = "−"
		contentBackground = semantic.Token(kittheme.TokenDiffRemovedContentBg)
		gutterBackground = semantic.Token(kittheme.TokenDiffRemovedLineNumberBg)
	}
	gutterStyle := ui.Style{Foreground: theme.MutedForeground, Background: gutterBackground}
	contentStyle := ui.Style{Foreground: theme.Foreground, Background: contentBackground}
	indicator := "   "
	indicatorStyle := gutterStyle
	if selected {
		indicator = " + "
		indicatorStyle.Foreground = theme.Background
		indicatorStyle.Background = theme.Primary
	}
	lineNumbers := fmt.Sprintf("%5s %5s ", oldNumber, newNumber)
	diffMarker := marker + " "
	if len(syntaxSpans) == 0 {
		syntaxSpans = []ui.TextSpan{{Text: highlight.Sanitize(line.Content), Style: contentStyle}}
	} else {
		syntaxSpans = append([]ui.TextSpan(nil), syntaxSpans...)
		for index := range syntaxSpans {
			syntaxSpans[index].Style.Background = contentBackground
		}
	}
	row := ui.Widget(ui.SizedBox{Height: 1, Child: ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
		ui.RichText{Spans: []ui.TextSpan{{Text: lineNumbers, Style: gutterStyle}, {Text: indicator, Style: indicatorStyle}, {Text: diffMarker, Style: gutterStyle}}, SoftWrap: false},
		ui.Expanded(ui.RichText{Spans: syntaxSpans, SoftWrap: false}),
	}}})
	return mouseActivator{Child: row, OnHover: onHover, OnPressed: onPressed}
}

func (s *workspaceDiffPaneState) footerText() string {
	w := s.Widget().(workspaceDiffPane)
	if s.isFrozen(w) {
		return "Frozen evidence " + glyphMiddleDot + " navigation only"
	}
	horizontalHint := "←→ columns"
	parts := []string{"↑↓ lines", horizontalHint, "[ ] files", "{ } hunks", "r refresh"}
	if s.viewportWidth >= workspaceDiffSplitBreakpoint {
		parts[1] = "←→ pan"
		wrapHint := "w wrap"
		if s.wrapLines {
			wrapHint = "w clip"
			parts = append(parts[:2], append([]string{wrapHint}, parts[2:]...)...)
		} else {
			parts[1] = "←→/H/L pan"
			parts = append(parts[:2], append([]string{wrapHint}, parts[2:]...)...)
		}
	}
	if s.loadingMore {
		parts = append([]string{"Loading more diff content…"}, parts...)
	} else if s.fileNextCursor != "" {
		parts = append([]string{"m more"}, parts...)
	}
	if s.observationCursor != "" {
		parts = append([]string{"More changed files available"}, parts...)
	}
	if !s.observation.Complete && s.observation.Revision != "" {
		parts = append([]string{"Incomplete observation"}, parts...)
	}
	return strings.Join(parts, " "+glyphMiddleDot+" ")
}
