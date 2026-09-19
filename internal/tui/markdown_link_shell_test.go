package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type markdownLinkShell struct{ state *markdownLinkShellState }

func (w markdownLinkShell) CreateState() ui.State { return w.state }

type markdownLinkShellState struct {
	appState
	scroll ui.ScrollController
	opened []string
	copied string
}

func (*markdownLinkShellState) InitState() {}
func (*markdownLinkShellState) Dispose()   {}
func (s *markdownLinkShellState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}
func (s *markdownLinkShellState) Build(ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	return keyShortcuts{Bindings: ui.ShortcutMap{"Alt+y": ui.CopySelectionTextIntent{OnCopied: func(text string) { s.copied = text }}}, Child: shellView{
		Snapshot:  shellSnapshot{Phase: s.phase, Session: s.session, Messages: s.messages, Scroll: &s.scroll, PendingInteractions: s.pendingInteractions},
		Callbacks: shellCallbacks{InputOwner: s.inputOwner, OpenURL: func(_ ui.EventContext, url string) { s.opened = append(s.opened, url) }},
	}}
}
func mountMarkdownLinkShell(t *testing.T, source string) (*uitest.App, *markdownLinkShellState) {
	t.Helper()
	state := &markdownLinkShellState{appState: appState{phase: phaseReady, session: protocol.SessionInfo{ID: "session_links", Name: "Links", Model: "test/model"}, messages: []transcriptMessage{{ID: "message", TurnID: "turn", Role: "assistant", Text: source}}}}
	app := uitest.New(markdownLinkShell{state})
	app.Pump(100, 28)
	return app, state
}
func sendMarkdownLinkMouse(app *uitest.App, x, y int, event vaxis.EventType) {
	app.Send(vaxis.Mouse{Col: x, Row: y, Button: vaxis.MouseLeftButton, EventType: event})
}
func TestMarkdownLinkShellPrimaryReleaseOpensSafeLiteralDestination(t *testing.T) {
	for _, target := range []string{"https://example.com/docs", "http://localhost:8080/help", "mailto:hello@example.com"} {
		t.Run(target, func(t *testing.T) {
			app, state := mountMarkdownLinkShell(t, "[Read docs]("+target+")")
			x, y := findTextCell(t, paintedRows(app, 100, 28), "Read docs")
			if app.Cell(x, y).Hyperlink != target {
				t.Fatalf("native link=%q", app.Cell(x, y).Hyperlink)
			}
			findTextCell(t, paintedRows(app, 100, 28), target)
			sendMarkdownLinkMouse(app, x, y, vaxis.EventPress)
			if len(state.opened) != 0 {
				t.Fatalf("opened before release: %v", state.opened)
			}
			sendMarkdownLinkMouse(app, x, y, vaxis.EventRelease)
			if len(state.opened) != 1 || state.opened[0] != target {
				t.Fatalf("opened=%v, want %s", state.opened, target)
			}
		})
	}
}
func TestMarkdownLinkShellDragSelectsAndCopiesWithoutOpening(t *testing.T) {
	app, state := mountMarkdownLinkShell(t, "[Read docs](https://example.com/docs)")
	x, y := findTextCell(t, paintedRows(app, 100, 28), "Read docs")
	sendMarkdownLinkMouse(app, x, y, vaxis.EventPress)
	sendMarkdownLinkMouse(app, x+4, y, vaxis.EventMotion)
	sendMarkdownLinkMouse(app, x+4, y, vaxis.EventRelease)
	app.Pump(100, 28)
	for col := x; col < x+4; col++ {
		if got := app.Cell(col, y).Style.Background; got != ui.DefaultTheme().Selection {
			t.Fatalf("selected cell %d color=%v", col, got)
		}
	}
	app.Send(vaxis.Key{Keycode: 'y', Modifiers: vaxis.ModAlt})
	if state.copied != "Read" {
		t.Fatalf("copied=%q", state.copied)
	}
	if len(state.opened) != 0 {
		t.Fatalf("drag unexpectedly opened %v", state.opened)
	}
	// Returning to the original cell after a drag is still selection, not a click.
	sendMarkdownLinkMouse(app, x, y, vaxis.EventPress)
	sendMarkdownLinkMouse(app, x+5, y, vaxis.EventMotion)
	sendMarkdownLinkMouse(app, x, y, vaxis.EventMotion)
	sendMarkdownLinkMouse(app, x, y, vaxis.EventRelease)
	if len(state.opened) != 0 {
		t.Fatalf("returning drag opened %v", state.opened)
	}
}
func TestMarkdownLinkShellBackgroundClickDoesNotOpenDuringInteraction(t *testing.T) {
	app, state := mountMarkdownLinkShell(t, "[Read docs](https://example.com/docs)")
	state.SetState(func() {
		state.pendingInteractions = []protocol.InteractionRequest{{ID: "question", Kind: protocol.InteractionInput, Title: "Answer"}}
	})
	app.Pump(100, 28)
	x, y := findTextCell(t, paintedRows(app, 100, 28), "Read docs")
	sendMarkdownLinkMouse(app, x, y, vaxis.EventPress)
	sendMarkdownLinkMouse(app, x, y, vaxis.EventRelease)
	if len(state.opened) != 0 {
		t.Fatalf("background link opened while dock owns input: %v", state.opened)
	}
}

