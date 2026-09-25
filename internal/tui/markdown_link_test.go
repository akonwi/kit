package tui

import (
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const markdownLinkTestURL = "https://example.test/docs"

func TestMarkdownLinkActivatorRequiresUnmodifiedStationaryPrimaryGesture(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		events []ui.Event
		want   int
	}{
		{
			name: "click",
			events: []ui.Event{
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0),
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, 0),
			},
			want: 1,
		},
		{
			name: "movement away and back",
			events: []ui.Event{
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0),
				markdownLinkMouse(1, 0, ui.MouseLeftButton, ui.EventMotion, 0),
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventMotion, 0),
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, 0),
			},
		},
		{
			name: "release outside",
			events: []ui.Event{
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0),
				markdownLinkMouse(9, 0, ui.MouseLeftButton, ui.EventRelease, 0),
			},
		},
		{
			name: "secondary press",
			events: []ui.Event{
				markdownLinkMouse(0, 0, ui.MouseRightButton, ui.EventPress, 0),
				markdownLinkMouse(0, 0, ui.MouseRightButton, ui.EventRelease, 0),
			},
		},
		{
			name: "modified press",
			events: []ui.Event{
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, vaxis.ModShift),
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, vaxis.ModShift),
			},
		},
		{
			name: "modified release",
			events: []ui.Event{
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0),
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, vaxis.ModCtrl),
			},
		},
		{
			name: "scroll during press",
			events: []ui.Event{
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0),
				markdownLinkMouse(0, 0, ui.MouseWheelDown, ui.EventPress, 0),
				markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, 0),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opened := []string(nil)
			app := newMarkdownLinkTestApp(markdownLinkTestURL, func(_ ui.EventContext, target string) {
				opened = append(opened, target)
			}, nil)
			pumpMarkdownLinkTestApp(app, 12, 2)
			for _, event := range test.events {
				app.Send(event)
			}
			if len(opened) != test.want {
				t.Fatalf("opened = %#v, want %d activation(s)", opened, test.want)
			}
			if test.want == 1 && opened[0] != markdownLinkTestURL {
				t.Fatalf("opened target = %q", opened[0])
			}
		})
	}
}

func TestMarkdownLinkActivatorSurvivesSelectionRepaintBetweenPressAndRelease(t *testing.T) {
	t.Parallel()

	opened := ""
	app := newMarkdownLinkTestApp(markdownLinkTestURL, func(_ ui.EventContext, target string) { opened = target }, nil)
	pumpMarkdownLinkTestApp(app, 12, 2)
	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0))
	pumpMarkdownLinkTestApp(app, 12, 2)
	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, 0))
	if opened != markdownLinkTestURL {
		t.Fatalf("activation after selection repaint = %q", opened)
	}
}

func TestMarkdownLinkActivatorUsesPaintedSafeHyperlinkCells(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"javascript:alert(1)", "file:///tmp/private", "https://example.test/\nunsafe"} {
		opened := false
		app := newMarkdownLinkTestApp(target, func(ui.EventContext, string) { opened = true }, nil)
		pumpMarkdownLinkTestApp(app, 20, 2)
		app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0))
		app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, 0))
		if opened {
			t.Errorf("unsafe painted target %q activated", target)
		}
	}
}

func TestMarkdownLinkActivatorUsesLocalCoordinatesWhenNested(t *testing.T) {
	t.Parallel()

	opened := ""
	app := ui.NewApp(ui.Padding(ui.Insets{Left: 3, Top: 1}, markdownLinkActivator{
		OpenURL: func(_ ui.EventContext, target string) { opened = target },
		Child: ui.SelectionArea{Child: ui.Text{
			Value: "docs", Style: ui.Style{Hyperlink: markdownLinkTestURL},
		}},
	}))
	pumpMarkdownLinkTestApp(app, 12, 3)
	app.Send(markdownLinkMouse(3, 1, ui.MouseLeftButton, ui.EventPress, 0))
	app.Send(markdownLinkMouse(3, 1, ui.MouseLeftButton, ui.EventRelease, 0))
	if opened != markdownLinkTestURL {
		t.Fatalf("nested activation = %q", opened)
	}
}

