package tui

import "go.rockorager.dev/vaxis/ui"

// proportionalWidth gives a child a viewport-relative width bounded by named
// minimum and maximum dimensions.
type proportionalWidth struct {
	Percent int
	Min     int
	Max     int
	Child   ui.Widget
}

func (w proportionalWidth) WidgetChild() ui.Widget { return w.Child }

func (w proportionalWidth) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderProportionalWidth{Percent: w.Percent, Min: w.Min, Max: w.Max}
}

func (w proportionalWidth) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderProportionalWidth)
	if render.Percent == w.Percent && render.Min == w.Min && render.Max == w.Max {
		return
	}
	render.Percent = w.Percent
	render.Min = w.Min
	render.Max = w.Max
	render.MarkNeedsLayout()
}

type renderProportionalWidth struct {
	ui.SingleChildRenderObject
	Percent int
	Min     int
	Max     int
}

func (r *renderProportionalWidth) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	r.SetSize(r.layout(ctx, constraints, false))
}

func (r *renderProportionalWidth) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return r.layout(ctx, constraints, true)
}

func (r *renderProportionalWidth) layout(ctx ui.LayoutContext, constraints ui.Constraints, dry bool) ui.Size {
	width := r.Max
	if constraints.HasBoundedWidth() {
		percent := r.Percent
		if percent <= 0 {
			percent = 100
		}
		width = constraints.MaxWidth * percent / 100
	}
	if r.Min > 0 && width < r.Min {
		width = r.Min
	}
	if r.Max > 0 && width > r.Max {
		width = r.Max
	}
	width = constraints.Constrain(ui.Size{Width: width}).Width
	childConstraints := constraints
	childConstraints.MinWidth = width
	childConstraints.MaxWidth = width
	child := r.Child()
	if child == nil {
		return constraints.Constrain(ui.Size{Width: width})
	}
	if dry {
		return constraints.Constrain(ui.DryLayout(ctx, child, childConstraints))
	}
	child.Layout(ctx, childConstraints)
	return constraints.Constrain(child.Base().Size())
}

func (r *renderProportionalWidth) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (r *renderProportionalWidth) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
