package tui

import (
	"context"
	"fmt"
	"github.com/akonwi/kit/internal/highlight"
	"github.com/akonwi/kit/internal/protocol"

	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis/ui"
)

const workspaceDiffConcurrentWork = 2

type workspaceDiffWork struct {
	ctx context.Context
	run func()
}

type workspaceDiffWorkPool struct {
	queue   []workspaceDiffWork
	running int
}

// Reads and syntax work have independent bounds: a slow highlighter must not
// prevent the visible document from acquiring evidence. Only the UI loop mutates
// queues and accounting; every worker reports completion even after cancellation.
func (s *workspaceDiffPaneState) queueWork(ctx context.Context, run func()) {
	s.readWork.queue = append(s.readWork.queue, workspaceDiffWork{ctx: ctx, run: run})
	s.startQueuedWork(&s.readWork, workspaceDiffConcurrentWork)
}

func (s *workspaceDiffPaneState) queueHighlight(ctx context.Context, run func()) {
	s.highlightWork.queue = append(s.highlightWork.queue, workspaceDiffWork{ctx: ctx, run: run})
	s.startQueuedWork(&s.highlightWork, 1)
}

func (s *workspaceDiffPaneState) startQueuedWork(pool *workspaceDiffWorkPool, limit int) {
	if s.disposed {
		return
	}
	w := s.Widget().(workspaceDiffPane)
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	for pool.running < limit && len(pool.queue) > 0 {
		job := pool.queue[0]
		pool.queue[0] = workspaceDiffWork{}
		pool.queue = pool.queue[1:]
		if job.ctx.Err() != nil {
			continue
		}
		pool.running++
		go func() {
			defer dispatch(func() { pool.running--; s.startQueuedWork(pool, limit) })
			if job.ctx.Err() == nil {
				job.run()
			}
		}()
	}
}

func (s *workspaceDiffPaneState) resetSections() {
	for _, section := range s.sections {
		section.dispose()
	}
	s.sections = nil
	s.layoutReady = false
	s.scrollCorrection = 0
	s.active = &workspaceDiffFileState{pane: s, cursorSide: workspaceDiffSideNew}
	s.ensureSections()
}

func (s *workspaceDiffPaneState) ensureSections() {
	for len(s.sections) < len(s.files) {
		file := s.files[len(s.sections)]
		s.sections = append(s.sections, &workspaceDiffFileState{pane: s, index: len(s.sections), file: file,
			key:    workspaceDiffFileKey{targetID: s.observation.Target.ID, targetRevision: s.observation.Revision, path: file.Path, fileRevision: file.FileRevision},
			height: 4, cursorSide: workspaceDiffSideNew})
	}
	if s.selectedFile >= 0 && s.selectedFile < len(s.sections) {
		s.active = s.sections[s.selectedFile]
	}
}

// activateSection changes keyboard ownership without replacing the document.
// An editor or mouse range never migrates to another file's coordinates.
func (s *workspaceDiffPaneState) activateSection(index int) bool {
	if index < 0 || index >= len(s.sections) {
		return false
	}
	if s.selectedFile == index {
		return true
	}
	if s.active.commenting || s.active.selectionActive() {
		return false
	}
	s.selectedFile = index
	s.active = s.sections[index]
	return true
}

func (s *workspaceDiffPaneState) invalidateDocumentLayout() {
	for _, section := range s.sections {
		section.splitColumn = 0
		section.invalidateSplitTargets()
	}
	s.cursorRevealPending = true
	s.revealPendingLayout = true
}

