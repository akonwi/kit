package tui

import "go.rockorager.dev/vaxis/ui"

// pickerModalMinHeight keeps picker-style dialogs visually stable when their
// current result sets contain only a few rows.
const pickerModalMinHeight = 20

// pickerDialogContent owns the shared border and fixed footer structure for
// picker-style dialogs. Callers own the body above the divider.
func pickerDialogContent(theme ui.Theme, body, footer ui.Widget) ui.Widget {
	return ui.DecoratedBox(
		ui.Decoration{
			Style:  ui.Style{Foreground: theme.Foreground, Background: theme.Background},
			Border: ui.BorderAll(ui.Style{Foreground: theme.Border}),
		},
		ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			ui.Expanded(body),
			dialogDivider{Style: ui.Style{Foreground: theme.Border}},
			ui.Padding(ui.Insets{Right: 2, Bottom: 1, Left: 2}, footer),
		}},
	)
}

type pickerDialogLayoutState struct{ AvailableRows int }

// pickerDialogPositioner gives picker-style dialogs their shared width bounds,
// minimum height, and top-quarter placement. State is optional and reports the
// rows left after a caller's fixed body chrome.
type pickerDialogPositioner struct {
	Percent      int
	MinWidth     int
	MaxWidth     int
	Height       int
	ReservedRows int
	State        *pickerDialogLayoutState
	Child        ui.Widget
}

func (w pickerDialogPositioner) WidgetChild() ui.Widget { return w.Child }

func (w pickerDialogPositioner) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderPickerDialogPositioner{
		Percent: w.Percent, MinWidth: w.MinWidth, MaxWidth: w.MaxWidth,
		Height: w.Height, ReservedRows: w.ReservedRows, State: w.State,
	}
}

func (w pickerDialogPositioner) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderPickerDialogPositioner)
	if render.Percent == w.Percent && render.MinWidth == w.MinWidth && render.MaxWidth == w.MaxWidth &&
		render.Height == w.Height && render.ReservedRows == w.ReservedRows && render.State == w.State {
		return
	}
	render.Percent = w.Percent
	render.MinWidth = w.MinWidth
	render.MaxWidth = w.MaxWidth
	render.Height = w.Height
	render.ReservedRows = w.ReservedRows
	render.State = w.State
	render.MarkNeedsLayout()
}

type renderPickerDialogPositioner struct {
	ui.SingleChildRenderObject
	Percent      int
	MinWidth     int
	MaxWidth     int
	Height       int
	ReservedRows int
	State        *pickerDialogLayoutState
	offset       ui.Offset
}

func (r *renderPickerDialogPositioner) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	size := pickerDialogViewportSize(constraints)
	width := size.Width * r.Percent / 100
	width = max(r.MinWidth, min(r.MaxWidth, width))
	width = min(size.Width, width)
	height := min(size.Height, r.Height)
	if r.State != nil {
		r.State.AvailableRows = max(0, height-r.ReservedRows)
	}
	if child := r.Child(); child != nil {
		child.Layout(ctx, ui.Tight(ui.Size{Width: width, Height: height}))
		r.offset = ui.Offset{X: max(0, (size.Width-width)/2), Y: pickerTopOffset(size.Height, height)}
	}
	r.SetSize(size)
}

func (r *renderPickerDialogPositioner) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return pickerDialogViewportSize(constraints)
}

func pickerDialogViewportSize(constraints ui.Constraints) ui.Size {
	size := ui.Size{Width: constraints.MinWidth, Height: constraints.MinHeight}
	if constraints.HasBoundedWidth() {
		size.Width = constraints.MaxWidth
	}
	if constraints.HasBoundedHeight() {
		size.Height = constraints.MaxHeight
	}
	return constraints.Constrain(size)
}

func pickerTopOffset(height, childHeight int) int {
	return min(max(0, height/4), max(0, height-childHeight))
}

func (r *renderPickerDialogPositioner) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset.Add(r.offset))
	}
}

func (r *renderPickerDialogPositioner) ChildOffset(ui.RenderObject) ui.Offset {
	return r.offset
}

func (*renderPickerDialogPositioner) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

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
