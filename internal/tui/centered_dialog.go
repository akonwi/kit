package tui

import "go.rockorager.dev/vaxis/ui"

// centeredDialogPositioner bounds a non-picker dialog to the viewport and keeps
// it centered. Its child receives tight constraints so an expanded scroll body
// owns compression on short terminals.
type centeredDialogPositioner struct {
	Percent, MinWidth, MaxWidth, Height int
	Child                               ui.Widget
}

func (w centeredDialogPositioner) WidgetChild() ui.Widget { return w.Child }
func (w centeredDialogPositioner) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderCenteredDialogPositioner{Percent: w.Percent, MinWidth: w.MinWidth, MaxWidth: w.MaxWidth, Height: w.Height}
}
func (w centeredDialogPositioner) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderCenteredDialogPositioner)
	if render.Percent == w.Percent && render.MinWidth == w.MinWidth && render.MaxWidth == w.MaxWidth && render.Height == w.Height {
		return
	}
	render.Percent, render.MinWidth, render.MaxWidth, render.Height = w.Percent, w.MinWidth, w.MaxWidth, w.Height
	render.MarkNeedsLayout()
}

type renderCenteredDialogPositioner struct {
	ui.SingleChildRenderObject
	Percent, MinWidth, MaxWidth, Height int
	offset                              ui.Offset
}

func (r *renderCenteredDialogPositioner) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	size := pickerDialogViewportSize(constraints)
	width := min(size.Width, max(r.MinWidth, min(r.MaxWidth, size.Width*r.Percent/100)))
	height := min(size.Height, r.Height)
	if child := r.Child(); child != nil {
		child.Layout(ctx, ui.Tight(ui.Size{Width: width, Height: height}))
		r.offset = ui.Offset{X: max(0, (size.Width-width)/2), Y: max(0, (size.Height-height)/2)}
	}
	r.SetSize(size)
}
func (r *renderCenteredDialogPositioner) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return pickerDialogViewportSize(constraints)
}
func (r *renderCenteredDialogPositioner) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset.Add(r.offset))
	}
}
func (r *renderCenteredDialogPositioner) ChildOffset(ui.RenderObject) ui.Offset  { return r.offset }
func (*renderCenteredDialogPositioner) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