func (s *workspaceDiffPaneState) documentRows(theme ui.Theme, semantic SemanticTheme) ([]ui.Widget, int, int) {
	rows := make([]ui.Widget, 0, len(s.sections))
	width, height := max(1, s.viewportWidth), 0
	metrics := s.scroll.Metrics(ui.ScrollVertical)
	top, viewport := metrics.ScrollOffset, max(1, metrics.ViewportHeight)
	var anchor *workspaceDiffFileState
	anchorOffset := 0
	if s.layoutReady && !s.cursorRevealPending {
		for _, section := range s.sections {
			if section.offset <= top && section.offset+section.height > top {
				anchor, anchorOffset = section, section.offset
				break
			}
		}
	}
	for _, section := range s.sections {
		section.offset = height
		sectionWidth, sectionHeight := section.measure()
		section.height = sectionHeight
		width = max(width, sectionWidth)
		height += sectionHeight
		// Preserve measured extent without mounting offscreen code or spinner widgets.
		if (section.offset+section.height < top-viewport || section.offset > top+2*viewport) && !section.commenting && !section.selectionActive() {
			rows = append(rows, ui.SizedBox{Height: section.height})
			continue
		}
		file := s.files[section.index]
		title := file.Path
		if file.Additions != nil && file.Deletions != nil {
			title += fmt.Sprintf("  +%d −%d", *file.Additions, *file.Deletions)
		}
		header := headerControl{Label: title, OnPressed: func(event ui.EventContext) {
			w := s.Widget().(workspaceDiffPane)
			if w.OnFocusRequest != nil {
				w.OnFocusRequest(event)
			}
			s.SetState(func() { s.activateSection(section.index) })
		}}
		fileRows := []ui.Widget{
			ui.SizedBox{Height: 1, Child: ui.DecoratedBox(ui.Decoration{Style: ui.Style{Background: theme.Surface}}, ui.Padding(ui.Symmetric(1, 0), header))},
			ui.SizedBox{Height: 1, Child: ui.Divider{Style: ui.Style{Foreground: theme.Border}}},
		}
		switch {
		case section.loaded && len(section.hunks) > 0:
			var body []ui.Widget
			if s.splitLayout() {
				body, _, _ = section.splitDiffRows(theme, semantic)
			} else {
				body, _, _ = section.diffRows(theme, semantic)
			}
			fileRows = append(fileRows, body...)
		case section.errorText != "": // the local retry row below owns this state
		case !section.loaded:
			child := ui.Widget(ui.Text{Value: "Scroll to load diff", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
			if section.loadingFile {
				child = spinnerWithLabel("Loading file diff…", ui.Style{Foreground: theme.MutedForeground})
			}
			fileRows = append(fileRows, ui.SizedBox{Height: 1, Child: child})
		default:
			fileRows = append(fileRows, ui.SizedBox{Height: 1, Child: ui.Text{Value: workspaceDiffContentStateText(file), Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1}})
		}
		if section.errorText != "" {
			fileRows = append(fileRows, ui.SizedBox{Height: 1, Child: headerControl{Label: section.errorText + " · retry", OnPressed: func(ui.EventContext) {
				s.SetState(func() {
					if section.fileNextCursor != "" {
						section.requestFilePage(section.fileNextCursor, true)
					} else {
						section.startFileLoad()
					}
				})
			}}})
		} else if section.fileNextCursor != "" {
			label := "Load more diff lines"
			if section.loadingMore {
				label = "Loading more diff lines…"
			}
			fileRows = append(fileRows, ui.SizedBox{Height: 1, Child: headerControl{Label: label, OnPressed: func(ui.EventContext) {
				s.SetState(func() { section.loadMoreFileDiff() })
			}}})
		}
		fileRows = append(fileRows, ui.SizedBox{Height: 1})
		rows = append(rows, ui.SizedBox{Height: section.height, Child: ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: fileRows}})
	}
	if s.observationCursor != "" {
		label := "Loading more changed files…"
		if s.observationError != "" {
			label = s.observationError + " · retry changed files"
		}
		rows = append(rows, ui.SizedBox{Height: 1, Child: headerControl{Label: label, OnPressed: func(ui.EventContext) {
			if s.observationError != "" && !s.isFrozen(s.Widget().(workspaceDiffPane)) {
				s.SetState(func() { s.observationError = ""; s.requestObservationPage(s.observationCursor) })
			}
		}}})
		height++
	}
	if anchor != nil {
		s.scrollCorrection += anchor.offset - anchorOffset
	}
	s.layoutReady = true
	return rows, width, height
}

// measure uses semantic rows rather than mounted widgets. Offscreen sections keep
// their extent while their code, editors, and animation widgets are unmounted.
func (s *workspaceDiffFileState) measure() (int, int) {
	key := workspaceDiffMeasureKey{
		width: s.pane.viewportWidth, split: s.pane.splitLayout(), wrap: s.pane.wrapLines,
		generation: s.generation, annotations: s.pane.annotationLayoutVersion,
		hunks: len(s.hunks), lines: len(s.lineRows), loaded: s.loaded,
		more: s.fileNextCursor != "", failed: s.errorText != "",
		commenting: s.commenting, pending: s.commentPending, loading: s.commentLoading,
		commentError: s.commentError != "", commentID: s.commentAnnotationID,
		changes: s.pane.changesAvailable,
	}
	if s.measureValid && s.measureKey == key {
		return s.measuredWidth, s.measuredHeight
	}
	width, height := max(1, s.pane.viewportWidth), 3 // header, divider, gap
	if s.loaded && len(s.hunks) > 0 {
		height += len(s.hunks)
		if s.pane.splitLayout() {
			for _, target := range s.splitTargets() {
				height += target.height
			}
		}
		for _, hunk := range s.hunks {
			for _, line := range hunk.Lines {
				if !s.pane.splitLayout() {
					height += s.unifiedLineHeight(line)
					if !s.pane.wrapLines {
						width = max(width, workspaceDiffUnifiedLineWidth(line))
					}
				}
				if line.OldLine != nil {
					height += s.annotationHeightAfter("old", *line.OldLine)
				}
				if line.NewLine != nil {
					height += s.annotationHeightAfter("new", *line.NewLine)
				}
			}
		}
	} else if s.errorText == "" {
		height++
	}
	if s.errorText != "" || s.fileNextCursor != "" {
		height++
	}
	s.measureKey, s.measureValid = key, true
	s.measuredWidth, s.measuredHeight = width, height
	return width, height
}

func (s *workspaceDiffPaneState) documentBody(theme ui.Theme, semantic SemanticTheme) ui.Widget {
	rows, width, height := s.documentRows(theme, semantic)
	return ui.Scrollbar{
		Child: ui.ScrollPane{Controller: &s.scroll, Child: ui.SizedBox{Width: width, Height: height, Child: ui.Flex{
			Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows,
		}}},
		ThumbStyle: ui.Style{Foreground: semantic.Token(kittheme.TokenScrollbarForeground)},
		TrackStyle: ui.Style{Foreground: semantic.Token(kittheme.TokenScrollbarBackground)},
	}
}

// Demand is measured after layout, so mouse scrolling loads sections as well as
// keyboard navigation. One viewport of overscan avoids stalls at file boundaries.
func (s *workspaceDiffPaneState) loadVisibleSections() bool {
	w := s.Widget().(workspaceDiffPane)
	if !w.Presentation.Active || !w.Presentation.Visible || s.isFrozen(w) || s.pendingTarget.Reference != "" || s.phase != workspaceDiffReady || !s.scroll.Attached() {
		return false
	}
	metrics := s.scroll.Metrics(ui.ScrollVertical)
	top, bottom := metrics.ScrollOffset, metrics.ScrollOffset+metrics.ViewportHeight
	changed := false
	for _, section := range s.sections {
		if section.offset+section.height < top-metrics.ViewportHeight || section.offset > bottom+metrics.ViewportHeight {
			continue
		}
		if section.loaded && len(section.hunks) > 0 && !section.highlightReady && section.highlightCancel == nil {
			section.startHighlight()
		}
		if !section.loaded && !section.loadingFile && section.errorText == "" {
			section.startFileLoad()
			changed = true
		} else if section.fileNextCursor != "" && !section.loadingMore && section.errorText == "" && section.offset+section.height >= top && section.offset+section.height <= bottom+3 {
			section.loadMoreFileDiff()
			changed = true
		}
	}
	return changed
}

func (s *workspaceDiffFileState) stopRead() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.generation++
	s.loadingFile, s.loadingMore = false, false
	s.stopHighlight()
}

