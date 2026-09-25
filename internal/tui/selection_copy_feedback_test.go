package tui

import (
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

func TestComposerSelectionCopyPulseRestartsWithoutChangingEditorSelection(t *testing.T) {
	theme := ui.DefaultThemeSet().Dark
	now := time.Now()
	clock := now
	backend := &selectionCopyBackend{toastAnimationBackend: toastAnimationBackend{size: ui.Size{Width: 80, Height: 24}}}
	observed := []string(nil)
	root := ui.Provider[selectionCopyNow]{Value: func() time.Time { return clock }, Child: markdownThemedTestSurface(theme,
		shellView{
			Snapshot:  shellSnapshot{Phase: phaseReady, Composer: "copy editor text", Scroll: &ui.ScrollController{}},
			Callbacks: shellCallbacks{CopySelection: func(text string) { observed = append(observed, text) }},
		},
	)}
	application := ui.NewApp(root)
	runner := ui.NewRunner(application, backend, nil)
	runner.Start(now)
	if err := runner.HandleFrame(now); err != nil {
		t.Fatal(err)
	}
	x, y := findTextCell(t, toastPainterRows(backend.painter), "copy editor text")
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
	if baseline.Background != theme.Selection {
		t.Fatalf("editor selection background = %v, want %v", baseline.Background, theme.Selection)
	}

	runner.HandleEvent(vaxis.Key{Keycode: 'c', Modifiers: vaxis.ModSuper}, now)
	immediate := paintSelectionCopyApp(application, backend.size)
	assertSelectionCopyCellOnlyBackgroundChanged(t, immediate.Cell(x, y), baseline, selectionCopyPulseColor(theme, 0))
	if len(observed) != 1 || observed[0] != "copy" || len(backend.copied) != 1 || backend.copied[0] != "copy" {
		t.Fatalf("first editor copy observed=%v clipboard=%v", observed, backend.copied)
	}

	if err := runner.HandleFrame(now.Add(200 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	clock = now.Add(200 * time.Millisecond)
	runner.HandleEvent(vaxis.Key{Keycode: 'c', Modifiers: vaxis.ModSuper}, clock)
	restarted := paintSelectionCopyApp(application, backend.size)
	assertSelectionCopyCellOnlyBackgroundChanged(t, restarted.Cell(x, y), baseline, selectionCopyPulseColor(theme, 0))

	// This frame is beyond the first pulse's deadline but still inside the
	// restarted pulse, proving copies reset one controller rather than stack.
	if err := runner.HandleFrame(now.Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if got := backend.painter.Cell(x, y); got.Background == theme.Selection {
		t.Fatalf("restarted pulse ended at the original deadline: %+v", got)
	}
	if len(observed) != 2 || len(backend.copied) != 2 {
		t.Fatalf("repeated editor copy observed=%v clipboard=%v", observed, backend.copied)
	}

	if err := runner.HandleFrame(now.Add(500 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if got := backend.painter.Cell(x, y); got != baseline {
		t.Fatalf("editor selection after pulse = %+v, want %+v", got, baseline)
	}
}

func paintSelectionCopyApp(application *ui.App, size ui.Size) *ui.Painter {
	application.Pump(size)
	painter := ui.NewPainter(size)
	application.Paint(painter)
	return painter
}

func assertSelectionCopyCellOnlyBackgroundChanged(t *testing.T, got, baseline ui.Cell, background ui.Color) {
	t.Helper()
	want := baseline
	want.Background = background
	if got != want {
		t.Fatalf("selection pulse cell = %+v, want %+v", got, want)
	}
}

func TestSelectionCopyPulseColorFadesToOriginalFill(t *testing.T) {
	for _, test := range []struct {
		name       string
		background ui.Color
		progress   float64
		want       ui.Color
	}{
		{"dark start", ui.RGB(0, 0, 0), 0, ui.RGB(78, 117, 156)},
		{"dark halfway", ui.RGB(0, 0, 0), 0.5, ui.RGB(89, 134, 178)},
		{"dark end", ui.RGB(0, 0, 0), 1, ui.RGB(100, 150, 200)},
		{"light start", ui.RGB(255, 255, 255), 0, ui.RGB(134, 173, 212)},
		{"light end", ui.RGB(255, 255, 255), 1, ui.RGB(100, 150, 200)},
	} {
		t.Run(test.name, func(t *testing.T) {
			theme := ui.DefaultTheme()
			theme.Selection = ui.RGB(100, 150, 200)
			theme.Background = test.background
			if got := selectionCopyPulseColor(theme, test.progress); got != test.want {
				t.Fatalf("pulse fill=%v, want %v", got, test.want)
			}
		})
	}
}
