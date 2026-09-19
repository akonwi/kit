package tui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/highlight"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type markdownCodeTestCall struct {
	context context.Context
	request highlight.Request
	result  chan highlight.Result
}

type markdownCodeTestHighlighter struct{ calls chan markdownCodeTestCall }

type markdownCodeHighlighterFunc func(context.Context, highlight.Request) highlight.Result

func (f markdownCodeHighlighterFunc) Highlight(ctx context.Context, request highlight.Request) highlight.Result {
	return f(ctx, request)
}

func (h markdownCodeTestHighlighter) Highlight(ctx context.Context, request highlight.Request) highlight.Result {
	call := markdownCodeTestCall{context: ctx, request: request, result: make(chan highlight.Result, 1)}
	h.calls <- call
	return <-call.result
}

type markdownCodeTestDispatch struct {
	mu        sync.Mutex
	callbacks []func()
}

func (d *markdownCodeTestDispatch) dispatch(callback func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.callbacks = append(d.callbacks, callback)
}

func (d *markdownCodeTestDispatch) flush() {
	d.mu.Lock()
	callbacks := d.callbacks
	d.callbacks = nil
	d.mu.Unlock()
	for _, callback := range callbacks {
		callback()
	}
}

func (d *markdownCodeTestDispatch) pending() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.callbacks)
}

type markdownCodeLifecycleHarness struct{ state *markdownCodeLifecycleState }

func (w markdownCodeLifecycleHarness) CreateState() ui.State { return w.state }

type markdownCodeLifecycleState struct {
	ui.StateBase
	source      string
	show        bool
	highlighter highlight.Highlighter
	dispatch    func(func())
	codeState   *markdownCodeViewState
	baseStyle   ui.Style
}

func (s *markdownCodeLifecycleState) Build(ui.BuildContext) ui.Widget {
	child := ui.Widget(ui.Text{Value: "unmounted"})
	if s.show {
		child = markdownCodeView{Language: "go", Source: s.source, BaseStyle: s.baseStyle, testState: s.codeState}
	}
	return ui.Provider[markdownCodeHighlighting]{
		Value: markdownCodeHighlighting{Highlighter: s.highlighter, Dispatch: s.dispatch},
		Child: child,
	}
}

func waitMarkdownCodeDispatch(t *testing.T, dispatch *markdownCodeTestDispatch) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for dispatch.pending() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if dispatch.pending() == 0 {
		t.Fatal("highlight result did not dispatch")
	}
}

func receiveMarkdownCodeCall(t *testing.T, calls <-chan markdownCodeTestCall) markdownCodeTestCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("highlight request did not start")
		return markdownCodeTestCall{}
	}
}

func waitMarkdownCodeCanceled(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("highlight request was not canceled")
	}
}

func TestMarkdownCodeHighlightLifecycleDropsStaleAndUnmountedResults(t *testing.T) {
	calls := make(chan markdownCodeTestCall, 4)
	highlighter := markdownCodeTestHighlighter{calls: calls}
	dispatch := &markdownCodeTestDispatch{}
	state := &markdownCodeLifecycleState{source: "old\tvalue", show: true, highlighter: highlighter, dispatch: dispatch.dispatch}
	application := uitest.New(markdownCodeLifecycleHarness{state: state})
	application.Pump(30, 4)
	oldCall := receiveMarkdownCodeCall(t, calls)
	if oldCall.request.Source != "old  value" {
		t.Fatalf("pre-expanded request source = %q", oldCall.request.Source)
	}
	if !application.Contains("old  value") {
		t.Fatalf("sanitized fallback was not immediate: %q", application.Text())
	}

	state.SetState(func() { state.source = "current" })
	application.Pump(30, 4)
	waitMarkdownCodeCanceled(t, oldCall.context)
	currentCall := receiveMarkdownCodeCall(t, calls)
	currentCall.result <- highlight.Result{Source: "current", Language: "go", Spans: []highlight.Span{{Start: 0, End: 7, Role: highlight.Keyword}}}
	waitMarkdownCodeDispatch(t, dispatch)

	// A callback already queued for the previous generation must not replace
	// the next source's immediate readable fallback.
	state.SetState(func() { state.source = "replacement" })
	application.Pump(30, 4)
	replacementCall := receiveMarkdownCodeCall(t, calls)
	dispatch.flush()
	application.Pump(30, 4)
	if !application.Contains("replacement") {
		t.Fatalf("queued stale result replaced current fallback: %q", application.Text())
	}
	replacementCall.result <- highlight.Result{Source: "replacement", Language: "go", Spans: []highlight.Span{{Start: 0, End: 11, Role: highlight.Keyword}}}
	waitMarkdownCodeDispatch(t, dispatch)
	dispatch.flush()
	application.Pump(30, 4)

	oldCall.result <- highlight.Result{Source: oldCall.request.Source, Language: "go"}
	time.Sleep(time.Millisecond)
	if dispatch.pending() != 0 {
		t.Fatal("stale canceled highlight queued a UI callback")
	}

	// Disposal after completion but before callback delivery must make the
	// queued callback inert.
	state.SetState(func() { state.source = "dispose me" })
	application.Pump(30, 4)
	disposeCall := receiveMarkdownCodeCall(t, calls)
	disposeCall.result <- highlight.Result{Source: disposeCall.request.Source, Language: "go"}
	waitMarkdownCodeDispatch(t, dispatch)
	state.SetState(func() { state.show = false })
	application.Pump(30, 4)
	dispatch.flush()
	if dispatch.pending() != 0 || !application.Contains("unmounted") {
		t.Fatalf("unmounted highlight mutated UI: pending=%d text=%q", dispatch.pending(), application.Text())
	}
}

