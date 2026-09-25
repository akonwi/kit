package tui

import (
	"context"
	"errors"

	"github.com/akonwi/kit/internal/highlight"
	"go.rockorager.dev/vaxis/ui"
)

var (
	errMarkdownCodeSourceChanged = errors.New("highlighted Markdown source changed")
	markdownCodeHighlightSlots   = make(chan struct{}, highlight.DefaultWorkers+highlight.DefaultQueueDepth)
)

// markdownCodeHighlighting is an optional test and host injection surface.
// Dispatch, when supplied, must marshal callbacks onto the UI thread.
type markdownCodeHighlighting struct {
	Highlighter highlight.Highlighter
	Dispatch    func(func())
}

type markdownCodeView struct {
	Language  string
	Source    string
	BaseStyle ui.Style
	testState *markdownCodeViewState
}

func (w markdownCodeView) CreateState() ui.State {
	if w.testState != nil {
		return w.testState
	}
	return &markdownCodeViewState{}
}

type markdownCodeResult struct {
	generation uint64
	result     highlight.Result
}

type markdownCodeViewState struct {
	ui.StateBase
	request    highlight.Request
	result     highlight.Result
	cancel     context.CancelFunc
	generation uint64
	started    bool
	pending    bool
	disposed   bool
}

func (s *markdownCodeViewState) InitState() {
	s.reset(s.Widget().(markdownCodeView))
}

func (s *markdownCodeViewState) DidUpdateWidget(old ui.Widget) {
	previous := old.(markdownCodeView)
	current := s.Widget().(markdownCodeView)
	if previous.Language != current.Language || previous.Source != current.Source {
		s.reset(current)
	}
}

func (s *markdownCodeViewState) Dispose() {
	s.disposed = true
	s.generation++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *markdownCodeViewState) reset(view markdownCodeView) {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.generation++
	s.request = highlight.Request{Language: view.Language, Source: expandMarkdownTabs(view.Source)}
	s.result = highlight.Plain(s.request, nil)
	s.started = false
	s.pending = false
}

func (s *markdownCodeViewState) start(ctx ui.BuildContext) {
	if s.started || s.disposed {
		return
	}
	s.started = true
	select {
	case markdownCodeHighlightSlots <- struct{}{}:
		s.pending = true
	default:
		// The readable fallback is already painted; do not create an unbounded
		// producer backlog in front of the shared bounded scheduler.
		return
	}
	configuration, _ := ui.Depend[markdownCodeHighlighting](ctx)
	highlighter := configuration.Highlighter
	if highlighter == nil {
		highlighter = highlight.Default
	}
	request := s.request
	expectedSource := s.result.Source
	generation := s.generation
	workContext, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	dispatch := configuration.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	go func() {
		defer func() {
			cancel()
			<-markdownCodeHighlightSlots
		}()
		result := highlight.Validate(highlighter.Highlight(workContext, request))
		if result.Err != nil || result.Fallback {
			result = highlight.Plain(request, result.Err)
		}
		if result.Source != expectedSource {
			result = highlight.Plain(request, errMarkdownCodeSourceChanged)
		}
		if workContext.Err() != nil {
			return
		}
		pending := markdownCodeResult{generation: generation, result: result}
		dispatch(func() { s.applyDispatched(pending) })
	}()
}

func (s *markdownCodeViewState) applyDispatched(pending markdownCodeResult) {
	if s.disposed || pending.generation != s.generation {
		return
	}
	s.SetState(func() {
		if s.disposed || pending.generation != s.generation {
			return
		}
		s.result = pending.result
		s.pending = false
		s.cancel = nil
	})
}

func (s *markdownCodeViewState) Build(ctx ui.BuildContext) ui.Widget {
	view := s.Widget().(markdownCodeView)
	s.start(ctx)
	theme := ui.MustDepend[ui.Theme](ctx)
	semantic, ok := ui.Depend[SemanticTheme](ctx)
	if !ok {
		semantic = semanticFallback(theme)
	}
	base := view.BaseStyle
	if base.Foreground == 0 {
		base.Foreground = theme.Foreground
	}
	base.Background = theme.Surface
	spans := markdownCodeSpans(s.result, theme, semantic, base)
	content := ui.ConstrainedBox{
		Constraints: ui.Constraints{MinHeight: 1},
		Child:       ui.RichText{Spans: spans, SoftWrap: true},
	}
	return ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: theme.Surface}},
		ui.Padding(ui.Symmetric(1, 0), content),
	)
}

func markdownCodeBlock(_ ui.Theme, base ui.Style, language, source string) ui.Widget {
	return markdownCodeView{Language: language, Source: source, BaseStyle: base}
}

func markdownCodeSpans(result highlight.Result, theme ui.Theme, semantic SemanticTheme, base ui.Style) []ui.TextSpan {
	if len(result.Spans) == 0 {
		return []ui.TextSpan{{Text: result.Source, Style: base}}
	}
	spans := make([]ui.TextSpan, 0, len(result.Spans)*2+1)
	position := 0
	appendSpan := func(text string, role highlight.Role) {
		if text == "" {
			return
		}
		style := base
		if markdownSemanticColorAllowed(theme, base) {
			if color := semantic.Syntax(string(role)); color != 0 {
				style.Foreground = color
			}
		}
		spans = append(spans, ui.TextSpan{Text: text, Style: style})
	}
	for _, capture := range result.Spans {
		if capture.Start > position {
			appendSpan(result.Source[position:capture.Start], highlight.Text)
		}
		appendSpan(result.Source[capture.Start:capture.End], capture.Role)
		position = capture.End
	}
	if position < len(result.Source) {
		appendSpan(result.Source[position:], highlight.Text)
	}
	if len(spans) == 0 {
		spans = append(spans, ui.TextSpan{Text: result.Source, Style: base})
	}
	return spans
}