func TestMarkdownLinkActivatorCoversWrappedAndWideHyperlinkCells(t *testing.T) {
	t.Parallel()

	opened := []string(nil)
	child := ui.SelectionArea{Child: ui.RichText{SoftWrap: true, Spans: []ui.TextSpan{{
		Text: "界abcdef", Style: ui.Style{Hyperlink: markdownLinkTestURL},
	}}}}
	app := ui.NewApp(markdownLinkActivator{
		Child: child, OpenURL: func(_ ui.EventContext, target string) { opened = append(opened, target) },
	})
	pumpMarkdownLinkTestApp(app, 4, 3)

	// Column 1 is the continuation cell of the two-column first grapheme.
	for _, point := range []ui.Point{{X: 1, Y: 0}, {X: 1, Y: 1}} {
		app.Send(markdownLinkMouse(point.X, point.Y, ui.MouseLeftButton, ui.EventPress, 0))
		app.Send(markdownLinkMouse(point.X, point.Y, ui.MouseLeftButton, ui.EventRelease, 0))
	}
	if len(opened) != 2 || opened[0] != markdownLinkTestURL || opened[1] != markdownLinkTestURL {
		t.Fatalf("wrapped Unicode activations = %#v", opened)
	}
}

func TestMarkdownLinkActivatorRejectsDisabledResizedAndStaleGestures(t *testing.T) {
	t.Parallel()

	enabled := true
	opened := []string(nil)
	root := func(key, target string) ui.Widget {
		return markdownLinkActivator{
			GestureKey: key,
			Enabled:    func() bool { return enabled },
			OpenURL:    func(_ ui.EventContext, target string) { opened = append(opened, target) },
			Child:      ui.SelectionArea{Child: ui.Text{Value: "docs", Style: ui.Style{Hyperlink: target}}},
		}
	}
	app := ui.NewApp(root("session-a", markdownLinkTestURL))
	pumpMarkdownLinkTestApp(app, 12, 2)

	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0))
	enabled = false
	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, 0))

	enabled = true
	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0))
	pumpMarkdownLinkTestApp(app, 10, 2)
	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, 0))

	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0))
	app.UpdateRoot(root("session-a", "https://example.test/changed"))
	pumpMarkdownLinkTestApp(app, 10, 2)
	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, 0))

	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0))
	app.UpdateRoot(root("session-b", markdownLinkTestURL))
	pumpMarkdownLinkTestApp(app, 10, 2)
	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, 0))

	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventPress, 0))
	app.UpdateRoot(ui.Text{Value: "unmounted"})
	pumpMarkdownLinkTestApp(app, 10, 2)
	app.Send(markdownLinkMouse(0, 0, ui.MouseLeftButton, ui.EventRelease, 0))

	if len(opened) != 0 {
		t.Fatalf("stale gestures opened = %#v", opened)
	}
}

func TestMarkdownLinkActivatorAddsNoFocusTargets(t *testing.T) {
	t.Parallel()

	plain := ui.NewApp(ui.SelectionArea{Child: ui.Text{Value: "docs", Style: ui.Style{Hyperlink: markdownLinkTestURL}}})
	wrapped := newMarkdownLinkTestApp(markdownLinkTestURL, func(ui.EventContext, string) {}, nil)
	pumpMarkdownLinkTestApp(plain, 12, 2)
	pumpMarkdownLinkTestApp(wrapped, 12, 2)
	if got, want := len(wrapped.DebugSnapshot().Focusables), len(plain.DebugSnapshot().Focusables); got != want {
		t.Fatalf("focusable count = %d, want unchanged %d", got, want)
	}
}

func newMarkdownLinkTestApp(target string, open ui.TextChangedCallback, enabled func() bool) *ui.App {
	return ui.NewApp(markdownLinkActivator{
		Enabled: enabled,
		OpenURL: open,
		Child:   ui.SelectionArea{Child: ui.Text{Value: "docs", Style: ui.Style{Hyperlink: target}}},
	})
}

func pumpMarkdownLinkTestApp(app *ui.App, width, height int) {
	size := ui.Size{Width: width, Height: height}
	app.Pump(size)
	app.Paint(ui.NewPainter(size))
}

func markdownLinkMouse(column, row int, button ui.MouseButton, eventType vaxis.EventType, modifiers vaxis.ModifierMask) ui.Mouse {
	return ui.Mouse{Col: column, Row: row, Button: button, EventType: eventType, Modifiers: modifiers}
}
