package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/highlight"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

// Counting the semantic requests proves resize/theme changes repaint cached
// roles instead of reparsing the same fenced source.
type markdownPresentationHighlighter struct {
	mu       sync.Mutex
	requests []highlight.Request
	backend  highlight.Highlighter
}

func (h *markdownPresentationHighlighter) Highlight(ctx context.Context, request highlight.Request) highlight.Result {
	h.mu.Lock()
	h.requests = append(h.requests, request)
	h.mu.Unlock()
	return h.backend.Highlight(ctx, request)
}
func (h *markdownPresentationHighlighter) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.requests)
}

type markdownPresentationHarness struct{ state *markdownPresentationState }

func (w markdownPresentationHarness) CreateState() ui.State { return w.state }

type markdownPresentationState struct {
	ui.StateBase
	source      string
	base        ui.Style
	theme       ui.Theme
	semantic    SemanticTheme
	highlighter highlight.Highlighter
	dispatch    toolNavigationDispatch
	copied      string
}

func (s *markdownPresentationState) Build(ui.BuildContext) ui.Widget {
	return ui.Provider[markdownCodeHighlighting]{Value: markdownCodeHighlighting{Highlighter: s.highlighter, Dispatch: s.dispatch.dispatch}, Child: ui.Provider[SemanticTheme]{Value: s.semantic, Child: markdownThemedTestSurface(s.theme,
		keyShortcuts{Bindings: ui.ShortcutMap{"Alt+y": ui.CopySelectionTextIntent{OnCopied: func(text string) { s.copied = text }}}, Child: ui.SelectionArea{Child: markdownView{ID: "highlighted", Source: s.source, BaseStyle: s.base}}})}}
}
func (s *markdownPresentationState) pump(app *uitest.App, width, height int) {
	s.dispatch.flush()
	app.Pump(width, height)
}
func mountMarkdownPresentation(t *testing.T, source string) (*uitest.App, *markdownPresentationState, *markdownPresentationHighlighter) {
	t.Helper()
	scheduler := highlight.NewScheduler(1, 4, 1<<20)
	t.Cleanup(scheduler.Close)
	highlighter := &markdownPresentationHighlighter{backend: scheduler}
	theme := ui.DefaultThemeSet().Dark
	state := &markdownPresentationState{source: source, theme: theme, semantic: semanticFallback(theme), highlighter: highlighter}
	state.semantic.SyntaxPalette[string(highlight.Keyword)] = ui.RGB(211, 21, 31)
	state.semantic.SyntaxPalette[string(highlight.String)] = ui.RGB(21, 211, 31)
	app := uitest.New(markdownPresentationHarness{state})
	app.Pump(50, 8)
	return app, state, highlighter
}
func awaitMarkdownPresentation(t *testing.T, app *uitest.App, state *markdownPresentationState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for state.dispatch.pending() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if state.dispatch.pending() == 0 {
		t.Fatal("highlight completion did not arrive")
	}
	state.pump(app, 50, 8)
}
func assertMarkdownCodeCell(t *testing.T, app *uitest.App, width, height int, text string, foreground, background ui.Color) {
	t.Helper()
	x, y := findTextCell(t, paintedRows(app, width, height), text)
	cell := app.Cell(x, y)
	if cell.Style.Foreground != foreground || cell.Style.Background != background {
		t.Fatalf("%q style=%+v, want foreground=%v background=%v", text, cell.Style, foreground, background)
	}
}
func TestMarkdownFenceNativeHighlightRepaintsThemeWithoutReparse(t *testing.T) {
	app, state, highlighter := mountMarkdownPresentation(t, "```golang\nconst café = \"héllo\"\n```")
	awaitMarkdownPresentation(t, app, state)
	assertMarkdownCodeCell(t, app, 50, 8, "const", state.semantic.Syntax("keyword"), state.theme.Surface)
	assertMarkdownCodeCell(t, app, 50, 8, "\"héllo\"", state.semantic.Syntax("string"), state.theme.Surface)
	if got := strings.TrimSpace(paintedRows(app, 50, 8)[0]); got != "const café = \"héllo\"" {
		t.Fatalf("code row=%q", got)
	}
	state.SetState(func() {
		state.theme = ui.DefaultThemeSet().Light
		state.semantic = semanticFallback(state.theme)
		state.semantic.SyntaxPalette["keyword"] = ui.RGB(61, 31, 171)
		state.semantic.SyntaxPalette["string"] = ui.RGB(171, 61, 31)
	})
	state.pump(app, 30, 8)
	assertMarkdownCodeCell(t, app, 30, 8, "const", state.semantic.Syntax("keyword"), state.theme.Surface)
	assertMarkdownCodeCell(t, app, 30, 8, "\"héllo\"", state.semantic.Syntax("string"), state.theme.Surface)
	if highlighter.count() != 1 {
		t.Fatalf("theme/resize reparsed source %d times", highlighter.count())
	}
}
func TestMarkdownFenceWrappingAndCopyPreserveSource(t *testing.T) {
	app, state, _ := mountMarkdownPresentation(t, "```go\n\tconst café = \"héllo\"\n```")
	awaitMarkdownPresentation(t, app, state)
	state.pump(app, 16, 8)
	rows := paintedRows(app, 16, 8)
	// Two spaces expand the tab; the padded code surface soft-wraps at 14 cells.
	if got := strings.TrimRight(rows[0], " "); got != "   const café =" {
		t.Fatalf("wrapped first row=%q", got)
	}
	if got := strings.TrimSpace(rows[1]); got != "\"héllo\"" {
		t.Fatalf("wrapped continuation=%q", got)
	}
	x, y := findTextCell(t, rows, "const")
	app.Click(x, y)
	app.Send(vaxis.Mouse{Col: x + 5, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion})
	app.Send(vaxis.Mouse{Col: x + 5, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})
	state.pump(app, 16, 8)
	for col := x; col < x+5; col++ {
		if got := app.Cell(col, y).Style.Background; got != state.theme.Selection {
			t.Fatalf("selected column %d color=%v", col, got)
		}
	}
	app.Send(vaxis.Key{Keycode: 'y', Modifiers: vaxis.ModAlt})
	if state.copied != "const" {
		t.Fatalf("copied=%q, want const", state.copied)
	}
}
func TestMarkdownFenceUnsupportedLanguageRemainsReadable(t *testing.T) {
	app, state, _ := mountMarkdownPresentation(t, "```not-a-language\n\tplain café\n```")
	awaitMarkdownPresentation(t, app, state)
	if got := strings.TrimRight(paintedRows(app, 50, 8)[0], " "); got != "   plain café" {
		t.Fatalf("plain fallback=%q", got)
	}
	assertMarkdownCodeCell(t, app, 50, 8, "plain", state.theme.Foreground, state.theme.Surface)
}
func TestMarkdownFenceThinkingKeepsMutedItalicStyle(t *testing.T) {
	app, state, _ := mountMarkdownPresentation(t, "```go\nconst café = \"héllo\"\n```")
	state.SetState(func() { state.base = ui.Style{Foreground: state.theme.MutedForeground, Attribute: ui.AttrItalic} })
	awaitMarkdownPresentation(t, app, state)
	assertMarkdownCodeCell(t, app, 50, 8, "const", state.theme.MutedForeground, state.theme.Surface)
	x, y := findTextCell(t, paintedRows(app, 50, 8), "const")
	if app.Cell(x, y).Attribute&ui.AttrItalic != ui.AttrItalic {
		t.Fatal("thinking code must remain italic")
	}
}

