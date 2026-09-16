package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/highlight"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis/ui"
)

type workspaceFileLoadState uint8

const (
	workspaceFileInitial workspaceFileLoadState = iota
	workspaceFileLoading
	workspaceFileReady
	workspaceFileEmpty
	workspaceFileTruncated
	workspaceFileStaleFile
	workspaceFileFrozen
	workspaceFileBinary
	workspaceFileMissing
	workspaceFilePermission
	workspaceFileUnavailable
	workspaceFileCapacity
	workspaceFileCanceled
	workspaceFileFailed
)

type workspaceFilePane struct {
	Descriptor         workspacePaneDescriptor
	CurrentWorkspaceID string
	Files              sessionclient.WorkspaceFilesSession
	Presentation       workspacePanePresentation
	Highlighter        highlight.Highlighter
	Dispatch           func(func())
	OnFocusRequest     ui.VoidCallback
}

func (workspaceFilePane) CreateState() ui.State { return &workspaceFilePaneState{} }

type workspaceFileResult struct {
	generation uint64
	read       protocol.WorkspaceFileRead
	err        error
}

type workspaceHighlightResult struct {
	generation uint64
	result     highlight.Result
}

type workspaceFilePaneState struct {
	ui.StateBase
	loadState           workspaceFileLoadState
	read                protocol.WorkspaceFileRead
	lines               []string
	hasContent          bool
	generation          uint64
	cancel              context.CancelFunc
	disposed            bool
	resultMu            sync.Mutex
	pendingResult       *workspaceFileResult
	highlightGeneration uint64
	highlightCancel     context.CancelFunc
	pendingHighlight    *workspaceHighlightResult
	highlighted         highlight.Result
	highlightReady      bool
	scroll              ui.ScrollPaneController
	focus               ui.FocusNode
	cursorLine          int
	pendingReveal       int
	appliedOpen         uint64
	cachedBody          ui.Widget
}

func (s *workspaceFilePaneState) InitState() {
	w := s.Widget().(workspaceFilePane)
	s.cursorLine = max(1, w.Descriptor.RevealStartLine)
	s.pendingReveal = w.Descriptor.RevealStartLine
	s.appliedOpen = w.Descriptor.OpenGeneration
	if s.isFrozen(w) {
		s.loadState = workspaceFileFrozen
		return
	}
	if w.Presentation.Active {
		s.startLoad(false)
	}
}

func (s *workspaceFilePaneState) DidUpdateWidget(old ui.Widget) {
	previous := old.(workspaceFilePane)
	w := s.Widget().(workspaceFilePane)
	if s.isFrozen(w) {
		if s.loadState != workspaceFileFrozen {
			s.stopLoad()
			s.loadState = workspaceFileFrozen
		}
		return
	}
	if previous.Presentation.Active && !w.Presentation.Active {
		s.stopLoad()
		return
	}
	if w.Descriptor.OpenGeneration != s.appliedOpen {
		s.appliedOpen = w.Descriptor.OpenGeneration
		line := w.Descriptor.RevealStartLine
		if line > 0 {
			if s.hasContent {
				line = min(line, max(1, len(s.lines)))
			}
			s.cursorLine = line
			s.pendingReveal = line
		}
		s.focus.RequestFocus()
	}
	if !previous.Presentation.Active && w.Presentation.Active {
		if s.loadState == workspaceFileInitial || s.loadState == workspaceFileLoading || s.loadState == workspaceFileCanceled {
			s.startLoad(false)
		} else if s.hasContent && !s.highlightReady {
			s.startHighlight()
		}
	}
}

func (s *workspaceFilePaneState) Dispose() {
	s.disposed = true
	s.stopLoad()
}

func (s *workspaceFilePaneState) isFrozen(w workspaceFilePane) bool {
	return w.CurrentWorkspaceID != "" && w.CurrentWorkspaceID != w.Descriptor.WorkspaceID
}

func (s *workspaceFilePaneState) stopLoad() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.generation++
	}
	s.stopHighlight()
}

func (s *workspaceFilePaneState) stopHighlight() {
	if s.highlightCancel != nil {
		s.highlightCancel()
		s.highlightCancel = nil
		s.highlightGeneration++
	}
}

func (s *workspaceFilePaneState) startHighlight() {
	w := s.Widget().(workspaceFilePane)
	if !w.Presentation.Active || !s.hasContent {
		return
	}
	s.stopHighlight()
	s.highlightGeneration++
	generation := s.highlightGeneration
	ctx, cancel := context.WithCancel(context.Background())
	s.highlightCancel = cancel
	request := highlight.Request{Path: w.Descriptor.Path, Source: s.read.Content, Revision: s.read.Revision}
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	highlighter := w.Highlighter
	if highlighter == nil {
		highlighter = highlight.Default
	}
	go func() {
		result := highlight.Validate(highlighter.Highlight(ctx, request))
		s.resultMu.Lock()
		if s.pendingHighlight == nil || generation >= s.pendingHighlight.generation {
			s.pendingHighlight = &workspaceHighlightResult{generation: generation, result: result}
		}
		s.resultMu.Unlock()
		dispatch(func() {
			if !s.disposed {
				s.SetState(func() {})
			}
		})
	}()
}

