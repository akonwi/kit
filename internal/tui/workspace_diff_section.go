package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/akonwi/kit/internal/highlight"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/rockorager/go-uucode"
	"go.rockorager.dev/vaxis/ui"
)

// workspaceDiffFileState owns one revision-bound section of the shared document.
// It is not a widget: the pane owns its lifecycle, focus, and scroll controller.
type workspaceDiffFileKey struct {
	targetID       string
	targetRevision string
	path           string
	fileRevision   string
}

type workspaceDiffMeasureKey struct {
	width                                               int
	split, wrap                                         bool
	generation, annotations                             uint64
	hunks, lines                                        int
	loaded, more, failed                                bool
	commenting, pending, loading, commentError, changes bool
	commentID                                           uint64
}

type workspaceDiffFileState struct {
	measureKey                    workspaceDiffMeasureKey
	measureValid                  bool
	measuredWidth, measuredHeight int
	key                           workspaceDiffFileKey
	file                          protocol.DiffFileSummary
	disposed                      bool
	pane                          *workspaceDiffPaneState
	index                         int
	offset                        int
	height                        int
	loaded                        bool
	pendingHunk                   bool
	pendingEdge                   int
	errorText                     string
	generation                    uint64
	cancel                        context.CancelFunc
	resultMu                      sync.Mutex
	pendingFile                   *workspaceDiffFileResult
	hunks                         []protocol.DiffHunk
	cursorRow                     int
	cursorSide                    workspaceDiffSide
	splitColumn                   int
	splitTargetsCached            []workspaceDiffSplitTarget
	splitNavigationCached         []workspaceDiffNavigationLine
	splitNavigationValid          bool
	splitTargetsWidth             int
	splitTargetsWrap              bool
	splitMaxColumn                int
	splitTargetsValid             bool
	lineRows                      []int
	hunkRows                      []int
	loadingFile                   bool
	loadingMore                   bool
	fileNextCursor                string
	highlightGeneration           uint64
	highlightCancel               context.CancelFunc
	pendingHighlight              *workspaceDiffHighlightResult
	oldHighlighted                highlight.Result
	newHighlighted                highlight.Result
	highlightReady                bool
	commenting                    bool
	commentBody                   string
	commentPending                bool
	commentLoading                bool
	commentError                  string
	commentAnchor                 protocol.WorkingTreeDiffAnnotationAnchor
	commentAnnotationID           uint64
	selectionAnchor               protocol.WorkingTreeDiffAnnotationAnchor
	mouseSelectionGeneration      uint64
	commentLoadCancel             func()
	commentOperation              uint64
}

func (s *workspaceDiffFileState) stopHighlight() {
	if s.highlightCancel != nil {
		s.highlightCancel()
		s.highlightCancel = nil
		s.highlightGeneration++
	}
}

func (s *workspaceDiffFileState) startFileLoad() {
	w := s.pane.Widget().(workspaceDiffPane)
	if s.disposed || !w.Presentation.Active || !w.Presentation.Visible || s.pane.isFrozen(w) {
		return
	}
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

func (s *workspaceDiffFileState) requestFilePage(cursor string, appendPage bool) {
	w := s.pane.Widget().(workspaceDiffPane)
	if s.disposed || s.key.targetID != s.pane.observation.Target.ID || s.key.targetRevision != s.pane.observation.Revision || w.Diff == nil || !w.Presentation.Active || !w.Presentation.Visible || s.pane.isFrozen(w) || s.index < 0 || s.index >= len(s.pane.files) {
		return
	}
	if s.loadingFile || s.loadingMore {
		return
	}
	s.stopRead()
	s.generation++
	generation := s.generation
	s.loadingFile = !appendPage
	s.loadingMore = appendPage
	s.errorText = ""
	file := s.file
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.pane.Context().Runtime().Dispatch
	}
	annotationID := uint64(0)
	if s.pane.pinnedEvidence {
		annotationID = w.Descriptor.AnnotationID
	}
	input := protocol.ReadFileDiffInput{
		TargetID: s.key.targetID, TargetRevision: s.key.targetRevision,
		Path: file.Path, ExpectedFileRevision: file.FileRevision, AnnotationID: annotationID, Cursor: cursor,
	}
	s.pane.queueWork(ctx, func() {
		page, err := w.Diff.ReadFileDiff(ctx, input)
		if ctx.Err() != nil {
			return
		}
		s.resultMu.Lock()
		if s.pendingFile == nil || generation >= s.pendingFile.generation {
			s.pendingFile = &workspaceDiffFileResult{generation: generation, page: page, append: appendPage, err: err}
		}
		s.resultMu.Unlock()
		s.dispatchPendingResults(dispatch)
	})
}