func TestMarkdownCodeUnmountCancelsInFlightHighlight(t *testing.T) {
	calls := make(chan markdownCodeTestCall, 1)
	dispatch := &markdownCodeTestDispatch{}
	state := &markdownCodeLifecycleState{
		source: "blocked", show: true,
		highlighter: markdownCodeTestHighlighter{calls: calls}, dispatch: dispatch.dispatch,
	}
	application := uitest.New(markdownCodeLifecycleHarness{state: state})
	application.Pump(20, 3)
	call := receiveMarkdownCodeCall(t, calls)
	state.SetState(func() { state.show = false })
	application.Pump(20, 3)
	waitMarkdownCodeCanceled(t, call.context)
	call.result <- highlight.Result{Source: call.request.Source, Language: "go"}
	time.Sleep(time.Millisecond)
	if dispatch.pending() != 0 || !application.Contains("unmounted") {
		t.Fatalf("canceled unmount dispatched result: pending=%d text=%q", dispatch.pending(), application.Text())
	}
}

func TestMarkdownCodeInvalidServiceResultsKeepStyledPlainFallback(t *testing.T) {
	tests := []struct {
		name   string
		result func(highlight.Request) highlight.Result
	}{
		{name: "malformed spans", result: func(request highlight.Request) highlight.Result {
			return highlight.Result{Source: request.Source, Language: "go", Spans: []highlight.Span{{Start: 0, End: len(request.Source) + 1, Role: highlight.Keyword}}}
		}},
		{name: "service failure", result: func(request highlight.Request) highlight.Result {
			return highlight.Result{Source: request.Source, Language: "go", Spans: []highlight.Span{{Start: 0, End: len(request.Source), Role: highlight.Keyword}}, Err: errors.New("failed")}
		}},
		{name: "changed source", result: func(highlight.Request) highlight.Result {
			return highlight.Result{Source: "different", Language: "go"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dispatch := &markdownCodeTestDispatch{}
			codeState := &markdownCodeViewState{}
			theme := ui.DefaultTheme()
			base := ui.Style{Foreground: theme.MutedForeground, Attribute: ui.AttrItalic}
			state := &markdownCodeLifecycleState{
				source: "safe", show: true, codeState: codeState, baseStyle: base, dispatch: dispatch.dispatch,
				highlighter: markdownCodeHighlighterFunc(func(_ context.Context, request highlight.Request) highlight.Result { return test.result(request) }),
			}
			application := uitest.New(markdownCodeLifecycleHarness{state: state})
			application.Pump(20, 3)
			waitMarkdownCodeDispatch(t, dispatch)
			dispatch.flush()
			application.Pump(20, 3)
			if !codeState.result.Fallback || codeState.result.Source != "safe" || len(codeState.result.Spans) != 0 {
				t.Fatalf("fallback result = %+v", codeState.result)
			}
			column, row := findTextCell(t, paintedRows(application, 20, 3), "safe")
			cell := application.Cell(column, row)
			if cell.Style.Foreground != theme.MutedForeground || cell.Style.Background != theme.Surface || cell.Style.Attribute&ui.AttrItalic == 0 {
				t.Fatalf("fallback style = %+v", cell.Style)
			}
		})
	}
}

type markdownCodeRunnerBackend struct {
	events    chan ui.Event
	callbacks chan func()
	painter   *ui.Painter
}

func (b *markdownCodeRunnerBackend) Events() <-chan ui.Event { return b.events }
func (*markdownCodeRunnerBackend) Size() ui.Size             { return ui.Size{Width: 30, Height: 4} }
func (b *markdownCodeRunnerBackend) Render(painter *ui.Painter) error {
	b.painter = painter
	return nil
}
func (b *markdownCodeRunnerBackend) Dispatch(callback func())  { b.callbacks <- callback }
func (*markdownCodeRunnerBackend) SetMouseShape(ui.MouseShape) {}
func (*markdownCodeRunnerBackend) Close() error                { return nil }

func TestMarkdownCodeDefaultRuntimeDispatchRequestsCompletionFrame(t *testing.T) {
	calls := make(chan markdownCodeTestCall, 1)
	highlighter := markdownCodeTestHighlighter{calls: calls}
	root := ui.Provider[markdownCodeHighlighting]{
		Value: markdownCodeHighlighting{Highlighter: highlighter},
		Child: markdownCodeView{Language: "go", Source: "const value"},
	}
	backend := &markdownCodeRunnerBackend{events: make(chan ui.Event), callbacks: make(chan func(), 1)}
	runner := ui.NewRunner(ui.NewApp(root), backend, ui.NewFrameScheduler(time.Second/60))
	now := time.Now()
	runner.Start(now)
	if err := runner.HandleFrame(now); err != nil {
		t.Fatal(err)
	}
	call := receiveMarkdownCodeCall(t, calls)
	call.result <- highlight.Result{Source: call.request.Source, Language: "go", Spans: []highlight.Span{{Start: 0, End: 5, Role: highlight.Keyword}}}
	select {
	case callback := <-backend.callbacks:
		callback()
	case <-time.After(time.Second):
		t.Fatal("default runtime dispatcher was not used")
	}
	if err := runner.HandleFrame(now.Add(time.Second / 60)); err != nil {
		t.Fatal(err)
	}
	if backend.painter == nil {
		t.Fatal("completion frame did not paint")
	}
	foundKeyword := false
	for row := 0; row < 4; row++ {
		for column := 0; column < 30; column++ {
			cell := backend.painter.Cell(column, row)
			if cell.Grapheme == "c" && cell.Style.Foreground == ui.DefaultTheme().AccentText {
				foundKeyword = true
			}
		}
	}
	if !foundKeyword {
		t.Fatal("completion frame did not paint semantic keyword style")
	}
}
