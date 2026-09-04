package tui

import "go.rockorager.dev/vaxis/ui"

// workspaceSelectionGate keeps retained hidden surfaces out of transcript
// selection while leaving both columns selectable in wide mode.
type workspaceSelectionGate struct {
	LayoutState *workspaceLayoutState
	Activity    bool
	Child       ui.Widget
}

func (w workspaceSelectionGate) WidgetChild() ui.Widget { return w.Child }

func (w workspaceSelectionGate) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderWorkspaceSelectionGate{LayoutState: w.LayoutState, Activity: w.Activity}
}

func (w workspaceSelectionGate) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	render := renderObject.(*renderWorkspaceSelectionGate)
	render.LayoutState = w.LayoutState
	render.Activity = w.Activity
}

type renderWorkspaceSelectionGate struct {
	ui.SingleChildRenderObject
	LayoutState *workspaceLayoutState
	Activity    bool
}

func (r *renderWorkspaceSelectionGate) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
		return
	}
	r.SetSize(constraints.Constrain(ui.Size{}))
}

func (r *renderWorkspaceSelectionGate) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderWorkspaceSelectionGate) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderWorkspaceSelectionGate) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderWorkspaceSelectionGate) SelectionSize() ui.Size {
	child := r.Child()
	if child == nil {
		return r.Size()
	}
	size := child.Base().Size()
	if provider, ok := child.(interface{ SelectionSize() ui.Size }); ok {
		size = provider.SelectionSize()
	}
	if provider, ok := child.(interface{ ScrollMetrics() ui.ScrollMetrics }); ok {
		metrics := provider.ScrollMetrics()
		size.Width = max(size.Width, metrics.ContentWidth)
		size.Height = max(size.Height, metrics.ContentHeight)
	}
	return size
}

func (r *renderWorkspaceSelectionGate) SelectionDisabled() bool {
	if r.LayoutState == nil {
		return false
	}
	if r.Activity {
		return !r.LayoutState.ActivityVisible
	}
	return !r.LayoutState.TranscriptVisible
}