func (s *workspaceDiffFileState) completeFileLoad(page protocol.FileDiffPage, appendPage bool, err error) {
	s.cancel = nil
	s.loadingFile = false
	s.loadingMore = false
	if err != nil {
		s.errorText = workspaceDiffErrorText(err)
		return
	}
	expected := s.file
	if page.Observation.Target.ID != s.key.targetID || page.Observation.Revision != s.key.targetRevision || page.File.Path != expected.Path || page.File.FileRevision != expected.FileRevision {
		s.errorText = "Diff evidence changed; refresh to continue"
		return
	}
	s.errorText = ""
	s.loaded = true
	if appendPage {
		s.appendHunks(page.Hunks)
	} else {
		if s.pane.pinnedEvidence {
			s.pane.observation = page.Observation
		}
		s.file = page.File
		s.hunks = append([]protocol.DiffHunk(nil), page.Hunks...)
	}
	s.invalidateSplitTargets()
	s.fileNextCursor = page.NextCursor
	s.rebuildHunkRows()
	descriptor := s.pane.Widget().(workspaceDiffPane).Descriptor
	if descriptor.Path != s.file.Path {
		descriptor.RevealStartLine, descriptor.RevealEndLine = 0, 0
	}
	refreshReveal := s.pane.refreshPath == s.key.path && s.pane.refreshLine > 0
	if refreshReveal {
		descriptor.DiffSide = "new"
		if s.pane.refreshSide == workspaceDiffSideOld {
			descriptor.DiffSide = "old"
		}
		descriptor.RevealStartLine = s.pane.refreshLine
		descriptor.RevealEndLine = s.pane.refreshLine
		s.cursorSide = s.pane.refreshSide
	}
	foundRevealStart, foundRevealEnd := false, descriptor.RevealEndLine <= 0 || descriptor.RevealEndLine == descriptor.RevealStartLine
	if reveal := descriptor.RevealStartLine; reveal > 0 {
		row := 0
		found := false
		for _, hunk := range s.hunks {
			row++
			for _, line := range hunk.Lines {
				matches := descriptor.DiffSide == "old" && line.OldLine != nil && *line.OldLine == reveal || descriptor.DiffSide == "new" && line.NewLine != nil && *line.NewLine == reveal
				if descriptor.DiffSide == "" {
					matches = line.OldLine != nil && *line.OldLine == reveal || line.NewLine != nil && *line.NewLine == reveal
				}
				if matches {
					s.setCursorRow(row)
					found = true
					break
				}
				row++
			}
			if found {
				break
			}
		}
		foundRevealStart = found
		if end := descriptor.RevealEndLine; end > 0 {
			for _, hunk := range s.hunks {
				for _, line := range hunk.Lines {
					foundRevealEnd = foundRevealEnd || descriptor.DiffSide == "old" && line.OldLine != nil && *line.OldLine == end || descriptor.DiffSide == "new" && line.NewLine != nil && *line.NewLine == end
					if descriptor.DiffSide == "" {
						foundRevealEnd = foundRevealEnd || line.OldLine != nil && *line.OldLine == end || line.NewLine != nil && *line.NewLine == end
					}
				}
			}
		}
	}
	if (descriptor.DiffTargetID != "" || refreshReveal) && descriptor.RevealStartLine > 0 && (!foundRevealStart || !foundRevealEnd) && s.fileNextCursor != "" {
		s.requestFilePage(s.fileNextCursor, true)
		return
	}
	if s.cursorRow == 0 && len(s.hunkRows) > 0 {
		s.setCursorRow(s.hunkRows[0])
	}
	if refreshReveal {
		s.pane.refreshLine = 0
		s.pane.refreshPath = ""
	}
	if s.cursorVisible() && (refreshReveal || descriptor.RevealStartLine > 0) {
		s.pane.cursorRevealPending, s.pane.revealPendingLayout = true, true
	}
	s.applyPendingEdge()
	s.startHighlight()
}

func (s *workspaceDiffFileState) startHighlight() {
	w := s.pane.Widget().(workspaceDiffPane)
	if s.disposed || !w.Presentation.Active || !w.Presentation.Visible || s.pane.isFrozen(w) || len(s.hunks) == 0 || s.index < 0 || s.index >= len(s.pane.files) {
		return
	}
	s.stopHighlight()
	s.highlightReady = false
	s.highlightGeneration++
	generation := s.highlightGeneration
	oldSource, newSource := workspaceDiffHighlightSources(s.hunks)
	file := s.file
	ctx, cancel := context.WithCancel(context.Background())
	s.highlightCancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.pane.Context().Runtime().Dispatch
	}
	highlighter := w.Highlighter
	if highlighter == nil {
		highlighter = highlight.Default
	}
	s.pane.queueHighlight(ctx, func() {
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
		s.dispatchPendingResults(dispatch)
	})
}

