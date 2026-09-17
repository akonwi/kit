package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

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
	Annotations        []protocol.AnnotationSummary
	MouseGestures      *workspaceMouseGestureController
	OnFocusRequest     ui.VoidCallback
	OnCreateAnnotation func(protocol.AnnotationAnchor, string, func(error))
	OnLoadAnnotation   func(uint64, func(string, error)) func()
	OnUpdateAnnotation func(uint64, string, func(error))
	OnRemoveAnnotation func(ui.EventContext, uint64)
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
	loadState                  workspaceFileLoadState
	read                       protocol.WorkspaceFileRead
	lines                      []string
	hasContent                 bool
	generation                 uint64
	cancel                     context.CancelFunc
	disposed                   bool
	resultMu                   sync.Mutex
	pendingResult              *workspaceFileResult
	highlightGeneration        uint64
	highlightCancel            context.CancelFunc
	pendingHighlight           *workspaceHighlightResult
	highlighted                highlight.Result
	highlightReady             bool
	scroll                     ui.ScrollPaneController
	focus                      ui.FocusNode
	cursorLine                 int
	selectionAnchor            int
	mouseSelectionAnchor       int
	mouseSelectionGeneration   uint64
	commenting                 bool
	commentBody                string
	commentPending             bool
	commentLoading             bool
	commentLoadCancel          func()
	commentError               string
	commentLoadFailed          bool
	commentAnchor              protocol.WorkspaceFileAnnotationAnchor
	commentAnnotationID        uint64
	commentCursorEndGeneration uint64
	commentOperation           uint64
	commentReveal              bool
	commentScrollQueued        bool
	cursorScrollQueued         bool
	commentFocusGeneration     uint64
	viewportWidth              int
	pendingReveal              int
	appliedOpen                uint64
	cachedBody                 ui.Widget
}

func (s *workspaceFilePaneState) InitState() {
	w := s.Widget().(workspaceFilePane)
	s.cursorLine = max(1, w.Descriptor.RevealStartLine)
	if w.Descriptor.RevealEndLine > w.Descriptor.RevealStartLine && w.Descriptor.RevealStartLine > 0 {
		s.selectionAnchor = w.Descriptor.RevealEndLine
	}
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
	if !workspaceAnnotationSummariesEqual(previous.Annotations, w.Annotations) {
		s.cachedBody = nil
	}
	if s.isFrozen(w) {
		if s.loadState != workspaceFileFrozen {
			s.stopLoad()
			s.loadState = workspaceFileFrozen
		}
		return
	}
	if previous.Presentation.Active && !w.Presentation.Active {
		s.mouseSelectionAnchor = 0
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
			if w.Descriptor.RevealEndLine > line {
				s.selectionAnchor = w.Descriptor.RevealEndLine
			} else {
				s.selectionAnchor = 0
			}
			s.pendingReveal = line
		}
		s.focus.RequestFocus()
	}
	if !previous.Presentation.Active && w.Presentation.Active {
		if s.commenting {
			s.commentFocusGeneration++
			s.commentReveal = true
			s.cachedBody = nil
		}
		if s.loadState == workspaceFileInitial || s.loadState == workspaceFileLoading || s.loadState == workspaceFileCanceled {
			s.startLoad(false)
		} else if s.hasContent && !s.highlightReady {
			s.startHighlight()
		}
	}
}