func (s *workspaceFilePaneState) startLoad(acceptCurrent bool) {
	w := s.Widget().(workspaceFilePane)
	if !w.Presentation.Active || s.isFrozen(w) {
		return
	}
	s.stopLoad()
	s.generation++
	generation := s.generation
	s.loadState = workspaceFileLoading
	if w.Files == nil {
		s.loadState = workspaceFileUnavailable
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	expected := w.Descriptor.ExpectedRevision
	if s.hasContent {
		expected = s.read.Revision
	}
	if acceptCurrent {
		expected = ""
	}
	input := protocol.ReadWorkspaceFileInput{WorkspaceID: w.Descriptor.WorkspaceID, Path: w.Descriptor.Path, ExpectedFileRevision: expected}
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	go func() {
		read, err := w.Files.ReadWorkspaceFile(ctx, input)
		s.queueResult(workspaceFileResult{generation: generation, read: read, err: err})
		dispatch(func() {
			if !s.disposed {
				s.SetState(func() {})
			}
		})
	}()
}

func (s *workspaceFilePaneState) queueResult(result workspaceFileResult) {
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	if s.pendingResult == nil || result.generation >= s.pendingResult.generation {
		s.pendingResult = &result
	}
}

func (s *workspaceFilePaneState) applyPendingResults() {
	s.resultMu.Lock()
	result := s.pendingResult
	s.pendingResult = nil
	highlightResult := s.pendingHighlight
	s.pendingHighlight = nil
	s.resultMu.Unlock()
	if result != nil && !s.disposed && result.generation == s.generation {
		s.cancel = nil
		s.completeLoad(result.read, result.err)
	}
	if highlightResult != nil && !s.disposed && highlightResult.generation == s.highlightGeneration {
		s.highlightCancel = nil
		s.highlighted = highlightResult.result
		s.highlightReady = true
		s.cachedBody = nil
	}
}

func (s *workspaceFilePaneState) completeLoad(read protocol.WorkspaceFileRead, err error) {
	s.cachedBody = nil
	s.highlightReady = false
	s.highlighted = highlight.Result{}
	if err != nil {
		s.loadState = classifyWorkspaceFileError(err)
		return
	}
	w := s.Widget().(workspaceFilePane)
	if read.Workspace.WorkspaceID != w.Descriptor.WorkspaceID || read.Path != w.Descriptor.Path {
		s.loadState = workspaceFileFailed
		return
	}
	s.read = read
	s.lines = workspaceFileLines(read.Content)
	s.hasContent = true
	s.loadState = workspaceFileReady
	if len(s.lines) == 0 {
		s.loadState = workspaceFileEmpty
	} else if read.Truncated {
		s.loadState = workspaceFileTruncated
	}
	if s.cursorLine > max(1, len(s.lines)) {
		s.cursorLine = max(1, len(s.lines))
	}
	if s.pendingReveal == 0 {
		s.pendingReveal = s.cursorLine
	}
	s.startHighlight()
}

func classifyWorkspaceFileError(err error) workspaceFileLoadState {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return workspaceFileCanceled
	}
	var workspaceErr *protocol.WorkspaceError
	if !errors.As(err, &workspaceErr) {
		return workspaceFileFailed
	}
	switch workspaceErr.Code {
	case protocol.WorkspaceErrorStaleWorkspace:
		return workspaceFileFrozen
	case protocol.WorkspaceErrorStaleFile:
		return workspaceFileStaleFile
	case protocol.WorkspaceErrorBinary:
		return workspaceFileBinary
	case protocol.WorkspaceErrorNotFound, protocol.WorkspaceErrorNotFile:
		return workspaceFileMissing
	case protocol.WorkspaceErrorPermissionDenied, protocol.WorkspaceErrorOutside, protocol.WorkspaceErrorSymlink:
		return workspaceFilePermission
	case protocol.WorkspaceErrorUnavailable:
		return workspaceFileUnavailable
	case protocol.WorkspaceErrorCapacity, protocol.WorkspaceErrorLimit:
		return workspaceFileCapacity
	default:
		return workspaceFileFailed
	}
}

func workspaceFileLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type moveWorkspaceFileCursorIntent struct{ lines, columns int }

func (moveWorkspaceFileCursorIntent) IntentType() ui.IntentType { return "kit.workspace-file.move" }