func TestMarkdownFenceNestedJavaScriptUsesFenceLanguage(t *testing.T) {
	app, state, _ := mountMarkdownPresentation(t, "> ```js\n> const value = \"hello\";\n> ```")
	awaitMarkdownPresentation(t, app, state)
	assertMarkdownCodeCell(t, app, 50, 8, "const", state.semantic.Syntax("keyword"), state.theme.Surface)
	assertMarkdownCodeCell(t, app, 50, 8, "\"hello\"", state.semantic.Syntax("string"), state.theme.Surface)
	if got := strings.TrimRight(paintedRows(app, 50, 8)[0], " "); got != "│  const value = \"hello\";" {
		t.Fatalf("nested code row=%q", got)
	}
}

func TestShellTranscriptFencedCodeUsesSemanticHighlighting(t *testing.T) {
	scheduler := highlight.NewScheduler(1, 4, 1<<20)
	t.Cleanup(scheduler.Close)
	theme := ui.DefaultThemeSet().Dark
	semantic := semanticFallback(theme)
	semantic.SyntaxPalette["keyword"] = ui.RGB(211, 21, 31)
	dispatch := &toolNavigationDispatch{}
	app := uitest.New(ui.Provider[markdownCodeHighlighting]{Value: markdownCodeHighlighting{Highlighter: scheduler, Dispatch: dispatch.dispatch}, Child: ui.Provider[SemanticTheme]{Value: semantic, Child: markdownThemedTestSurface(theme, shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Scroll: &ui.ScrollController{}, Messages: []transcriptMessage{{ID: "assistant", TurnID: "turn", Role: "assistant", Text: "Example:\n\n```python\nreturn \"hello\"\n```"}},
	}})}})
	app.Pump(80, 24)
	deadline := time.Now().Add(2 * time.Second)
	for dispatch.pending() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if dispatch.pending() == 0 {
		t.Fatal("transcript highlight did not complete")
	}
	dispatch.flush()
	app.Pump(80, 24)
	assertMarkdownCodeCell(t, app, 80, 24, "return", semantic.Syntax("keyword"), theme.Surface)
	if !app.Contains("Example:") {
		t.Fatalf("transcript prose missing: %s", app.Text())
	}
}
