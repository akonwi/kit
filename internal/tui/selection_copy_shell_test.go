package tui

import (
	"reflect"
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

type selectionCopyBackend struct {
	toastAnimationBackend
	copied []string
}

func (b *selectionCopyBackend) CopyToClipboard(text string) { b.copied = append(b.copied, text) }

func TestShellSelectionCopyPulsesOnlySelectedBackground(t *testing.T) {
	for _, variant := range []struct {
		name  string
		theme ui.Theme
	}{{"dark", ui.DefaultThemeSet().Dark}, {"light", ui.DefaultThemeSet().Light}} {
		t.Run(variant.name, func(t *testing.T) {
			theme := variant.theme
			backend := &selectionCopyBackend{toastAnimationBackend: toastAnimationBackend{size: ui.Size{Width: 80, Height: 24}}}
			observed := ""
			root := markdownThemedTestSurface(theme, shellView{Snapshot: shellSnapshot{Phase: phaseReady, Scroll: &ui.ScrollController{}, Messages: []transcriptMessage{{ID: "copy", TurnID: "turn", Role: "assistant", Text: "copy these words"}}}, Callbacks: shellCallbacks{CopySelection: func(text string) { observed = text }}})
			app := ui.NewApp(root)
			runner := ui.NewRunner(app, backend, nil)
			now := time.Now()
			runner.Start(now)
			if err := runner.HandleFrame(now); err != nil {
				t.Fatal(err)
			}
			x, y := findTextCell(t, toastPainterRows(backend.painter), "copy these words")
			for _, mouse := range []vaxis.Mouse{
				{Col: x, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress},
				{Col: x + 4, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion},
				{Col: x + 4, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease},
			} {
				runner.HandleEvent(mouse, now)
			}
			if err := runner.HandleFrame(now); err != nil {
				t.Fatal(err)
			}
			baseline := backend.painter.Cell(x, y)
			neighbor := backend.painter.Cell(x+5, y)
			if baseline.Background != theme.Selection {
				t.Fatalf("selection background=%v, want %v", baseline.Background, theme.Selection)
			}
			focusBefore := app.DebugSnapshot().Focusables
			runner.HandleEvent(vaxis.Key{Keycode: 'c', Modifiers: vaxis.ModSuper}, now)
			// Pump without advancing animation time, capturing the immediate feedback.
			app.Pump(backend.size)
			pulse := ui.NewPainter(backend.size)
			app.Paint(pulse)
			if observed != "copy" || len(backend.copied) != 1 || backend.copied[0] != "copy" {
				t.Fatalf("copy observed=%q clipboard=%v", observed, backend.copied)
			}
			expected := baseline
			expected.Background = selectionCopyPulseColor(theme, 0)
			if got := pulse.Cell(x, y); got != expected {
				t.Fatalf("copy pulse=%+v, want %+v", got, expected)
			}
			if got := pulse.Cell(x+5, y); got != neighbor {
				t.Fatalf("unselected neighbor=%+v, want %+v", got, neighbor)
			}
			if got := app.DebugSnapshot().Focusables; !reflect.DeepEqual(got, focusBefore) {
				t.Fatalf("copy changed focus: before=%+v after=%+v", focusBefore, got)
			}
			if err := runner.HandleFrame(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if got := backend.painter.Cell(x, y); got != baseline {
				t.Fatalf("restored selection=%+v, want %+v", got, baseline)
			}
			// Selection survives the effect and remains copyable.
			runner.HandleEvent(vaxis.Key{Keycode: 'c', Modifiers: vaxis.ModSuper}, time.Now())
			if len(backend.copied) != 2 || backend.copied[1] != "copy" {
				t.Fatalf("selection after pulse copied=%v", backend.copied)
			}
		})
	}
}
