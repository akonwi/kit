package tui

import (
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type selectionCopyDomainSurface struct {
	Theme  ui.Theme
	Copied *string
}

func (w selectionCopyDomainSurface) Build(ui.BuildContext) ui.Widget {
	copyAction := func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
		return ctx.Invoke(ui.CopySelectionTextIntent{OnCopied: func(text string) {
			*w.Copied = text
			ctx.Invoke(selectionCopyPulseIntent{Now: time.Now()})
		}})
	}
	return ui.Provider[ui.Theme]{Value: w.Theme, Child: ui.Actions{
		Bindings: map[ui.IntentType]ui.ActionFunc{copySelectionIntent{}.IntentType(): copyAction},
		Child: keyShortcuts{Bindings: ui.ShortcutMap{"Alt+y": copySelectionIntent{}}, Child: selectionFeedbackArea{Child: ui.Flex{
			Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStart,
			Children: []ui.Widget{
				ui.Text{Value: "outer copy"},
				ui.Text{Value: "ordinary", Style: ui.Style{Background: w.Theme.Selection}},
				selectionFeedbackArea{Child: ui.Text{Value: "inner copy"}},
			},
		}}},
	}}
}

func TestSelectionCopyPulseIsLimitedToNearestMarkedDomain(t *testing.T) {
	theme := ui.DefaultThemeSet().Dark
	copied := ""
	application := uitest.New(selectionCopyDomainSurface{Theme: theme, Copied: &copied})
	application.Pump(30, 4)
	rows := paintedRows(application, 30, 4)
	outerX, outerY := findTextCell(t, rows, "outer copy")
	ordinaryX, ordinaryY := findTextCell(t, rows, "ordinary")
	innerX, innerY := findTextCell(t, rows, "inner copy")

	selectTextRange(application, outerX, outerY, 5)
	selectTextRange(application, innerX, innerY, 5)
	application.Pump(30, 4)
	if got := application.Cell(outerX, outerY).Background; got != theme.Selection {
		t.Fatalf("retained outer selection = %v, want %v", got, theme.Selection)
	}
	if got := application.Cell(ordinaryX, ordinaryY).Background; got != theme.Selection {
		t.Fatalf("ordinary same-color background = %v, want %v", got, theme.Selection)
	}

	application.Send(vaxis.Key{Text: "y", Keycode: 'y', Modifiers: vaxis.ModAlt})
	application.Pump(30, 4)
	if copied != "inner" {
		t.Fatalf("copied = %q, want inner", copied)
	}
	pulse := selectionCopyPulseColor(theme, 0)
	if got := application.Cell(innerX, innerY).Background; got != pulse {
		t.Fatalf("inner pulse = %v, want %v", got, pulse)
	}
	if got := application.Cell(outerX, outerY).Background; got != theme.Selection {
		t.Fatalf("uncopied retained selection pulsed: %v", got)
	}
	if got := application.Cell(ordinaryX, ordinaryY).Background; got != theme.Selection {
		t.Fatalf("ordinary same-color background pulsed: %v", got)
	}
}

func selectTextRange(application *uitest.App, x, y, width int) {
	application.Send(vaxis.Mouse{Col: x, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	application.Send(vaxis.Mouse{Col: x + width, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion})
	application.Send(vaxis.Mouse{Col: x + width, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})
}