func (s *workspaceFilePaneState) Dispose() {
	s.disposed = true
	s.mouseSelectionAnchor = 0
	if s.commentLoadCancel != nil {
		s.commentLoadCancel()
	}
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
	if s.selectionAnchor > len(s.lines) {
		s.selectionAnchor = 0
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

type toggleWorkspaceFileSelectionIntent struct{}

func (toggleWorkspaceFileSelectionIntent) IntentType() ui.IntentType {
	return "kit.workspace-file.toggle-selection"
}

type annotateWorkspaceFileIntent struct{}

func (annotateWorkspaceFileIntent) IntentType() ui.IntentType { return "kit.workspace-file.annotate" }

type editWorkspaceFileAnnotationIntent struct{}

func (editWorkspaceFileAnnotationIntent) IntentType() ui.IntentType {
	return "kit.workspace-file.edit-annotation"
}

type removeWorkspaceFileAnnotationIntent struct{}

func (removeWorkspaceFileAnnotationIntent) IntentType() ui.IntentType {
	return "kit.workspace-file.remove-annotation"
}

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
	if w.Presentation.Active && s.commenting {
		bindings[ui.IntentType("vaxis.dismiss")] = func(ui.EventContext, ui.Intent) ui.EventResult {
			if !s.commentPending {
				s.SetState(func() { s.closeCommentEditor() })
			}
			return ui.EventHandled
		}
		bindings[ui.IntentType("vaxis.next-focus")] = func(ui.EventContext, ui.Intent) ui.EventResult { return ui.EventHandled }
		bindings[ui.IntentType("vaxis.previous-focus")] = func(ui.EventContext, ui.Intent) ui.EventResult { return ui.EventHandled }
	} else if w.Presentation.Active {
		bindings[moveWorkspaceFileCursorIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			movement := intent.(moveWorkspaceFileCursorIntent)
			s.SetState(func() {
				if movement.lines != 0 {
					next := max(1, min(max(1, len(s.lines)), s.cursorLine+movement.lines))
					if s.selectionAnchor > 0 {
						next = max(s.selectionAnchor-protocol.MaxAnnotationRangeLines+1, min(s.selectionAnchor+protocol.MaxAnnotationRangeLines-1, next))
					}
					s.cursorLine = next
					s.pendingReveal = s.cursorLine
				}
				if movement.columns != 0 {
					s.scroll.ScrollBy(movement.columns, 0)
				}
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
		bindings[toggleWorkspaceFileSelectionIntent{}.IntentType()] = func(_ ui.EventContext, _ ui.Intent) ui.EventResult {
			s.SetState(func() {
				if s.selectionAnchor > 0 {
					s.selectionAnchor = 0
				} else {
					s.selectionAnchor = s.cursorLine
				}
				s.cachedBody = nil
			})
			return ui.EventHandled
		}
		shortcuts["v"] = toggleWorkspaceFileSelectionIntent{}
		if w.OnCreateAnnotation != nil && s.hasContent && s.read.Revision != "" && (s.loadState == workspaceFileReady || s.loadState == workspaceFileTruncated) {
			bindings[annotateWorkspaceFileIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
				start, end := s.selectedRange()
				s.beginNewComment(w, start, end)
				return ui.EventHandled
			}
			shortcuts["c"] = annotateWorkspaceFileIntent{}
		}
		if annotation, ok := s.editableAnnotationAtCursor(w); ok {
			if w.OnLoadAnnotation != nil {
				bindings[editWorkspaceFileAnnotationIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
					s.beginEditComment(w, annotation)
					return ui.EventHandled
				}
				shortcuts["e"] = editWorkspaceFileAnnotationIntent{}
			}
			if w.OnRemoveAnnotation != nil {
				bindings[removeWorkspaceFileAnnotationIntent{}.IntentType()] = func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
					w.OnRemoveAnnotation(ctx, annotation.ID)
					return ui.EventHandled
				}
				shortcuts["d"] = removeWorkspaceFileAnnotationIntent{}
			}
		}
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
	content = mouseReleaseListener{Child: content, OnRelease: func(ui.EventContext) { s.finishGutterComment(w) }}
	content = mouseActivator{Child: content, DefaultMouseShape: true, OnPrimaryDownCapture: func(event ui.EventContext) {
		s.mouseSelectionAnchor = 0
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
		start, end := s.selectedRange()
		rows, contentWidth, contentHeight := s.fileRows(result, theme, semantic, w, start, end)
		contentKey := s.read.Revision + ":plain"
		if s.commenting {
			contentKey = fmt.Sprintf("%s:comment:%d:%d:%d:%d", s.read.Revision, s.commentAnchor.StartLine, s.commentAnchor.EndLine, s.commentAnnotationID, s.commentFocusGeneration)
		}
		pane := ui.Widget(keyedWorkspaceFileContent{Key: contentKey, Child: ui.ScrollPane{Controller: &s.scroll, Child: ui.SizedBox{
			Width: max(s.viewportWidth, contentWidth), Height: contentHeight,
			Child: ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows},
		}}})
		pane = ui.Scrollbar{
			Child:      pane,
			ThumbStyle: ui.Style{Foreground: semantic.Token(kittheme.TokenScrollbarForeground)},
			TrackStyle: ui.Style{Foreground: semantic.Token(kittheme.TokenScrollbarBackground)},
		}
		pane = widthProbe{WidthChanged: func(width int) {
			if width != s.viewportWidth {
				s.viewportWidth = width
				s.MarkNeedsBuild()
			}
			if s.commenting && s.commentReveal && !s.commentScrollQueued {
				s.commentScrollQueued = true
				s.Context().Runtime().Dispatch(func() {
					s.commentScrollQueued = false
					s.revealCommentEditor(w)
				})
			} else if s.pendingReveal > 0 && !s.cursorScrollQueued {
				s.cursorScrollQueued = true
				s.Context().Runtime().Dispatch(func() {
					s.cursorScrollQueued = false
					s.revealCursor(w)
				})
			}
		}, Child: pane}
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

type keyedWorkspaceFileContent struct {
	Key   string
	Child ui.Widget
}

func (w keyedWorkspaceFileContent) WidgetKey() ui.KeyValue          { return ui.KeyValue(w.Key) }
func (w keyedWorkspaceFileContent) Build(ui.BuildContext) ui.Widget { return w.Child }

func (s *workspaceFilePaneState) beginNewComment(w workspaceFilePane, start, end int) {
	s.SetState(func() {
		s.commentOperation++
		s.commenting = true
		s.commentLoading = false
		s.commentBody = ""
		s.commentError = ""
		s.commentLoadFailed = false
		s.commentAnnotationID = 0
		s.commentReveal = true
		s.commentAnchor = protocol.WorkspaceFileAnnotationAnchor{
			WorkspaceID: s.read.Workspace.WorkspaceID, Path: w.Descriptor.Path, FileRevision: s.read.Revision,
			StartLine: start, EndLine: end,
		}
	})
}

func (s *workspaceFilePaneState) beginGutterComment(w workspaceFilePane, line int) {
	if s.commenting || s.commentPending || w.OnCreateAnnotation == nil || !s.hasContent || s.read.Revision == "" || s.loadState != workspaceFileReady && s.loadState != workspaceFileTruncated {
		return
	}
	s.SetState(func() {
		s.mouseSelectionAnchor = line
		s.mouseSelectionGeneration = w.MouseGestures.Generation()
		s.cursorLine = line
		s.selectionAnchor = 0
	})
}

func (s *workspaceFilePaneState) extendGutterComment(line int) {
	w := s.Widget().(workspaceFilePane)
	if w.MouseGestures.Generation() != s.mouseSelectionGeneration {
		s.mouseSelectionAnchor = 0
		return
	}
	if s.mouseSelectionAnchor <= 0 || s.commenting {
		return
	}
	line = max(1, min(len(s.lines), line))
	line = max(s.mouseSelectionAnchor-protocol.MaxAnnotationRangeLines+1, min(s.mouseSelectionAnchor+protocol.MaxAnnotationRangeLines-1, line))
	s.SetState(func() {
		s.selectionAnchor = s.mouseSelectionAnchor
		s.cursorLine = line
	})
}

func (s *workspaceFilePaneState) finishGutterComment(w workspaceFilePane) {
	if s.mouseSelectionAnchor <= 0 || w.MouseGestures.ReleasedGeneration() != s.mouseSelectionGeneration {
		s.mouseSelectionAnchor = 0
		return
	}
	start, end := s.selectedRange()
	s.mouseSelectionAnchor = 0
	s.beginNewComment(w, start, end)
}

func (s *workspaceFilePaneState) beginEditComment(w workspaceFilePane, annotation protocol.AnnotationSummary) {
	anchor := annotation.Anchor.WorkspaceFile
	if s.commenting || anchor == nil || annotation.Stale || w.OnLoadAnnotation == nil {
		return
	}
	var operation uint64
	s.SetState(func() {
		s.commentOperation++
		operation = s.commentOperation
		s.commenting = true
		s.commentPending = false
		s.commentLoading = true
		s.commentError = ""
		s.commentLoadFailed = false
		s.commentBody = annotation.BodyPreview
		s.commentAnnotationID = annotation.ID
		s.commentAnchor = *anchor
		s.commentReveal = true
	})
	s.commentLoadCancel = w.OnLoadAnnotation(annotation.ID, func(body string, err error) {
		if s.disposed || s.commentOperation != operation || s.commentAnnotationID != annotation.ID {
			return
		}
		s.SetState(func() {
			s.commentLoadCancel = nil
			s.commentLoading = false
			if err != nil {
				s.commentError = err.Error()
				s.commentLoadFailed = true
				return
			}
			s.commentBody = body
			s.commentCursorEndGeneration++
			s.commentReveal = true
		})
	})
}

func (s *workspaceFilePaneState) closeCommentEditor() {
	s.commentOperation++
	if s.commentLoadCancel != nil {
		s.commentLoadCancel()
		s.commentLoadCancel = nil
	}
	if s.commentAnchor.EndLine > 0 {
		s.pendingReveal = s.commentAnchor.EndLine
	}
	s.commenting = false
	s.commentBody = ""
	s.commentPending = false
	s.commentLoading = false
	s.commentError = ""
	s.commentLoadFailed = false
	s.commentAnchor = protocol.WorkspaceFileAnnotationAnchor{}
	s.commentAnnotationID = 0
	s.commentReveal = false
	s.commentScrollQueued = false
	s.cursorScrollQueued = false
	s.cachedBody = nil
}

func (s *workspaceFilePaneState) submitComment(w workspaceFilePane, body string) {
	if !s.commenting || s.commentPending || strings.TrimSpace(body) == "" || s.commentLoadFailed {
		return
	}
	if s.commentAnnotationID == 0 && w.OnCreateAnnotation == nil || s.commentAnnotationID != 0 && w.OnUpdateAnnotation == nil {
		return
	}
	s.SetState(func() {
		s.commentBody = body
		s.commentPending = true
		s.commentError = ""
	})
	done := func(err error) {
		if s.disposed {
			return
		}
		s.SetState(func() {
			if err != nil {
				s.commentPending = false
				s.commentError = err.Error()
				return
			}
			s.closeCommentEditor()
			s.selectionAnchor = 0
		})
	}
	if s.commentAnnotationID != 0 {
		w.OnUpdateAnnotation(s.commentAnnotationID, body, done)
	} else {
		anchor := s.commentAnchor
		w.OnCreateAnnotation(protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &anchor}, body, done)
	}
}

func (s *workspaceFilePaneState) selectedRange() (int, int) {
	start, end := s.cursorLine, s.cursorLine
	if s.selectionAnchor > 0 {
		start = min(s.selectionAnchor, s.cursorLine)
		end = max(s.selectionAnchor, s.cursorLine)
	}
	return max(1, start), max(1, end)
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

func (s *workspaceFilePaneState) editableAnnotationAtCursor(w workspaceFilePane) (protocol.AnnotationSummary, bool) {
	for _, annotation := range w.Annotations {
		anchor := annotation.Anchor.WorkspaceFile
		if anchor != nil && !annotation.Stale && anchor.WorkspaceID == s.read.Workspace.WorkspaceID && anchor.Path == w.Descriptor.Path && anchor.FileRevision == s.read.Revision && s.cursorLine >= anchor.StartLine && s.cursorLine <= anchor.EndLine {
			return annotation, true
		}
	}
	return protocol.AnnotationSummary{}, false
}

func (s *workspaceFilePaneState) footerText(w workspaceFilePane) string {
	if s.commenting {
		return "Inline comment · enter save · shift+enter newline · esc cancel"
	}
	if s.loadState == workspaceFileFrozen {
		return "Frozen evidence · navigation only"
	}
	_, hasEditableAnnotation := s.editableAnnotationAtCursor(w)
	parts := []string{"↑↓ lines", "←→ columns"}
	if hasEditableAnnotation {
		parts = []string{"↑↓", "←→"}
	}
	if s.loadState != workspaceFileLoading {
		parts = append(parts, "r refresh")
	}
	parts = append(parts, "v select")
	if hasEditableAnnotation {
		parts = append(parts, "e edit", "d del")
	}
	parts = append(parts, "c comment")
	if s.loadState == workspaceFileTruncated {
		parts = append([]string{"Preview truncated (" + strings.ReplaceAll(s.read.TruncationReason, "_", " ") + ")"}, parts...)
	} else if s.loadState == workspaceFileStaleFile {
		parts = append([]string{"File changed"}, parts...)
	} else if s.loadState == workspaceFileLoading && s.hasContent {
		parts = append([]string{"Refreshing…"}, parts...)
	}
	return strings.Join(parts, " · ")
}

func (s *workspaceFilePaneState) revealCommentEditor(w workspaceFilePane) {
	if !s.commenting || !s.scroll.Attached() {
		return
	}
	vertical := s.scroll.Metrics(ui.ScrollVertical)
	top := workspaceCommentEditorTop(w.Annotations, s.read.Workspace.WorkspaceID, w.Descriptor.Path, s.read.Revision, s.commentAnchor.EndLine, s.commentAnnotationID)
	bottom := top + s.inlineCommentEditorHeight()
	viewportHeight := max(1, vertical.ViewportHeight)
	target := vertical.ScrollOffset
	if s.inlineCommentEditorHeight() >= viewportHeight || top < vertical.ScrollOffset {
		target = top
	} else if bottom > vertical.ScrollOffset+viewportHeight {
		target = bottom - viewportHeight
	}
	s.scroll.ScrollTo(s.scroll.Metrics(ui.ScrollHorizontal).ScrollOffset, max(0, target))
	if vertical.MaxScrollOffset >= target {
		s.commentReveal = false
		s.pendingReveal = 0
	} else {
		s.MarkNeedsBuild()
	}
}

func (s *workspaceFilePaneState) revealCursor(w workspaceFilePane) {
	if s.pendingReveal <= 0 || !s.scroll.Attached() {
		return
	}
	vertical := s.scroll.Metrics(ui.ScrollVertical)
	horizontal := s.scroll.Metrics(ui.ScrollHorizontal).ScrollOffset
	row := max(0, s.pendingReveal-1+workspaceAnnotationRowsBefore(w.Annotations, s.read.Workspace.WorkspaceID, w.Descriptor.Path, s.read.Revision, s.pendingReveal))
	viewportHeight := max(1, vertical.ViewportHeight)
	if row < vertical.ScrollOffset {
		s.scroll.ScrollTo(horizontal, row)
	} else if row >= vertical.ScrollOffset+viewportHeight {
		s.scroll.ScrollTo(horizontal, max(0, row-viewportHeight+1))
	}
	s.pendingReveal = 0
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

func (s *workspaceFilePaneState) fileRows(result highlight.Result, theme ui.Theme, semantic SemanticTheme, w workspaceFilePane, selectionStart, selectionEnd int) ([]ui.Widget, int, int) {
	spans := workspaceFileSpansWithSelection(result, theme, semantic, selectionStart, selectionEnd, w.Annotations, s.read.Workspace.WorkspaceID, w.Descriptor.Path, s.read.Revision)
	visualRows := splitWorkspaceFileSpans(spans)
	rows := make([]ui.Widget, 0, len(visualRows)+1)
	rowIndex := 0
	contentWidth := 1
	contentHeight := 0
	lineNumberWidth := len(strconv.Itoa(max(1, len(s.lines))))
	for _, line := range s.lines {
		contentWidth = max(contentWidth, len(line)+lineNumberWidth+5)
	}
	for _, annotation := range w.Annotations {
		for _, bodyLine := range workspaceAnnotationBodyLines(annotation.BodyPreview) {
			contentWidth = max(contentWidth, len(bodyLine)+lineNumberWidth+5)
		}
	}
	rowWidth := max(s.viewportWidth, contentWidth)
	for lineNumber := 1; lineNumber <= len(s.lines) && rowIndex < len(visualRows); lineNumber++ {
		style := ui.Style{}
		if lineNumber >= selectionStart && lineNumber <= selectionEnd {
			style.Background = theme.SurfaceHovered
		}
		lineSpans := visualRows[rowIndex]
		lineContent := ui.Widget(ui.RichText{Spans: lineSpans, SoftWrap: false})
		if len(lineSpans) > 0 {
			gutter := ui.Widget(ui.RichText{Spans: lineSpans[:1], SoftWrap: false})
			if w.OnCreateAnnotation != nil && (s.loadState == workspaceFileReady || s.loadState == workspaceFileTruncated) {
				gutter = mouseActivator{
					Child:     gutter,
					OnPressed: func(ui.EventContext) { s.beginGutterComment(w, lineNumber) },
					OnMotion: func(_ ui.EventContext, mouse ui.Mouse) {
						if mouse.Button == ui.MouseLeftButton {
							s.extendGutterComment(lineNumber)
						} else {
							s.mouseSelectionAnchor = 0
						}
					},
				}
			}
			lineContent = ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{gutter, ui.Expanded(ui.RichText{Spans: lineSpans[1:], SoftWrap: false})}}
		}
		rows = append(rows, ui.SizedBox{Width: rowWidth, Child: ui.DecoratedBox(ui.Decoration{Style: style}, lineContent)})
		rowIndex++
		contentHeight++
		for _, annotation := range w.Annotations {
			anchor := annotation.Anchor.WorkspaceFile
			if anchor == nil || annotation.Stale || anchor.WorkspaceID != s.read.Workspace.WorkspaceID || anchor.Path != w.Descriptor.Path || anchor.FileRevision != s.read.Revision || anchor.EndLine != lineNumber || rowIndex >= len(visualRows) {
				continue
			}
			editingThis := s.commenting && s.commentAnnotationID == annotation.ID
			for bodyLineIndex := range workspaceAnnotationBodyLines(annotation.BodyPreview) {
				if rowIndex >= len(visualRows) {
					break
				}
				if !editingThis {
					noteSpans := visualRows[rowIndex]
					noteContent := ui.Widget(ui.RichText{Spans: noteSpans, SoftWrap: false})
					if bodyLineIndex == 0 && w.OnRemoveAnnotation != nil {
						noteSpans = append([]ui.TextSpan(nil), noteSpans...)
						if len(noteSpans) > 0 {
							noteSpans[0].Text = strings.TrimPrefix(noteSpans[0].Text, strings.Repeat(" ", lineNumberWidth+3))
						}
						text := ui.Widget(ui.RichText{Spans: noteSpans, SoftWrap: false})
						if w.OnLoadAnnotation != nil {
							text = mouseActivator{Child: text, OnPressed: func(ui.EventContext) { s.beginEditComment(w, annotation) }}
						}
						removeLabel := strings.Repeat(" ", lineNumberWidth+1) + glyphTimes + " "
						remove := mouseActivator{Child: ui.Text{Value: removeLabel, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1}, OnPressed: func(event ui.EventContext) { w.OnRemoveAnnotation(event, annotation.ID) }}
						noteContent = ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{remove, ui.Expanded(text)}}
					} else if w.OnLoadAnnotation != nil {
						noteContent = mouseActivator{Child: noteContent, OnPressed: func(ui.EventContext) { s.beginEditComment(w, annotation) }}
					}
					note := ui.DecoratedBox(ui.Decoration{Style: ui.Style{Background: theme.Surface}}, noteContent)
					rows = append(rows, ui.SizedBox{Width: rowWidth, Child: note})
					contentHeight++
				}
				rowIndex++
			}
			if editingThis {
				editorHeight := s.inlineCommentEditorHeight()
				rows = append(rows, ui.SizedBox{Width: rowWidth, Height: editorHeight, Child: s.inlineCommentEditor(theme, w)})
				contentHeight += editorHeight
			}
		}
		commentSlot := ui.SizedBox{Width: rowWidth, Height: 0}
		if s.commenting && s.commentAnnotationID == 0 && s.commentAnchor.EndLine == lineNumber {
			editorHeight := s.inlineCommentEditorHeight()
			commentSlot = ui.SizedBox{Width: rowWidth, Height: editorHeight, Child: s.inlineCommentEditor(theme, w)}
			contentHeight += editorHeight
		}
		rows = append(rows, commentSlot)
	}
	if s.commenting {
		rows = append(rows, ui.SizedBox{Width: rowWidth, Height: 1, Child: ui.Text{Value: " "}})
		contentHeight++
	}
	return rows, contentWidth, contentHeight
}

func splitWorkspaceFileSpans(spans []ui.TextSpan) [][]ui.TextSpan {
	rows := [][]ui.TextSpan{{}}
	for _, span := range spans {
		parts := strings.Split(span.Text, "\n")
		for index, part := range parts {
			if part != "" {
				copy := span
				copy.Text = part
				rows[len(rows)-1] = append(rows[len(rows)-1], copy)
			}
			if index+1 < len(parts) {
				rows = append(rows, []ui.TextSpan{})
			}
		}
	}
	return rows
}

func (s *workspaceFilePaneState) inlineCommentEditor(theme ui.Theme, w workspaceFilePane) ui.Widget {
	children := []ui.Widget{ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Surface}}}
	if s.commentError != "" {
		children = append(children, ui.Text{Value: "Could not save: " + s.commentError, Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true, MaxLines: 1})
	}
	if s.commentPending || s.commentLoading {
		status := "Saving…"
		if s.commentLoading {
			status = "Loading…"
		}
		children = append(children, ui.Text{Value: s.commentBody, Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true, MaxLines: 4}, spinnerWithLabel(status, ui.Style{Foreground: theme.MutedForeground}))
	} else if s.commentLoadFailed {
		children = append(children, ui.Text{Value: "Press esc to close", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	} else {
		children = append(children, messageComposer{
			Value: s.commentBody, Placeholder: "Write a comment…", MaxHeight: 4,
			CursorEndGeneration: s.commentCursorEndGeneration,
			OnChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() {
					previousHeight := s.inlineCommentEditorHeight()
					s.commentBody = value
					s.commentError = ""
					if s.inlineCommentEditorHeight() != previousHeight {
						s.commentReveal = true
					}
				})
			},
			OnSubmitted: func(_ ui.EventContext, value string) { s.submitComment(w, value) },
		})
	}
	children = append(children,
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Surface}},
		ui.Text{Value: "enter save · shift+enter newline · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1},
	)
	gutterWidth := len(strconv.Itoa(max(1, len(s.lines)))) + 3
	editor := ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: theme.Surface}},
		ui.Padding(ui.Insets{Left: max(0, gutterWidth-1), Right: 1}, ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, MainAxisSize: ui.MainAxisSizeMin, Children: children}),
	)
	return ui.SizedBox{Height: s.inlineCommentEditorHeight(), Child: editor}
}

