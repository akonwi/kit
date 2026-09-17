package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis/ui"
)

type workspaceDiffPhase uint8

const (
	workspaceDiffInitial workspaceDiffPhase = iota
	workspaceDiffLoading
	workspaceDiffReady
	workspaceDiffEmpty
	workspaceDiffError
)

type moveWorkspaceDiffIntent struct{ lines int }

func (moveWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.move" }

type moveWorkspaceDiffFileIntent struct{ delta int }

func (moveWorkspaceDiffFileIntent) IntentType() ui.IntentType { return "kit.workspace-diff.file" }

type moveWorkspaceDiffHunkIntent struct{ delta int }

func (moveWorkspaceDiffHunkIntent) IntentType() ui.IntentType { return "kit.workspace-diff.hunk" }

type refreshWorkspaceDiffIntent struct{}

func (refreshWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.refresh" }

type loadMoreWorkspaceDiffIntent struct{}

func (loadMoreWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.load-more" }

type workspaceDiffPane struct {
	Descriptor         workspacePaneDescriptor
	CurrentWorkspaceID string
	Diff               sessionclient.WorkingTreeDiffSession
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

type workspaceDiffPaneState struct {
	ui.StateBase
	phase              workspaceDiffPhase
	generation         uint64
	cancel             context.CancelFunc
	observation        protocol.DiffObservation
	files              []protocol.DiffFileSummary
	selectedFile       int
	hunks              []protocol.DiffHunk
	cursorRow          int
	hunkRows           []int
	errorText          string
	loadingFile        bool
	loadingMore        bool
	fileNextCursor     string
	observationCursor  string
	scroll             ui.ScrollController
	focus              ui.FocusNode
	appliedOpen        uint64
	resultMu           sync.Mutex
	pendingObservation *workspaceDiffObservationResult
	pendingFile        *workspaceDiffFileResult
	disposed           bool
}

func (s *workspaceDiffPaneState) InitState() {
	w := s.Widget().(workspaceDiffPane)
	s.appliedOpen = w.Descriptor.OpenGeneration
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
	if !previous.Presentation.Active && w.Presentation.Active && (s.phase == workspaceDiffInitial || s.phase == workspaceDiffLoading) {
		s.startObservation()
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
}

func (s *workspaceDiffPaneState) startObservation() {
	w := s.Widget().(workspaceDiffPane)
	if !w.Presentation.Active || s.isFrozen(w) {
		return
	}
	s.stopWork()
	s.generation++
	generation := s.generation
	s.phase = workspaceDiffLoading
	s.errorText = ""
	s.files = nil
	s.hunks = nil
	s.observation = protocol.DiffObservation{}
	s.cursorRow = 0
	s.hunkRows = nil
	if w.Diff == nil {
		s.phase = workspaceDiffError
		s.errorText = "Working-tree diffs are unavailable"
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	workspaceID := w.Descriptor.WorkspaceID
	go func() {
		page, err := w.Diff.ObserveWorkingTree(ctx, protocol.ObserveWorkingTreeInput{WorkspaceID: workspaceID})
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
	s.observation = page.Observation
	s.files = append([]protocol.DiffFileSummary(nil), page.Files...)
	s.observationCursor = page.NextCursor
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
	s.cursorRow = 0
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
	s.fileNextCursor = page.NextCursor
	s.rebuildHunkRows()
	if s.cursorRow == 0 && len(s.hunkRows) > 0 {
		s.cursorRow = s.hunkRows[0]
	}
}

func (s *workspaceDiffPaneState) appendHunks(next []protocol.DiffHunk) {
	if len(s.hunks) > 0 && len(next) > 0 && next[0].ContinuedBefore && s.hunks[len(s.hunks)-1].ContinuedAfter {
		last := &s.hunks[len(s.hunks)-1]
		last.OldCount += next[0].OldCount
		last.NewCount += next[0].NewCount
		last.Lines = append(last.Lines, next[0].Lines...)
		last.ContinuedAfter = next[0].ContinuedAfter
		next = next[1:]
	}
	s.hunks = append(s.hunks, next...)
}

func (s *workspaceDiffPaneState) rebuildHunkRows() {
	s.hunkRows = s.hunkRows[:0]
	row := 0
	for _, hunk := range s.hunks {
		s.hunkRows = append(s.hunkRows, row+1)
		row += len(hunk.Lines) + 1
	}
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
	s.scroll = ui.ScrollController{}
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
	s.cursorRow = s.hunkRows[selected]
	s.scroll.ScrollToOffset(s.cursorRow)
}

func (s *workspaceDiffPaneState) applyPendingResults() {
	s.resultMu.Lock()
	observation := s.pendingObservation
	file := s.pendingFile
	s.pendingObservation = nil
	s.pendingFile = nil
	s.resultMu.Unlock()
	if observation != nil && observation.generation == s.generation {
		s.completeObservation(observation.page, observation.err)
	}
	if file != nil && file.generation == s.generation {
		s.completeFileLoad(file.page, file.append, file.err)
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
				s.cursorRow = max(0, s.cursorRow+movement.lines)
				metrics := s.scroll.Metrics()
				s.scroll.ScrollToOffset(max(0, metrics.ScrollOffset+movement.lines))
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
		shortcuts["{"] = moveWorkspaceDiffHunkIntent{delta: -1}
		shortcuts["}"] = moveWorkspaceDiffHunkIntent{delta: 1}
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
					s.SetState(func() { s.requestFilePage(s.fileNextCursor, true) })
					return ui.EventHandled
				}
				shortcuts["m"] = loadMoreWorkspaceDiffIntent{}
			}
		}
	}
	content := ui.Widget(workspacePanelLayout{Header: header, Body: body, Footer: footer})
	content = ui.Focus(&s.focus, content)
	content = ui.FocusScope{AutoFocus: w.Presentation.Active, Child: content}
	content = mouseActivator{Child: content, DefaultMouseShape: true, OnPrimaryDownCapture: func(event ui.EventContext) {
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
		rows := s.diffRows(theme, semantic)
		child = ui.Scrollbar{Child: ui.ScrollView{Controller: &s.scroll, Child: ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows}}}
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

func (s *workspaceDiffPaneState) diffRows(theme ui.Theme, semantic SemanticTheme) []ui.Widget {
	rows := make([]ui.Widget, 0)
	visualRow := 0
	for _, hunk := range s.hunks {
		header := fmt.Sprintf("%s -%d,%d +%d,%d", glyphDiamond, hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
		rows = append(rows, ui.SizedBox{Height: 1, Child: ui.Text{Value: header, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}})
		visualRow++
		for _, line := range hunk.Lines {
			row := visualRow
			moveCursor := func(ui.EventContext) {
				if s.cursorRow != row {
					s.SetState(func() { s.cursorRow = row })
				}
			}
			rows = append(rows, workspaceDiffLineWidget(line, row == s.cursorRow, theme, semantic, moveCursor, moveCursor))
			visualRow++
		}
	}
	return rows
}

func workspaceDiffLineWidget(line protocol.DiffLine, selected bool, theme ui.Theme, semantic SemanticTheme, onHover, onPressed ui.VoidCallback) ui.Widget {
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
	row := ui.Widget(ui.SizedBox{Height: 1, Child: ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
		ui.RichText{Spans: []ui.TextSpan{{Text: lineNumbers, Style: gutterStyle}, {Text: indicator, Style: indicatorStyle}, {Text: diffMarker, Style: gutterStyle}}, SoftWrap: false},
		ui.Expanded(ui.Text{Value: line.Content, Style: contentStyle, MaxLines: 1, Overflow: ui.TextOverflowClip}),
	}}})
	return mouseActivator{Child: row, OnHover: onHover, OnPressed: onPressed}
}

func (s *workspaceDiffPaneState) footerText() string {
	w := s.Widget().(workspaceDiffPane)
	if s.isFrozen(w) {
		return "Frozen evidence " + glyphMiddleDot + " navigation only"
	}
	parts := []string{"↑↓ lines", "[ ] files", "{ } hunks", "r refresh"}
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