func TestMarkdownLinkShellOwnerChangeBeforePaintCancelsPress(t *testing.T) {
	app, state := mountMarkdownLinkShell(t, "[Read docs](https://example.com/docs)")
	x, y := findTextCell(t, paintedRows(app, 100, 28), "Read docs")
	sendMarkdownLinkMouse(app, x, y, vaxis.EventPress)
	state.SetState(func() {
		state.pendingInteractions = []protocol.InteractionRequest{{ID: "question", Kind: protocol.InteractionInput, Title: "Answer"}}
	})
	// The interaction arrived between terminal events, before the next frame.
	sendMarkdownLinkMouse(app, x, y, vaxis.EventRelease)
	if len(state.opened) != 0 {
		t.Fatalf("stale owner opened %v", state.opened)
	}
}

func TestMarkdownLinkShellWrappedDestinationAndWideGlyphUseFullURL(t *testing.T) {
	const target = "https://example.com/a/very/long/documentation/path"
	app, state := mountMarkdownLinkShell(t, "[文 documentation]("+target+")")
	app.Pump(36, 28)
	x, y := findTextCell(t, paintedRows(app, 36, 28), "文")
	// Both terminal cells occupied by a wide glyph are part of the same link.
	sendMarkdownLinkMouse(app, x+1, y, vaxis.EventPress)
	sendMarkdownLinkMouse(app, x+1, y, vaxis.EventRelease)
	if len(state.opened) != 1 || state.opened[0] != target {
		t.Fatalf("wide glyph opened=%v", state.opened)
	}
	endX, endY := -1, -1
	for row := y + 1; row < 28; row++ {
		for col := 0; col < 36; col++ {
			if app.Cell(col, row).Hyperlink == target {
				endX, endY = col, row
			}
		}
	}
	if endY <= y {
		t.Fatal("expected a wrapped link continuation")
	}
	sendMarkdownLinkMouse(app, endX, endY, vaxis.EventPress)
	sendMarkdownLinkMouse(app, endX, endY, vaxis.EventRelease)
	if len(state.opened) != 2 || state.opened[1] != target {
		t.Fatalf("wrapped URL opened=%v", state.opened)
	}
}

func TestMarkdownLinkShellRepaintedTargetCancelsOldPress(t *testing.T) {
	app, state := mountMarkdownLinkShell(t, "[Read docs](https://example.com/old)")
	x, y := findTextCell(t, paintedRows(app, 100, 28), "Read docs")
	sendMarkdownLinkMouse(app, x, y, vaxis.EventPress)
	state.SetState(func() { state.messages[0].Text = "[Read docs](https://example.com/new)" })
	app.Pump(100, 28)
	sendMarkdownLinkMouse(app, x, y, vaxis.EventRelease)
	if len(state.opened) != 0 {
		t.Fatalf("stale press opened=%v", state.opened)
	}
	sendMarkdownLinkMouse(app, x, y, vaxis.EventPress)
	sendMarkdownLinkMouse(app, x, y, vaxis.EventRelease)
	if len(state.opened) != 1 || state.opened[0] != "https://example.com/new" {
		t.Fatalf("fresh click opened=%v", state.opened)
	}
}