func (s *workspaceFilePaneState) inlineCommentEditorHeight() int {
	inputWidth := max(12, s.viewportWidth-4)
	inputLines := 0
	for _, line := range strings.Split(s.commentBody, "\n") {
		inputLines += max(1, (utf8.RuneCountInString(line)+inputWidth-1)/inputWidth)
	}
	inputLines = max(1, min(4, inputLines))
	height := inputLines + 3
	if s.commentError != "" {
		height++
	}
	if s.commentPending || s.commentLoading {
		height++
	}
	return height
}

func workspaceFileSpans(result highlight.Result, theme ui.Theme, semantic SemanticTheme) []ui.TextSpan {
	return workspaceFileSpansWithSelection(result, theme, semantic, 0, 0, nil, "", "", "")
}

func workspaceFileSpansWithSelection(result highlight.Result, theme ui.Theme, semantic SemanticTheme, selectionStart, selectionEnd int, annotations []protocol.AnnotationSummary, workspaceID, filePath, revision string) []ui.TextSpan {
	lines := strings.Split(result.Source, "\n")
	if strings.HasSuffix(result.Source, "\n") {
		lines = lines[:len(lines)-1]
	}
	width := len(strconv.Itoa(max(1, len(lines))))
	spans := make([]ui.TextSpan, 0, len(lines)*6)
	offset := 0
	captureIndex := 0
	for index, line := range lines {
		lineNumber := index + 1
		lineSpanStart := len(spans)
		marker := glyphTableSeparator
		gutterStyle := ui.Style{Foreground: theme.MutedForeground}
		for _, annotation := range annotations {
			anchor := annotation.Anchor.WorkspaceFile
			if anchor != nil && !annotation.Stale && anchor.WorkspaceID == workspaceID && anchor.Path == filePath && anchor.FileRevision == revision && lineNumber >= anchor.StartLine && lineNumber <= anchor.EndLine {
				marker = glyphDiamond
				gutterStyle.Foreground = theme.AccentText
				break
			}
		}
		spans = append(spans, ui.TextSpan{Text: fmt.Sprintf("%*d %s ", width, lineNumber, marker), Style: gutterStyle})
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
		if selectionStart > 0 && lineNumber >= selectionStart && lineNumber <= selectionEnd {
			for spanIndex := lineSpanStart; spanIndex < len(spans); spanIndex++ {
				spans[spanIndex].Style.Background = theme.SurfaceHovered
			}
		}
		for _, annotation := range annotations {
			anchor := annotation.Anchor.WorkspaceFile
			if anchor == nil || annotation.Stale || anchor.WorkspaceID != workspaceID || anchor.Path != filePath || anchor.FileRevision != revision || anchor.EndLine != lineNumber {
				continue
			}
			noteStyle := ui.Style{Foreground: theme.AccentText, Background: theme.Surface}
			for _, bodyLine := range workspaceAnnotationBodyLines(annotation.BodyPreview) {
				spans = append(spans, ui.TextSpan{Text: "\n"}, ui.TextSpan{Text: strings.Repeat(" ", width+3) + bodyLine, Style: noteStyle})
			}
		}
		if index+1 < len(lines) {
			spans = append(spans, ui.TextSpan{Text: "\n"})
		}
		offset = lineEnd + 1
	}
	return spans
}