func (s *workspaceDiffFileState) appendHunks(next []protocol.DiffHunk) {
	if len(s.hunks) > 0 && len(next) > 0 && next[0].ContinuedBefore && s.hunks[len(s.hunks)-1].ContinuedAfter {
		last := &s.hunks[len(s.hunks)-1]
		last.Lines = append(last.Lines, next[0].Lines...)
		last.ContinuedAfter = next[0].ContinuedAfter
		next = next[1:]
	}
	s.hunks = append(s.hunks, next...)
}

func (s *workspaceDiffFileState) rebuildHunkRows() {
	s.invalidateSplitTargets()
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

func (s *workspaceDiffFileState) setCursorRow(row int) {
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

func (s *workspaceDiffFileState) lineAtRow(wanted int) (protocol.DiffLine, bool) {
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

func (s *workspaceDiffFileState) moveRangeLine(delta int) {
	if delta == 0 || !s.selectionActive() {
		return
	}
	side := s.selectionAnchor.Side
	rows := make([]int, 0, len(s.lineRows))
	row := 0
	for _, hunk := range s.hunks {
		row++
		for _, line := range hunk.Lines {
			coordinate := line.NewLine
			if side == "old" {
				coordinate = line.OldLine
			}
			if coordinate != nil {
				rows = append(rows, row)
			}
			row++
		}
	}
	selected := 0
	for index, candidate := range rows {
		if candidate == s.cursorRow {
			selected = index
			break
		}
	}
	next := max(0, min(len(rows)-1, selected+delta))
	cursorSide := workspaceDiffSideNew
	if side == "old" {
		cursorSide = workspaceDiffSideOld
	}
	if next != selected && s.setRangeCursor(rows[next], cursorSide) {
		s.revealCursor()
	}
	if delta > 0 && len(rows)-next <= 10 {
		s.loadMoreFileDiff()
	}
}

func (s *workspaceDiffFileState) moveLine(delta int) {
	if s.selectionActive() {
		s.moveRangeLine(delta)
		return
	}
	if s.pane.splitLayout() {
		s.moveSplitLine(delta)
	} else {
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
}

type workspaceDiffSplitTarget struct {
	oldRow, newRow int
	visualRow      int
	height         int
}

func (s *workspaceDiffFileState) invalidateSplitTargets() {
	s.splitTargetsCached = nil
	s.splitNavigationCached = nil
	s.splitNavigationValid = false
	s.splitMaxColumn = 0
	s.splitTargetsValid = false
}

func (s *workspaceDiffFileState) splitTargets() []workspaceDiffSplitTarget {
	if s.splitTargetsValid && s.splitTargetsWidth == s.pane.viewportWidth && s.splitTargetsWrap == s.pane.wrapLines {
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
				height := workspaceDiffSplitPairHeight(&line, &line, s.pane.viewportWidth, s.pane.wrapLines)
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
				target.height = workspaceDiffSplitPairHeight(oldLine, newLine, s.pane.viewportWidth, s.pane.wrapLines)
				targets = append(targets, target)
				visualRow += target.height
			}
			index = end
		}
	}
	oldWidth, newWidth := workspaceDiffSplitWidths(s.pane.viewportWidth)
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
	s.splitTargetsWidth = s.pane.viewportWidth
	s.splitTargetsWrap = s.pane.wrapLines
	s.splitTargetsValid = true
	return targets
}

type workspaceDiffNavigationLine struct {
	row       int
	side      workspaceDiffSide
	forceSide bool
}

func (s *workspaceDiffFileState) splitNavigationLines() []workspaceDiffNavigationLine {
	if s.splitNavigationValid {
		return s.splitNavigationCached
	}
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
	s.splitNavigationCached = lines
	s.splitNavigationValid = true
	return lines
}

func (s *workspaceDiffFileState) moveSplitLine(delta int) {
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

func (s *workspaceDiffFileState) panSplit(columns int) {
	if s.pane.wrapLines || columns == 0 {
		return
	}
	s.splitColumn = max(0, min(s.maxSplitColumn(), s.splitColumn+columns))
}

func (s *workspaceDiffFileState) maxSplitColumn() int {
	s.splitTargets()
	return s.splitMaxColumn
}

func (s *workspaceDiffFileState) loadMoreFileDiff() {
	if s.fileNextCursor != "" && !s.loadingMore {
		s.requestFilePage(s.fileNextCursor, true)
	}
}

func (s *workspaceDiffFileState) annotationHeightAfter(side string, line int) int {
	w := s.pane.Widget().(workspaceDiffPane)
	height := 0
	for _, annotation := range w.Annotations {
		anchor := annotation.Anchor.WorkingTreeDiff
		if !annotation.Stale && s.annotationMatches(anchor, side, line) && anchor.EndLine == line && !(s.commenting && annotation.ID == s.commentAnnotationID) {
			height += len(workspaceAnnotationBodyLines(annotation.BodyPreview))
		}
	}
	if s.commenting && s.commentAnchor.Side == side && s.commentAnchor.EndLine == line {
		height += s.diffCommentEditorHeight()
	}
	return height
}

func (s *workspaceDiffFileState) cursorVisualRow() int {
	if !s.pane.splitLayout() {
		logicalRow, physicalRow := 0, 0
		for _, hunk := range s.hunks {
			logicalRow++
			physicalRow++
			for _, line := range hunk.Lines {
				if logicalRow == s.cursorRow {
					return physicalRow
				}
				logicalRow++
				physicalRow += s.unifiedLineHeight(line)
				if line.OldLine != nil {
					physicalRow += s.annotationHeightAfter("old", *line.OldLine)
				}
				if line.NewLine != nil {
					physicalRow += s.annotationHeightAfter("new", *line.NewLine)
				}
			}
		}
		return physicalRow
	}
	extra := 0
	for _, target := range s.splitTargets() {
		if s.cursorSide == workspaceDiffSideOld && target.oldRow == s.cursorRow || s.cursorSide == workspaceDiffSideNew && target.newRow == s.cursorRow {
			return target.visualRow + extra
		}
		if target.oldRow >= 0 {
			if line, ok := s.lineAtRow(target.oldRow); ok && line.OldLine != nil {
				extra += s.annotationHeightAfter("old", *line.OldLine)
			}
		}
		if target.newRow >= 0 {
			if line, ok := s.lineAtRow(target.newRow); ok && line.NewLine != nil {
				extra += s.annotationHeightAfter("new", *line.NewLine)
			}
		}
	}
	return extra
}

func (s *workspaceDiffFileState) revealCursor() {
	if !s.pane.scroll.Attached() {
		return
	}
	vertical := s.pane.scroll.Metrics(ui.ScrollVertical)
	viewportHeight := max(1, vertical.ViewportHeight)
	row, target := s.offset+2+s.cursorVisualRow(), vertical.ScrollOffset
	if row < vertical.ScrollOffset {
		target = row
	} else if row >= vertical.ScrollOffset+viewportHeight {
		target = row - viewportHeight + 1
	}
	horizontal := s.pane.horizontalOffset()
	currentHorizontal := s.pane.scroll.Metrics(ui.ScrollHorizontal).ScrollOffset
	if target != vertical.ScrollOffset || horizontal != currentHorizontal {
		s.pane.scroll.ScrollTo(horizontal, max(0, target))
	}
}

func (s *workspaceDiffFileState) moveHunk(delta int) {
	if s.selectionActive() {
		s.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
		s.pane.reportFollowCWD()
		if s.pane.changesAvailable {
			s.pane.applyAvailableRefresh()
			return
		}
	}
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

func (s *workspaceDiffFileState) currentDiffAnchor() (protocol.WorkingTreeDiffAnnotationAnchor, bool) {
	if s.index < 0 || s.index >= len(s.pane.files) || s.cursorRow <= 0 {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	line, ok := s.lineAtRow(s.cursorRow)
	if !ok {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	lineNumber := line.NewLine
	side := "new"
	if s.cursorSide == workspaceDiffSideOld {
		lineNumber = line.OldLine
		side = "old"
	}
	if lineNumber == nil {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	file := s.file
	return protocol.WorkingTreeDiffAnnotationAnchor{
		TargetID: s.key.targetID, TargetRevision: s.key.targetRevision,
		Path: file.Path, FileRevision: file.FileRevision, Side: side,
		StartLine: *lineNumber, EndLine: *lineNumber,
	}, true
}

func (s *workspaceDiffFileState) selectionActive() bool {
	return s.selectionAnchor.TargetID != ""
}

func (s *workspaceDiffFileState) selectedDiffAnchor() (protocol.WorkingTreeDiffAnnotationAnchor, bool) {
	current, ok := s.currentDiffAnchor()
	if !ok {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	if !s.selectionActive() {
		return current, true
	}
	start := s.selectionAnchor
	if start.TargetID != current.TargetID || start.TargetRevision != current.TargetRevision || start.Path != current.Path || start.FileRevision != current.FileRevision || start.Side != current.Side {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	start.StartLine = min(start.StartLine, current.StartLine)
	start.EndLine = max(s.selectionAnchor.EndLine, current.EndLine)
	if start.EndLine-start.StartLine+1 > protocol.MaxAnnotationRangeLines {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	covered := make(map[int]struct{}, start.EndLine-start.StartLine+1)
	for _, hunk := range s.hunks {
		for _, line := range hunk.Lines {
			coordinate := line.NewLine
			if start.Side == "old" {
				coordinate = line.OldLine
			}
			if coordinate != nil && *coordinate >= start.StartLine && *coordinate <= start.EndLine {
				covered[*coordinate] = struct{}{}
			}
		}
	}
	if len(covered) != start.EndLine-start.StartLine+1 {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	return start, true
}

func (s *workspaceDiffFileState) setRangeCursor(row int, side workspaceDiffSide) bool {
	previousRow, previousSide := s.cursorRow, s.cursorSide
	s.cursorRow, s.cursorSide = row, side
	if _, ok := s.selectedDiffAnchor(); !ok {
		s.cursorRow, s.cursorSide = previousRow, previousSide
		return false
	}
	return true
}

func (s *workspaceDiffFileState) lineInSelection(side string, line int) bool {
	if !s.selectionActive() {
		return false
	}
	anchor, ok := s.selectedDiffAnchor()
	return ok && anchor.Side == side && line >= anchor.StartLine && line <= anchor.EndLine
}

func (s *workspaceDiffFileState) beginGutterRange(w workspaceDiffPane, row int, side workspaceDiffSide) {
	if s.commenting || s.commentPending || w.OnCreateAnnotation == nil {
		return
	}
	s.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
	s.cursorRow, s.cursorSide = row, side
	anchor, ok := s.currentDiffAnchor()
	if !ok {
		s.pane.reportFollowCWD()
		return
	}
	s.selectionAnchor = anchor
	s.pane.reportFollowCWD()
	s.mouseSelectionGeneration = w.MouseGestures.Generation()
}

func (s *workspaceDiffFileState) extendGutterRange(w workspaceDiffPane, row int, side workspaceDiffSide) {
	if !s.selectionActive() || w.MouseGestures.Generation() != s.mouseSelectionGeneration {
		return
	}
	expectedSide := workspaceDiffSideNew
	if s.selectionAnchor.Side == "old" {
		expectedSide = workspaceDiffSideOld
	}
	if side == expectedSide {
		s.setRangeCursor(row, side)
	}
}

func (s *workspaceDiffFileState) finishGutterRange(w workspaceDiffPane) {
	if w.Presentation.KeyboardBlocked || !s.selectionActive() || w.MouseGestures.ReleasedGeneration() != s.mouseSelectionGeneration {
		return
	}
	s.mouseSelectionGeneration = 0
	s.beginComment(w)
}

func (s *workspaceDiffFileState) annotationAtCursor(w workspaceDiffPane) (protocol.AnnotationSummary, bool) {
	anchor, ok := s.currentDiffAnchor()
	if !ok {
		return protocol.AnnotationSummary{}, false
	}
	for _, annotation := range w.Annotations {
		candidate := annotation.Anchor.WorkingTreeDiff
		if candidate != nil && !annotation.Stale && candidate.TargetID == anchor.TargetID && candidate.TargetRevision == anchor.TargetRevision && candidate.Path == anchor.Path && candidate.FileRevision == anchor.FileRevision && candidate.Side == anchor.Side && anchor.StartLine >= candidate.StartLine && anchor.StartLine <= candidate.EndLine {
			return annotation, true
		}
	}
	return protocol.AnnotationSummary{}, false
}

func (s *workspaceDiffFileState) beginComment(w workspaceDiffPane) {
	if s.commenting || w.OnCreateAnnotation == nil {
		return
	}
	anchor, ok := s.selectedDiffAnchor()
	if !ok {
		return
	}
	s.pane.SetState(func() {
		s.commentOperation++
		s.commenting = true
		s.commentBody = ""
		s.commentPending = false
		s.commentLoading = false
		s.commentError = ""
		s.commentAnchor = anchor
		s.commentAnnotationID = 0
		s.pane.reportFollowCWD()
	})
}

func (s *workspaceDiffFileState) beginEditComment(w workspaceDiffPane, annotation protocol.AnnotationSummary) {
	anchor := annotation.Anchor.WorkingTreeDiff
	if s.commenting || annotation.Stale || anchor == nil || w.OnLoadAnnotation == nil {
		return
	}
	var operation uint64
	s.pane.SetState(func() {
		s.commentOperation++
		operation = s.commentOperation
		s.commenting = true
		s.commentBody = annotation.BodyPreview
		s.commentPending = false
		s.commentLoading = true
		s.commentError = ""
		s.commentAnchor = *anchor
		s.commentAnnotationID = annotation.ID
		s.pane.reportFollowCWD()
	})
	s.commentLoadCancel = w.OnLoadAnnotation(annotation.ID, func(body string, err error) {
		if s.disposed || s.pane.disposed || operation != s.commentOperation {
			return
		}
		s.pane.SetState(func() {
			s.commentLoadCancel = nil
			s.commentLoading = false
			if err != nil {
				s.commentError = annotationErrorText(err)
				return
			}
			s.commentBody = body
		})
	})
}

func (s *workspaceDiffFileState) closeComment() {
	s.commentOperation++
	if s.commentLoadCancel != nil {
		s.commentLoadCancel()
		s.commentLoadCancel = nil
	}
	s.commenting = false
	s.commentBody = ""
	s.commentPending = false
	s.commentLoading = false
	s.commentError = ""
	s.commentAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
	s.commentAnnotationID = 0
	s.pane.reportFollowCWD()
	s.pane.applyAvailableRefresh()
}

func (s *workspaceDiffFileState) submitComment(w workspaceDiffPane, body string) {
	if !s.commenting || s.commentPending || s.commentLoading || strings.TrimSpace(body) == "" {
		return
	}
	s.pane.SetState(func() {
		s.commentPending = true
		s.commentBody = body
		s.commentError = ""
	})
	done := func(err error) {
		if s.disposed || s.pane.disposed {
			return
		}
		s.pane.SetState(func() {
			if err != nil {
				s.commentPending = false
				s.commentError = annotationErrorText(err)
				return
			}
			s.closeComment()
			s.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
			s.pane.reportFollowCWD()
			s.pane.applyAvailableRefresh()
		})
	}
	if s.commentAnnotationID != 0 {
		if w.OnUpdateAnnotation == nil {
			done(errors.New("annotation updates are unavailable"))
			return
		}
		w.OnUpdateAnnotation(s.commentAnnotationID, body, done)
		return
	}
	anchor := s.commentAnchor
	w.OnCreateAnnotation(protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &anchor}, body, done)
}

func (s *workspaceDiffFileState) annotationMatches(anchor *protocol.WorkingTreeDiffAnnotationAnchor, side string, line int) bool {
	if anchor == nil || s.index < 0 || s.index >= len(s.pane.files) {
		return false
	}
	file := s.file
	return anchor.TargetID == s.key.targetID && anchor.TargetRevision == s.key.targetRevision &&
		anchor.Path == file.Path && anchor.FileRevision == file.FileRevision && anchor.Side == side && line >= anchor.StartLine && line <= anchor.EndLine
}

func (s *workspaceDiffFileState) diffAnnotationRows(theme ui.Theme, w workspaceDiffPane, side string, line, width int) []ui.Widget {
	rows := make([]ui.Widget, 0)
	for _, annotation := range w.Annotations {
		anchor := annotation.Anchor.WorkingTreeDiff
		if annotation.Stale || !s.annotationMatches(anchor, side, line) || anchor.EndLine != line || annotation.ID == s.commentAnnotationID && s.commenting {
			continue
		}
		bodyLines := workspaceAnnotationBodyLines(annotation.BodyPreview)
		for index, bodyLine := range bodyLines {
			row := ui.Widget(ui.SizedBox{Width: width, Height: 1, Child: ui.Text{
				Value: "   " + glyphDiamond + " " + bodyLine,
				Style: ui.Style{Foreground: theme.AccentText, Background: theme.Surface}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis,
			}})
			if index == 0 && w.OnLoadAnnotation != nil {
				captured := annotation
				row = mouseActivator{Child: row, OnPressed: func(ui.EventContext) { s.interact(func() { s.beginEditComment(w, captured) }) }}
			}
			rows = append(rows, row)
		}
	}
	if s.commenting && s.commentAnchor.Side == side && s.commentAnchor.EndLine == line {
		rows = append(rows, s.diffCommentEditor(theme, w, width))
	}
	return rows
}

func (s *workspaceDiffFileState) diffCommentEditorHeight() int {
	height := 5
	if s.commentPending || s.commentLoading {
		height++
	}
	if s.commentError != "" {
		height++
	}
	if s.pane.changesAvailable {
		height++
	}
	return height
}

func (s *workspaceDiffFileState) diffCommentEditor(theme ui.Theme, w workspaceDiffPane, width int) ui.Widget {
	height := s.diffCommentEditorHeight()
	children := []ui.Widget{ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Surface}}}
	if s.commentError != "" {
		children = append(children, ui.Text{Value: "Could not save: " + s.commentError, Style: ui.Style{Foreground: theme.DangerText}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis})
	}
	if s.pane.changesAvailable {
		children = append(children, ui.Text{Value: "Changes available · refreshes after editing", Style: ui.Style{Foreground: theme.Warning}, MaxLines: 1})
	}
	if s.commentPending || s.commentLoading {
		status := "Saving…"
		if s.commentLoading {
			status = "Loading…"
		}
		children = append(children, ui.Text{Value: s.commentBody, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 2, SoftWrap: true}, spinnerWithLabel(status, ui.Style{Foreground: theme.MutedForeground}))
	} else {
		children = append(children, messageComposer{
			Value: s.commentBody, Placeholder: "Write a comment…", MaxHeight: 2,
			OnChanged: func(_ ui.EventContext, value string) {
				s.interact(func() { s.commentBody, s.commentError = value, "" })
			},
			OnSubmitted: func(_ ui.EventContext, value string) { s.submitComment(w, value) },
		})
	}
	children = append(children,
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Surface}},
		ui.Text{Value: "enter save · shift+enter newline · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1},
	)
	return ui.SizedBox{Width: width, Height: height, Child: ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: theme.Surface}},
		ui.Padding(ui.Insets{Left: 3, Right: 1}, ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, MainAxisSize: ui.MainAxisSizeMin, Children: children}),
	)}
}

func (s *workspaceDiffFileState) diffRows(theme ui.Theme, semantic SemanticTheme) ([]ui.Widget, int, int) {
	w := s.pane.Widget().(workspaceDiffPane)
	rows := make([]ui.Widget, 0)
	visualRow, contentWidth, contentHeight := 0, 1, 0
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
		contentHeight++
		for _, line := range hunk.Lines {
			if !s.pane.wrapLines {
				contentWidth = max(contentWidth, workspaceDiffUnifiedLineWidth(line))
			} else {
				contentWidth = max(contentWidth, s.pane.viewportWidth)
			}
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
				if !s.cursorVisible() || s.cursorRow != row {
					s.interact(func() {
						if !s.selectionActive() {
							s.setCursorRow(row)
							return
						}
						side := s.cursorSide
						if line.Kind == "deletion" {
							side = workspaceDiffSideOld
						} else if line.Kind == "addition" {
							side = workspaceDiffSideNew
						}
						s.setRangeCursor(row, side)
					})
				}
			}
			lineSide := s.cursorSide
			if line.Kind == "deletion" {
				lineSide = workspaceDiffSideOld
			} else if line.Kind == "addition" {
				lineSide = workspaceDiffSideNew
			}
			gutterPress := func(ui.EventContext) {
				s.interact(func() { s.beginGutterRange(w, row, lineSide) })
			}
			gutterMotion := func(_ ui.EventContext, mouse ui.Mouse) {
				if mouse.Button == ui.MouseLeftButton {
					s.interact(func() { s.extendGutterRange(w, row, lineSide) })
				}
			}
			oldRangeSelected := line.OldLine != nil && s.lineInSelection("old", *line.OldLine)
			newRangeSelected := line.NewLine != nil && s.lineInSelection("new", *line.NewLine)
			lineHeight := s.unifiedLineHeight(line)
			rows = append(rows, workspaceDiffLineWidget(line, syntaxSpans, s.cursorVisible() && row == s.cursorRow, oldRangeSelected, newRangeSelected, theme, semantic, moveCursor, gutterPress, gutterMotion, workspaceDiffLineLayout{height: lineHeight, wrap: s.pane.wrapLines}))
			contentHeight += lineHeight
			visualRow++
			annotationWidth := max(contentWidth, s.pane.viewportWidth)
			if line.OldLine != nil {
				notes := s.diffAnnotationRows(theme, w, "old", *line.OldLine, annotationWidth)
				rows = append(rows, notes...)
				for _, note := range notes {
					if box, ok := note.(ui.SizedBox); ok {
						contentHeight += max(1, box.Height)
					} else {
						contentHeight++
					}
				}
			}
			if line.NewLine != nil {
				notes := s.diffAnnotationRows(theme, w, "new", *line.NewLine, annotationWidth)
				rows = append(rows, notes...)
				for _, note := range notes {
					if box, ok := note.(ui.SizedBox); ok {
						contentHeight += max(1, box.Height)
					} else {
						contentHeight++
					}
				}
			}
		}
	}
	return rows, contentWidth, contentHeight
}

type workspaceDiffRenderedLine struct {
	line   protocol.DiffLine
	row    int
	syntax []ui.TextSpan
}

func (s *workspaceDiffFileState) splitDiffRows(theme ui.Theme, semantic SemanticTheme) ([]ui.Widget, int, int) {
	rows := make([]ui.Widget, 0)
	visualRow, contentWidth, contentHeight := 0, max(1, s.pane.viewportWidth), 0
	oldWidth, newWidth := workspaceDiffSplitWidths(s.pane.viewportWidth)
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
					s.cursorVisible() && item.row == s.cursorRow && s.cursorSide == workspaceDiffSideOld,
					s.cursorVisible() && item.row == s.cursorRow && s.cursorSide == workspaceDiffSideNew,
					rowHeight, theme, semantic)
				rows = append(rows, rowWidget)
				contentHeight += rowHeight
				if item.line.OldLine != nil {
					notes := s.diffAnnotationRows(theme, s.pane.Widget().(workspaceDiffPane), "old", *item.line.OldLine, oldWidth)
					rows = append(rows, workspaceDiffSplitSupplementRows(notes, true, s.pane.viewportWidth, theme)...)
					contentHeight += workspaceDiffWidgetsHeight(notes)
				}
				if item.line.NewLine != nil {
					notes := s.diffAnnotationRows(theme, s.pane.Widget().(workspaceDiffPane), "new", *item.line.NewLine, newWidth)
					rows = append(rows, workspaceDiffSplitSupplementRows(notes, false, s.pane.viewportWidth, theme)...)
					contentHeight += workspaceDiffWidgetsHeight(notes)
				}
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
					s.cursorVisible() && oldLine != nil && oldLine.row == s.cursorRow,
					s.cursorVisible() && newLine != nil && newLine.row == s.cursorRow,
					rowHeight, theme, semantic)
				rows = append(rows, rowWidget)
				contentHeight += rowHeight
				if oldLine != nil && oldLine.line.OldLine != nil {
					notes := s.diffAnnotationRows(theme, s.pane.Widget().(workspaceDiffPane), "old", *oldLine.line.OldLine, oldWidth)
					rows = append(rows, workspaceDiffSplitSupplementRows(notes, true, s.pane.viewportWidth, theme)...)
					contentHeight += workspaceDiffWidgetsHeight(notes)
				}
				if newLine != nil && newLine.line.NewLine != nil {
					notes := s.diffAnnotationRows(theme, s.pane.Widget().(workspaceDiffPane), "new", *newLine.line.NewLine, newWidth)
					rows = append(rows, workspaceDiffSplitSupplementRows(notes, false, s.pane.viewportWidth, theme)...)
					contentHeight += workspaceDiffWidgetsHeight(notes)
				}
			}
			index = end
		}
	}
	return rows, contentWidth, contentHeight
}