func (s *workspaceDiffFileState) dispose() {
	s.disposed = true
	s.stopRead()
	s.commentOperation++
	if s.commentLoadCancel != nil {
		s.commentLoadCancel()
		s.commentLoadCancel = nil
	}
}

func (s *workspaceDiffFileState) dispatchPendingResults(dispatch func(func())) {
	dispatch(func() {
		if s.disposed || s.pane.disposed {
			return
		}
		s.pane.SetState(func() {
			s.resultMu.Lock()
			file, highlighted := s.pendingFile, s.pendingHighlight
			s.pendingFile, s.pendingHighlight = nil, nil
			s.resultMu.Unlock()
			if file != nil && file.generation == s.generation {
				s.completeFileLoad(file.page, file.append, file.err)
			}
			if highlighted != nil && highlighted.generation == s.highlightGeneration {
				s.highlightCancel = nil
				s.oldHighlighted, s.newHighlighted = highlighted.old, highlighted.new
				s.highlightReady = true
			}
		})
	})
}

func (s *workspaceDiffFileState) interact(update func()) {
	s.pane.SetState(func() {
		if s.pane.activateSection(s.index) {
			update()
		}
	})
}

func (s *workspaceDiffFileState) cursorVisible() bool { return s.pane.active == s }

func (s *workspaceDiffPaneState) moveLine(delta int) {
	section := s.active
	if delta == 0 {
		return
	}
	if !section.selectionActive() && section.loaded && section.fileNextCursor == "" {
		rows := section.lineRows
		atStart, atEnd := len(rows) == 0, len(rows) == 0
		if len(rows) > 0 {
			atStart = section.cursorRow <= rows[0]
			atEnd = section.cursorRow >= rows[len(rows)-1]
		}
		if s.splitLayout() {
			lines := section.splitNavigationLines()
			if len(lines) > 0 {
				atStart = section.cursorRow == lines[0].row
				atEnd = section.cursorRow == lines[len(lines)-1].row
			}
		}
		if delta < 0 && atStart || delta > 0 && atEnd {
			if s.moveAcrossFile(delta, false) {
				return
			}
		}
	}
	section.moveLine(delta)
}