func workspaceAnnotationSummariesEqual(left, right []protocol.AnnotationSummary) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID || left[index].BodyPreview != right[index].BodyPreview || left[index].Stale != right[index].Stale || left[index].Anchor.Kind != right[index].Anchor.Kind || left[index].Anchor.WorkspaceFile == nil != (right[index].Anchor.WorkspaceFile == nil) || left[index].Anchor.WorkingTreeDiff == nil != (right[index].Anchor.WorkingTreeDiff == nil) {
			return false
		}
		if left[index].Anchor.WorkspaceFile != nil && *left[index].Anchor.WorkspaceFile != *right[index].Anchor.WorkspaceFile || left[index].Anchor.WorkingTreeDiff != nil && *left[index].Anchor.WorkingTreeDiff != *right[index].Anchor.WorkingTreeDiff {
			return false
		}
	}
	return true
}

func workspaceAnnotationBodyLines(body string) []string {
	return strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
}

func workspaceCommentEditorTop(annotations []protocol.AnnotationSummary, workspaceID, filePath, revision string, endLine int, editingID uint64) int {
	row := endLine
	for _, annotation := range annotations {
		anchor := annotation.Anchor.WorkspaceFile
		if anchor != nil && !annotation.Stale && anchor.WorkspaceID == workspaceID && anchor.Path == filePath && anchor.FileRevision == revision && anchor.EndLine < endLine {
			row += len(workspaceAnnotationBodyLines(annotation.BodyPreview))
		}
	}
	for _, annotation := range annotations {
		anchor := annotation.Anchor.WorkspaceFile
		if anchor == nil || annotation.Stale || anchor.WorkspaceID != workspaceID || anchor.Path != filePath || anchor.FileRevision != revision || anchor.EndLine != endLine {
			continue
		}
		if editingID != 0 && annotation.ID == editingID {
			break
		}
		row += len(workspaceAnnotationBodyLines(annotation.BodyPreview))
	}
	return row
}

func workspaceAnnotationRowsBefore(annotations []protocol.AnnotationSummary, workspaceID, filePath, revision string, line int) int {
	rows := 0
	for _, annotation := range annotations {
		anchor := annotation.Anchor.WorkspaceFile
		if anchor != nil && !annotation.Stale && anchor.WorkspaceID == workspaceID && anchor.Path == filePath && anchor.FileRevision == revision && anchor.EndLine < line {
			rows += len(workspaceAnnotationBodyLines(annotation.BodyPreview))
		}
	}
	return rows
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