type refreshWorkspaceFileIntent struct{}

func (refreshWorkspaceFileIntent) IntentType() ui.IntentType { return "kit.workspace-file.refresh" }

func (s *workspaceFilePaneState) Build(ctx ui.BuildContext) ui.Widget {
	s.applyPendingResults()
	w := s.Widget().(workspaceFilePane)
	theme := ui.MustDepend[ui.Theme](ctx)
	semantic, ok := ui.Depend[SemanticTheme](ctx)
	if !ok {
		semantic = semanticFallback(theme)
	}

	headerRight := s.headerStatus()
	header := workspacePanelHeader(theme, w.Descriptor.Path, headerRight)
	body := s.body(theme, semantic, w)
	footer := workspacePanelFooter(theme, s.footerText(w))

	bindings := map[ui.IntentType]ui.ActionFunc{}
	shortcuts := ui.ShortcutMap{}
	if w.Presentation.Active {
		bindings[moveWorkspaceFileCursorIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			movement := intent.(moveWorkspaceFileCursorIntent)
			s.SetState(func() {
				if movement.lines != 0 {
					s.cursorLine = max(1, min(max(1, len(s.lines)), s.cursorLine+movement.lines))
					s.pendingReveal = s.cursorLine
				}
				s.scroll.ScrollBy(movement.columns, movement.lines)
			})
			return ui.EventHandled
		}
		shortcuts["Up"] = moveWorkspaceFileCursorIntent{lines: -1}
		shortcuts["k"] = moveWorkspaceFileCursorIntent{lines: -1}
		shortcuts["Down"] = moveWorkspaceFileCursorIntent{lines: 1}
		shortcuts["j"] = moveWorkspaceFileCursorIntent{lines: 1}
		shortcuts["Left"] = moveWorkspaceFileCursorIntent{columns: -4}
		shortcuts["h"] = moveWorkspaceFileCursorIntent{columns: -4}
		shortcuts["Right"] = moveWorkspaceFileCursorIntent{columns: 4}
		shortcuts["l"] = moveWorkspaceFileCursorIntent{columns: 4}
		if s.loadState != workspaceFileFrozen {
			bindings[refreshWorkspaceFileIntent{}.IntentType()] = func(_ ui.EventContext, _ ui.Intent) ui.EventResult {
				s.SetState(func() { s.startLoad(s.loadState == workspaceFileStaleFile) })
				return ui.EventHandled
			}
			shortcuts["r"] = refreshWorkspaceFileIntent{}
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

func (s *workspaceFilePaneState) body(theme ui.Theme, semantic SemanticTheme, w workspaceFilePane) ui.Widget {
	if !w.Presentation.Active && s.cachedBody != nil {
		return s.cachedBody
	}
	if s.hasContent && (s.loadState == workspaceFileLoading || s.loadState == workspaceFileStaleFile || s.loadState == workspaceFileFrozen || s.loadState == workspaceFileTruncated || s.loadState == workspaceFileReady) {
		result := s.highlighted
		if !s.highlightReady {
			result = highlight.Plain(highlight.Request{Path: w.Descriptor.Path, Source: s.read.Content, Revision: s.read.Revision}, nil)
		}
		spans := workspaceFileSpans(result, theme, semantic)
		pane := ui.Widget(ui.ScrollPane{Controller: &s.scroll, Child: ui.RichText{Spans: spans, SoftWrap: false}})
		pane = ui.Scrollbar{
			Child:      pane,
			ThumbStyle: ui.Style{Foreground: semantic.Token(kittheme.TokenScrollbarForeground)},
			TrackStyle: ui.Style{Foreground: semantic.Token(kittheme.TokenScrollbarBackground)},
		}
		s.cachedBody = pane
		return pane
	}
	message, detail := s.emptyStateText()
	children := []ui.Widget{ui.Text{Value: message, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Align: ui.TextAlignCenter}}
	if detail != "" {
		children = append(children, ui.Text{Value: detail, Style: ui.Style{Foreground: theme.DisabledForeground}, MaxLines: 1, Align: ui.TextAlignCenter})
	}
	if s.loadState == workspaceFileLoading && w.Presentation.Active {
		children[0] = centeredSpinnerWithLabel(message, ui.Style{Foreground: theme.MutedForeground})
	}
	return ui.Flex{Axis: ui.Vertical, MainAxisAlignment: ui.MainAxisCenter, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}
}

func (s *workspaceFilePaneState) emptyStateText() (string, string) {
	switch s.loadState {
	case workspaceFileLoading:
		return "Loading file…", ""
	case workspaceFileEmpty:
		return "Empty file", "0 bytes"
	case workspaceFileFrozen:
		return "Stale workspace", "This pane is frozen at its original workspace."
	case workspaceFileStaleFile:
		return "File changed", "Press r to load the current revision."
	case workspaceFileBinary:
		return "Binary file", "A text preview is not available."
	case workspaceFileMissing:
		return "File not found", "The file may have moved or been removed."
	case workspaceFilePermission:
		return "File is unreadable", "Permission was denied by the workspace host."
	case workspaceFileUnavailable:
		return "Workspace files unavailable", "Try again when the session workspace is available."
	case workspaceFileCapacity:
		return "Workspace is busy", "Press r to retry."
	case workspaceFileCanceled:
		return "Load canceled", "Press r to retry."
	case workspaceFileFailed:
		return "Could not load file", "The workspace host returned an unexpected error."
	default:
		return "Waiting to load file", ""
	}
}

func (s *workspaceFilePaneState) headerStatus() string {
	if s.loadState == workspaceFileFrozen {
		return "stale · frozen"
	}
	if s.hasContent {
		return fmt.Sprintf("Ln %d · %d lines · %s", s.cursorLine, len(s.lines), formatWorkspaceFileSize(s.read.Size))
	}
	return ""
}

func (s *workspaceFilePaneState) footerText(w workspaceFilePane) string {
	if s.loadState == workspaceFileFrozen {
		return "Frozen evidence · navigation only"
	}
	parts := []string{"↑↓ lines", "←→ columns"}
	if s.loadState != workspaceFileLoading {
		parts = append(parts, "r refresh")
	}
	if s.loadState == workspaceFileTruncated {
		parts = append([]string{"Preview truncated (" + strings.ReplaceAll(s.read.TruncationReason, "_", " ") + ")"}, parts...)
	} else if s.loadState == workspaceFileStaleFile {
		parts = append([]string{"File changed"}, parts...)
	} else if s.loadState == workspaceFileLoading && s.hasContent {
		parts = append([]string{"Refreshing…"}, parts...)
	}
	return strings.Join(parts, " · ")
}

func (s *workspaceFilePaneState) TickFrame(_ time.Time) bool {
	if s.pendingReveal <= 0 || !s.scroll.Attached() {
		return s.pendingReveal > 0
	}
	row := max(0, s.pendingReveal-1)
	vertical := s.scroll.Metrics(ui.ScrollVertical)
	if row < vertical.ScrollOffset {
		s.scroll.ScrollTo(s.scroll.Metrics(ui.ScrollHorizontal).ScrollOffset, row)
	} else if row >= vertical.ScrollOffset+vertical.ViewportHeight {
		s.scroll.ScrollTo(s.scroll.Metrics(ui.ScrollHorizontal).ScrollOffset, max(0, row-vertical.ViewportHeight+1))
	}
	s.pendingReveal = 0
	return false
}

func formatWorkspaceFileSize(size int64) string {
	if size < 1024 {
		return strconv.FormatInt(size, 10) + " B"
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(size)/(1024*1024))
}

func workspaceFileSpans(result highlight.Result, theme ui.Theme, semantic SemanticTheme) []ui.TextSpan {
	lines := strings.Split(result.Source, "\n")
	if strings.HasSuffix(result.Source, "\n") {
		lines = lines[:len(lines)-1]
	}
	width := len(strconv.Itoa(max(1, len(lines))))
	spans := make([]ui.TextSpan, 0, len(lines)*6)
	offset := 0
	captureIndex := 0
	for index, line := range lines {
		spans = append(spans, ui.TextSpan{Text: fmt.Sprintf("%*d │ ", width, index+1), Style: ui.Style{Foreground: theme.MutedForeground}})
		lineEnd := offset + len(line)
		position := offset
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
		if index+1 < len(lines) {
			spans = append(spans, ui.TextSpan{Text: "\n"})
		}
		offset = lineEnd + 1
	}
	return spans
}

func syntaxTextSpan(text string, role highlight.Role, semantic SemanticTheme) ui.TextSpan {
	return ui.TextSpan{Text: text, Style: ui.Style{Foreground: semantic.Syntax(string(role))}}
}

type workspacePanelLayout struct{ Header, Body, Footer ui.Widget }

func (w workspacePanelLayout) Build(ui.BuildContext) ui.Widget {
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		w.Header,
		ui.Expanded(w.Body),
		w.Footer,
	}}
}

func workspacePanelHeader(theme ui.Theme, left, right string) ui.Widget {
	children := []ui.Widget{ui.Expanded(ui.Text{Value: left, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis})}
	if right != "" {
		children = append(children, ui.Text{Value: right, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Horizontal, Children: children}),
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
	}}
}

func workspacePanelFooter(theme ui.Theme, text string) ui.Widget {
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		ui.Padding(ui.Symmetric(1, 0), ui.Text{Value: text, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}),
	}}
}
