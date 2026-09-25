package tui

import "go.rockorager.dev/vaxis/ui"

// composerOverlayPositioner keeps a transient surface close to the composer.
// When Anchor is set, the surface is aligned above that byte offset in the
// wrapped composer text; otherwise it is centered above the composer.
type composerOverlayPositioner struct {
	BottomInset    int
	PrimaryPercent int
	Composer       string
	Anchor         *int
	Child          ui.Widget
}

func (w composerOverlayPositioner) WidgetChild() ui.Widget { return w.Child }

func (w composerOverlayPositioner) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderComposerOverlayPositioner{
		bottomInset: w.BottomInset, primaryPercent: w.PrimaryPercent,
		composer: w.Composer, anchor: cloneInt(w.Anchor),
	}
}

func (w composerOverlayPositioner) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderComposerOverlayPositioner)
	if render.bottomInset != w.BottomInset || render.primaryPercent != w.PrimaryPercent || render.composer != w.Composer || !equalOptionalInt(render.anchor, w.Anchor) {
		render.bottomInset = w.BottomInset
		render.primaryPercent = w.PrimaryPercent
		render.composer = w.Composer
		render.anchor = cloneInt(w.Anchor)
		render.MarkNeedsLayout()
	}
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func equalOptionalInt(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

type renderComposerOverlayPositioner struct {
	ui.SingleChildRenderObject
	bottomInset    int
	primaryPercent int
	composer       string
	anchor         *int
	offset         ui.Offset
}

func (r *renderComposerOverlayPositioner) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
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
		x := max(0, (primaryWidth-childSize.Width)/2)
		y := max(0, size.Height-r.bottomInset-childSize.Height)
		if r.anchor != nil {
			innerWidth := max(1, primaryWidth-2)
			layout := ui.LayoutText(
				[]ui.TextSpan{{Text: r.composer}},
				ui.Constraints{MaxWidth: innerWidth},
				ui.TextLayoutOptions{SoftWrap: true},
			)
			position := ui.TextPosition{ByteOffset: min(max(0, *r.anchor), len(r.composer))}
			row, column, ok := layout.CellForPosition(position)
			if ok {
				composerHeight := min(composerMaxHeight, max(1, len(layout.Lines)))
				visibleRow := row - max(0, len(layout.Lines)-composerHeight)
				visibleRow = min(max(0, visibleRow), composerHeight-1)
				x = min(max(0, column+1), max(0, primaryWidth-childSize.Width))
				y = max(0, size.Height-(2+composerHeight-visibleRow)-childSize.Height)
			}
		}
		r.offset = ui.Offset{X: x, Y: y}
	}
	r.SetSize(size)
}

func (r *renderComposerOverlayPositioner) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	size := ui.Size{}
	if constraints.HasBoundedWidth() {
		size.Width = constraints.MaxWidth
	}
	if constraints.HasBoundedHeight() {
		size.Height = constraints.MaxHeight
	}
	return constraints.Constrain(size)
}

func (r *renderComposerOverlayPositioner) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset.Add(r.offset))
	}
}

func (r *renderComposerOverlayPositioner) ChildOffset(ui.RenderObject) ui.Offset  { return r.offset }
func (*renderComposerOverlayPositioner) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
