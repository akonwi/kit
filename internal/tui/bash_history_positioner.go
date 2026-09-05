package tui

import "go.rockorager.dev/vaxis/ui"

// bashHistoryPositioner keeps the history surface close to the bottom composer
// while preserving a small inset for the composer and global footer.
type bashHistoryPositioner struct {
	BottomInset    int
	PrimaryPercent int
	Child          ui.Widget
}

func (w bashHistoryPositioner) WidgetChild() ui.Widget { return w.Child }

func (w bashHistoryPositioner) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderBashHistoryPositioner{bottomInset: w.BottomInset, primaryPercent: w.PrimaryPercent}
}

func (w bashHistoryPositioner) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderBashHistoryPositioner)
	if render.bottomInset != w.BottomInset || render.primaryPercent != w.PrimaryPercent {
		render.bottomInset = w.BottomInset
		render.primaryPercent = w.PrimaryPercent
		render.MarkNeedsLayout()
	}
}

type renderBashHistoryPositioner struct {
	ui.SingleChildRenderObject
	bottomInset    int
	primaryPercent int
	offset         ui.Offset
}

func (r *renderBashHistoryPositioner) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
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
		primaryWidth := size.Width
		if r.primaryPercent > 0 && r.primaryPercent < 100 {
			primaryWidth = max(1, size.Width*r.primaryPercent/100)
		}
		r.offset = ui.Offset{
			X: max(0, (primaryWidth-childSize.Width)/2),
			Y: max(0, size.Height-r.bottomInset-childSize.Height),
		}
	}
	r.SetSize(size)
}

func (r *renderBashHistoryPositioner) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	size := ui.Size{}
	if constraints.HasBoundedWidth() {
		size.Width = constraints.MaxWidth
	}
	if constraints.HasBoundedHeight() {
		size.Height = constraints.MaxHeight
	}
	return constraints.Constrain(size)
}

func (r *renderBashHistoryPositioner) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset.Add(r.offset))
	}
}

func (r *renderBashHistoryPositioner) ChildOffset(ui.RenderObject) ui.Offset  { return r.offset }
func (*renderBashHistoryPositioner) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