func (s *workspaceDiffPaneState) moveHunk(delta int) {
	section := s.active
	if delta == 0 {
		return
	}
	if !section.selectionActive() && section.loaded && section.fileNextCursor == "" {
		rows := section.hunkRows
		if len(rows) == 0 || delta < 0 && section.cursorRow <= rows[0] || delta > 0 && section.cursorRow >= rows[len(rows)-1] {
			if s.moveAcrossFile(delta, true) {
				return
			}
		}
	}
	section.moveHunk(delta)
}

func (s *workspaceDiffPaneState) moveAcrossFile(delta int, hunk bool) bool {
	next := s.selectedFile + 1
	if delta < 0 {
		next = s.selectedFile - 1
	}
	if !s.activateSection(next) {
		return false
	}
	section := s.active
	section.pendingEdge = delta
	section.pendingHunk = hunk
	if !section.loaded && !section.loadingFile {
		section.startFileLoad()
	}
	section.applyPendingEdge()
	s.cursorRevealPending, s.revealPendingLayout = true, true
	return true
}

func (s *workspaceDiffFileState) applyPendingEdge() {
	if s.pendingEdge == 0 || !s.loaded {
		return
	}
	rows := s.lineRows
	if s.pendingHunk {
		rows = s.hunkRows
	}
	if len(rows) > 0 {
		row := rows[0]
		if s.pendingEdge < 0 {
			row = rows[len(rows)-1]
		}
		s.setCursorRow(row)
	}
	s.pendingEdge = 0
	if s.cursorVisible() {
		s.pane.cursorRevealPending, s.pane.revealPendingLayout = true, true
	}
}

func (s *workspaceDiffPaneState) splitLayout() bool {
	return !s.unifiedLayout && s.viewportWidth >= workspaceDiffSplitBreakpoint
}

func (s *workspaceDiffFileState) unifiedLineHeight(line protocol.DiffLine) int {
	if !s.pane.wrapLines {
		return 1
	}
	return max(1, ui.LayoutText([]ui.TextSpan{{Text: highlight.Sanitize(line.Content)}},
		ui.Constraints{MaxWidth: max(1, s.pane.viewportWidth-14), MaxHeight: ui.Unbounded},
		ui.TextLayoutOptions{SoftWrap: true}).Size.Height)
}
