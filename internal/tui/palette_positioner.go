package tui

import "go.rockorager.dev/vaxis/ui"

const paletteTopDivisor = 4

// palettePositioner anchors a content-hugging palette at a stable top offset so
// filtering changes its bottom edge without moving the input row.
type palettePositioner struct{ Child ui.Widget }

func (w palettePositioner) WidgetChild() ui.Widget { return w.Child }

func (palettePositioner) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderPalettePositioner{}
}

func (palettePositioner) UpdateRenderObject(ui.BuildContext, ui.RenderObject) {}

type renderPalettePositioner struct {
	ui.SingleChildRenderObject
	offset ui.Offset
}

func (r *renderPalettePositioner) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	size := ui.Size{}
	if constraints.HasBoundedWidth() {
		size.Width = constraints.MaxWidth
	}
	if constraints.HasBoundedHeight() {
		size.Height = constraints.MaxHeight
	}
	size = constraints.Constrain(size)
	if child := r.Child(); child != nil {
		child.Layout(ctx, ui.Loose(size))
		childSize := child.Base().Size()
		r.offset = ui.Offset{
			X: max(0, (size.Width-childSize.Width)/2),
			Y: paletteTopOffset(size.Height, childSize.Height),
		}
	}
	r.SetSize(size)
}

func (r *renderPalettePositioner) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	size := ui.Size{}
	if constraints.HasBoundedWidth() {
		size.Width = constraints.MaxWidth
	}
	if constraints.HasBoundedHeight() {
		size.Height = constraints.MaxHeight
	}
	return constraints.Constrain(size)
}

func paletteTopOffset(height, childHeight int) int {
	return min(max(0, height/paletteTopDivisor), max(0, height-childHeight))
}

func (r *renderPalettePositioner) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset.Add(r.offset))
	}
}

func (r *renderPalettePositioner) ChildOffset(ui.RenderObject) ui.Offset  { return r.offset }
func (*renderPalettePositioner) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
