package tui

import "go.rockorager.dev/vaxis/ui"

const (
	// transcriptMinMargin is the horizontal breathing room kept on each side
	// of the transcript.
	transcriptMinMargin = 2
)

// readableColumn keeps at least MinMargin cells of breathing room on either
// side of its child. The child always receives the full inner width so fills
// and separators span the transcript.
type readableColumn struct {
	MinMargin int
	Child     ui.Widget
}

// transcriptColumn wraps child in Kit's transcript margins.
func transcriptColumn(child ui.Widget) ui.Widget {
	return readableColumn{MinMargin: transcriptMinMargin, Child: child}
}

func (w readableColumn) WidgetChild() ui.Widget { return w.Child }

func (w readableColumn) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderReadableColumn{MinMargin: w.MinMargin}
}

func (w readableColumn) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	render := renderObject.(*renderReadableColumn)
	if render.MinMargin != w.MinMargin {
		render.MinMargin = w.MinMargin
		render.MarkNeedsLayout()
	}
}

type renderReadableColumn struct {
	ui.SingleChildRenderObject
	MinMargin int
}

func (r *renderReadableColumn) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	r.SetSize(r.layout(ctx, constraints, false))
}

func (r *renderReadableColumn) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return r.layout(ctx, constraints, true)
}

func (r *renderReadableColumn) layout(ctx ui.LayoutContext, constraints ui.Constraints, dry bool) ui.Size {
	width := constraints.MaxWidth
	if !constraints.HasBoundedWidth() {
		width = constraints.MinWidth
	}
	inner := readableColumnWidth(width, r.MinMargin)
	child := r.Child()
	if child == nil {
		return constraints.Constrain(ui.Size{Width: width})
	}
	childConstraints := ui.Constraints{MinWidth: inner, MaxWidth: inner, MinHeight: constraints.MinHeight, MaxHeight: constraints.MaxHeight}
	var size ui.Size
	if dry {
		size = ui.DryLayout(ctx, child, childConstraints)
	} else {
		child.Layout(ctx, childConstraints)
		size = child.Base().Size()
	}
	return constraints.Constrain(ui.Size{Width: width, Height: size.Height})
}

// readableColumnWidth returns the inner width of a column placed at the
// left margin within width.
func readableColumnWidth(width, minMargin int) int {
	inner := max(0, width-2*minMargin)
	if inner == 0 {
		return max(0, width)
	}
	return inner
}

// transcriptLatestMessageOutOfView reports whether the transcript's latest
// message is entirely outside the viewport, matching the macOS transcript's
// resume affordance. A tall final message that starts above the viewport stays
// in view until the viewport scrolls past its end. itemOffset is the latest
// item's row offset within the scroll content; itemCount is the list length.
func transcriptLatestMessageOutOfView(metrics ui.ScrollMetrics, itemOffset, itemCount int, measured bool) bool {
	if !measured || itemCount == 0 || metrics.ViewportHeight <= 0 {
		return false
	}
	// The latest item is the last content, so it extends to the content end.
	// The one-row gap before it belongs to the preceding item.
	return metrics.ContentHeight <= metrics.ScrollOffset || itemOffset >= metrics.ScrollOffset+metrics.ViewportHeight
}

func (r *renderReadableColumn) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset.Add(ui.Offset{X: r.MinMargin}))
	}
}

func (r *renderReadableColumn) ChildOffset(ui.RenderObject) ui.Offset {
	return ui.Offset{X: r.MinMargin}
}

func (*renderReadableColumn) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

// transcriptLatestItemOffset estimates the latest item's start when the list
// has not measured rows beyond the viewport. It subtracts the measured prefix
// from the content height.
func transcriptLatestItemOffset(metrics ui.ScrollMetrics, offsetForIndex func(int) (int, bool), count int) (int, bool) {
	measured := 0
	for index := 0; index < count-1; index++ {
		offset, ok := offsetForIndex(index + 1)
		if !ok || offset == 0 {
			break
		}
		measured = offset
	}
	if measured == 0 {
		return 0, false
	}
	return measured, true
}
