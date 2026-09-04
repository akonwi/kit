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

// dialogDivider separates a footer while joining the dialog's outer border.
type dialogDivider struct{ Style ui.Style }

func (w dialogDivider) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderDialogDivider{Style: w.Style}
}

func (w dialogDivider) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderDialogDivider)
	if render.Style != w.Style {
		render.Style = w.Style
		render.MarkNeedsPaint()
	}
}

type renderDialogDivider struct {
	ui.LeafRenderObject
	Style ui.Style
}

func (r *renderDialogDivider) Layout(_ ui.LayoutContext, constraints ui.Constraints) {
	r.SetSize(r.dividerSize(constraints))
}

func (r *renderDialogDivider) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return r.dividerSize(constraints)
}

func (r *renderDialogDivider) dividerSize(constraints ui.Constraints) ui.Size {
	width := constraints.MinWidth
	if constraints.HasBoundedWidth() {
		width = constraints.MaxWidth
	}
	return constraints.Constrain(ui.Size{Width: width, Height: 1})
}

func (r *renderDialogDivider) Paint(painter *ui.Painter, offset ui.Offset) {
	width := r.Size().Width
	for column := 0; column < width; column++ {
		grapheme := "─"
		if column == 0 {
			grapheme = "├"
		} else if column == width-1 {
			grapheme = "┤"
		}
		painter.DrawCell(ui.Point{X: offset.X + column, Y: offset.Y}, ui.Cell{
			Character: ui.Character{Grapheme: grapheme, Width: 1}, Style: r.Style,
		})
	}
}
