package tui

import (
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

// keyShortcuts maps live keyboard events to intents while allowing bracketed
// paste events to reach text editors unchanged.
type keyShortcuts struct {
	Bindings ui.ShortcutMap
	Child    ui.Widget
}

func (w keyShortcuts) WidgetChild() ui.Widget { return w.Child }

func (w keyShortcuts) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderKeyShortcuts{Bindings: w.Bindings}
}

func (w keyShortcuts) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	renderObject.(*renderKeyShortcuts).Bindings = w.Bindings
}

type renderKeyShortcuts struct {
	ui.SingleChildRenderObject
	Bindings ui.ShortcutMap
}

func (r *renderKeyShortcuts) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
		return
	}
	r.SetSize(constraints.Constrain(ui.Size{}))
}

func (r *renderKeyShortcuts) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderKeyShortcuts) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderKeyShortcuts) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderKeyShortcuts) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() != ui.CapturePhase {
		return ui.EventIgnored
	}
	key, ok := event.(ui.Key)
	if !ok || key.EventType == ui.EventRelease {
		return ui.EventIgnored
	}
	if key.EventType == vaxis.EventPaste {
		return ui.EventIgnored
	}
	for binding, intent := range r.Bindings {
		if key.MatchString(binding) && ctx.Invoke(intent) == ui.EventHandled {
			return ui.EventHandled
		}
	}
	return ui.EventIgnored
}
