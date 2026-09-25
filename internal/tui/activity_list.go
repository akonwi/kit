package tui

import "go.rockorager.dev/vaxis/ui"

type activityListController struct{ target *renderActivityList }

func (c *activityListController) attach(target *renderActivityList) {
	if c != nil {
		c.target = target
	}
}

func (c *activityListController) detach(target *renderActivityList) {
	if c != nil && c.target == target {
		c.target = nil
	}
}

func (c *activityListController) Attached() bool {
	return c != nil && c.target != nil
}

func (c *activityListController) Reveal(key activityToolKey) bool {
	if c == nil || c.target == nil {
		return false
	}
	return c.target.Reveal(key)
}

type activityList struct {
	Controller  *activityListController
	OuterScroll *ui.ScrollController
	ToolKeys    []activityToolKey
	Children    []ui.Widget
}

func (w activityList) WidgetChildren() []ui.Widget { return w.Children }

func (w activityList) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	render := &renderActivityList{OuterScroll: w.OuterScroll, ToolKeys: append([]activityToolKey(nil), w.ToolKeys...)}
	if w.Controller != nil {
		w.Controller.attach(render)
		render.Controller = w.Controller
	}
	return render
}

func (w activityList) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	render := renderObject.(*renderActivityList)
	if render.Controller != w.Controller {
		if render.Controller != nil {
			render.Controller.detach(render)
		}
		if w.Controller != nil {
			w.Controller.attach(render)
		}
		render.Controller = w.Controller
	}
	render.OuterScroll = w.OuterScroll
	render.ToolKeys = append(render.ToolKeys[:0], w.ToolKeys...)
	render.MarkNeedsLayout()
}

type activityListParentData struct{ Offset ui.Offset }

func (data activityListParentData) RenderOffset() ui.Offset { return data.Offset }

type renderActivityList struct {
	ui.MultiChildRenderObject
	Controller  *activityListController
	OuterScroll *ui.ScrollController
	ToolKeys    []activityToolKey
	offsets     []int
}

func (r *renderActivityList) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	width := constraints.MaxWidth
	if !constraints.HasBoundedWidth() {
		width = constraints.MinWidth
	}
	y := 0
	children := r.Children()
	r.offsets = make([]int, len(children))
	for index, child := range children {
		child.Layout(ctx, ui.Constraints{MinWidth: width, MaxWidth: width, MaxHeight: ui.Unbounded})
		r.offsets[index] = y
		child.Base().SetParentData(activityListParentData{Offset: ui.Offset{Y: y}})
		y += child.Base().Size().Height
	}
	r.SetSize(constraints.Constrain(ui.Size{Width: width, Height: y}))
}

func (r *renderActivityList) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	width := constraints.MaxWidth
	if !constraints.HasBoundedWidth() {
		width = constraints.MinWidth
	}
	height := 0
	for _, child := range r.Children() {
		height += ui.DryLayout(ctx, child, ui.Constraints{MinWidth: width, MaxWidth: width, MaxHeight: ui.Unbounded}).Height
	}
	return constraints.Constrain(ui.Size{Width: width, Height: height})
}

func (r *renderActivityList) Paint(painter *ui.Painter, offset ui.Offset) {
	for index, child := range r.Children() {
		if index >= len(r.offsets) {
			break
		}
		child.Paint(painter, offset.Add(ui.Offset{Y: r.offsets[index]}))
	}
}

func (*renderActivityList) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderActivityList) ChildOffset(child ui.RenderObject) ui.Offset {
	data, _ := child.Base().ParentData().(activityListParentData)
	return data.Offset
}

func (r *renderActivityList) SelectionChildOffset(child ui.RenderObject) ui.Offset {
	offset := r.ChildOffset(child)
	for _, candidate := range r.Children() {
		if candidate == child {
			break
		}
		offset.Y += activityListSelectionSize(candidate).Height - candidate.Base().Size().Height
	}
	return offset
}

func (r *renderActivityList) SelectionSize() ui.Size {
	size := r.Size()
	height := 0
	for _, child := range r.Children() {
		childSize := activityListSelectionSize(child)
		height += childSize.Height
		size.Width = max(size.Width, childSize.Width)
	}
	size.Height = max(size.Height, height)
	return size
}

func activityListSelectionSize(renderObject ui.RenderObject) ui.Size {
	size := renderObject.Base().Size()
	if provider, ok := renderObject.(interface{ SelectionSize() ui.Size }); ok {
		size = provider.SelectionSize()
	}
	if provider, ok := renderObject.(interface{ ScrollMetrics() ui.ScrollMetrics }); ok {
		metrics := provider.ScrollMetrics()
		size.Width = max(size.Width, metrics.ContentWidth)
		size.Height = max(size.Height, metrics.ContentHeight)
	}
	return size
}

func (r *renderActivityList) Reveal(key activityToolKey) bool {
	if r.OuterScroll == nil || !r.OuterScroll.Attached() {
		return false
	}
	for index, candidate := range r.ToolKeys {
		if candidate != key || index >= len(r.offsets) || index >= len(r.Children()) {
			continue
		}
		metrics := r.OuterScroll.Metrics()
		top := r.offsets[index] + 1
		itemHeight := r.Children()[index].Base().Size().Height
		bottom := top + itemHeight
		if itemHeight >= metrics.ViewportHeight {
			return r.OuterScroll.ScrollToOffset(top)
		}
		if top < metrics.ScrollOffset {
			return r.OuterScroll.ScrollToOffset(top)
		}
		viewportBottom := metrics.ScrollOffset + metrics.ViewportHeight
		if bottom > viewportBottom {
			return r.OuterScroll.ScrollToOffset(bottom - metrics.ViewportHeight)
		}
		return false
	}
	return false
}
