package tui

import "go.rockorager.dev/vaxis/ui"

// mouseActivator adds primary-mouse activation without adding a focus-traversal stop.
type mouseActivator struct {
	Child                ui.Widget
	OnPressed            ui.VoidCallback
	OnHover              ui.VoidCallback
	OnHoverExit          ui.VoidCallback
	OnMotion             func(ui.EventContext, ui.Mouse)
	OnPrimaryDownCapture ui.VoidCallback
	DefaultMouseShape    bool
}

func (w mouseActivator) WidgetChild() ui.Widget { return w.Child }

func (w mouseActivator) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderMouseActivator{
		OnPressed: w.OnPressed, OnHover: w.OnHover, OnHoverExit: w.OnHoverExit,
		OnPrimaryDownCapture: w.OnPrimaryDownCapture, OnMotion: w.OnMotion,
		DefaultMouseShape: w.DefaultMouseShape,
	}
}

func (w mouseActivator) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	render := renderObject.(*renderMouseActivator)
	render.OnPressed = w.OnPressed
	render.OnHover = w.OnHover
	render.OnHoverExit = w.OnHoverExit
	render.OnMotion = w.OnMotion
	render.OnPrimaryDownCapture = w.OnPrimaryDownCapture
	render.DefaultMouseShape = w.DefaultMouseShape
}

type renderMouseActivator struct {
	ui.SingleChildRenderObject
	OnPressed            ui.VoidCallback
	OnHover              ui.VoidCallback
	OnHoverExit          ui.VoidCallback
	OnMotion             func(ui.EventContext, ui.Mouse)
	OnPrimaryDownCapture ui.VoidCallback
	DefaultMouseShape    bool
	hovered              bool
}

func (r *renderMouseActivator) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
		return
	}
	r.SetSize(constraints.Constrain(ui.Size{}))
}

func (r *renderMouseActivator) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderMouseActivator) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderMouseActivator) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderMouseActivator) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	mouse, ok := event.(ui.Mouse)
	if ctx.Phase() == ui.CapturePhase {
		if ok && mouse.EventType == ui.EventPress && mouse.Button == ui.MouseLeftButton && r.OnPrimaryDownCapture != nil {
			r.OnPrimaryDownCapture(ctx)
		}
		return ui.EventIgnored
	}
	if ctx.Phase() != ui.TargetPhase && ctx.Phase() != ui.BubblePhase {
		return ui.EventIgnored
	}
	if !ok {
		if r.hovered {
			r.hovered = false
			if r.OnHoverExit != nil {
				r.OnHoverExit(ctx)
			}
		}
		return ui.EventIgnored
	}
	if mouse.EventType == ui.EventMotion {
		if r.OnMotion != nil {
			r.OnMotion(ctx, mouse)
		}
		if !r.hovered {
			r.hovered = true
			if r.OnHover != nil {
				r.OnHover(ctx)
			}
		}
		return ui.EventIgnored
	}
	if mouse.EventType != ui.EventPress || mouse.Button != ui.MouseLeftButton || r.OnPressed == nil {
		return ui.EventIgnored
	}
	r.OnPressed(ctx)
	return ui.EventHandled
}

func (r *renderMouseActivator) MouseShape(ui.EventContext, ui.Mouse) ui.MouseShape {
	if r.OnPressed != nil && !r.DefaultMouseShape {
		return ui.MouseShapeClickable
	}
	return ui.MouseShapeDefault
}
