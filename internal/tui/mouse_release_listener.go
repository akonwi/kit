package tui

import "go.rockorager.dev/vaxis/ui"

type mouseReleaseListener struct {
	Child     ui.Widget
	OnPress   ui.VoidCallback
	OnRelease ui.VoidCallback
	Capture   bool
}

func (w mouseReleaseListener) WidgetChild() ui.Widget { return w.Child }

func (w mouseReleaseListener) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderMouseReleaseListener{OnPress: w.OnPress, OnRelease: w.OnRelease, Capture: w.Capture}
}

func (w mouseReleaseListener) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderMouseReleaseListener)
	render.OnPress = w.OnPress
	render.OnRelease = w.OnRelease
	render.Capture = w.Capture
}

type renderMouseReleaseListener struct {
	ui.SingleChildRenderObject
	OnPress   ui.VoidCallback
	OnRelease ui.VoidCallback
	Capture   bool
}

func (r *renderMouseReleaseListener) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
		return
	}
	r.SetSize(constraints.Constrain(ui.Size{}))
}

func (r *renderMouseReleaseListener) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderMouseReleaseListener) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderMouseReleaseListener) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderMouseReleaseListener) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	mouse, ok := event.(ui.Mouse)
	phase := ui.BubblePhase
	if r.Capture {
		phase = ui.CapturePhase
	}
	if ok && ctx.Phase() == phase && mouse.Button == ui.MouseLeftButton {
		if mouse.EventType == ui.EventPress && r.OnPress != nil {
			r.OnPress(ctx)
		}
		if mouse.EventType == ui.EventRelease && r.OnRelease != nil {
			r.OnRelease(ctx)
		}
	}
	return ui.EventIgnored
}