func (s *workspaceDiffFileState) workspaceDiffSplitRow(oldLine, newLine *workspaceDiffRenderedLine, oldSelected, newSelected bool, height int, theme ui.Theme, semantic SemanticTheme) ui.Widget {
	w := s.pane.Widget().(workspaceDiffPane)
	moveOld := func(ui.EventContext) {
		if oldLine != nil && (!s.cursorVisible() || s.cursorRow != oldLine.row || s.cursorSide != workspaceDiffSideOld) {
			s.interact(func() {
				if s.selectionActive() {
					s.setRangeCursor(oldLine.row, workspaceDiffSideOld)
				} else {
					s.cursorRow = oldLine.row
					s.cursorSide = workspaceDiffSideOld
				}
			})
		}
	}
	gutterPressOld := func(ui.EventContext) {
		if oldLine != nil {
			s.interact(func() { s.beginGutterRange(w, oldLine.row, workspaceDiffSideOld) })
		}
	}
	gutterMotionOld := func(_ ui.EventContext, mouse ui.Mouse) {
		if oldLine != nil && mouse.Button == ui.MouseLeftButton {
			s.interact(func() { s.extendGutterRange(w, oldLine.row, workspaceDiffSideOld) })
		}
	}
	moveNew := func(ui.EventContext) {
		if newLine != nil && (!s.cursorVisible() || s.cursorRow != newLine.row || s.cursorSide != workspaceDiffSideNew) {
			s.interact(func() {
				if s.selectionActive() {
					s.setRangeCursor(newLine.row, workspaceDiffSideNew)
				} else {
					s.cursorRow = newLine.row
					s.cursorSide = workspaceDiffSideNew
				}
			})
		}
	}
	gutterPressNew := func(ui.EventContext) {
		if newLine != nil {
			s.interact(func() { s.beginGutterRange(w, newLine.row, workspaceDiffSideNew) })
		}
	}
	gutterMotionNew := func(_ ui.EventContext, mouse ui.Mouse) {
		if newLine != nil && mouse.Button == ui.MouseLeftButton {
			s.interact(func() { s.extendGutterRange(w, newLine.row, workspaceDiffSideNew) })
		}
	}
	oldRangeSelected := oldLine != nil && oldLine.line.OldLine != nil && s.lineInSelection("old", *oldLine.line.OldLine)
	newRangeSelected := newLine != nil && newLine.line.NewLine != nil && s.lineInSelection("new", *newLine.line.NewLine)
	oldWidget := workspaceDiffSideWidget(oldLine, oldSelected, oldRangeSelected, false, height, s.pane.wrapLines, s.splitColumn, theme, semantic, moveOld, gutterPressOld, gutterMotionOld)
	newWidget := workspaceDiffSideWidget(newLine, newSelected, newRangeSelected, true, height, s.pane.wrapLines, s.splitColumn, theme, semantic, moveNew, gutterPressNew, gutterMotionNew)
	oldWidth, newWidth := workspaceDiffSplitWidths(s.pane.viewportWidth)
	row := ui.SizedBox{Width: oldWidth + newWidth + 1, Height: height, Child: ui.Flex{Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMax, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Width: oldWidth, Height: height, Child: oldWidget},
		ui.Text{Value: strings.TrimSuffix(strings.Repeat(glyphTableSeparator+"\n", height), "\n"), Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: height},
		ui.SizedBox{Width: newWidth, Height: height, Child: newWidget},
	}}}
	return row
}
