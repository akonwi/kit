package tui

import "go.rockorager.dev/vaxis/ui"

// conversationLayout gives the transcript all height not needed by the
// bottom-anchored activity slot and growing composer.
type conversationLayout struct {
	Transcript ui.Widget
	Activity   ui.Widget
	Separator  ui.Widget
	Composer   ui.Widget
}

func (w conversationLayout) WidgetChildren() []ui.Widget {
	return []ui.Widget{w.Transcript, w.Activity, w.Separator, w.Composer}
}

func (conversationLayout) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderConversationLayout{}
}

func (conversationLayout) UpdateRenderObject(ui.BuildContext, ui.RenderObject) {}

type conversationParentData struct {
	Offset ui.Offset
}

func (data conversationParentData) RenderOffset() ui.Offset { return data.Offset }

type renderConversationLayout struct {
	ui.MultiChildRenderObject
}

func (r *renderConversationLayout) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	size, childSizes := r.layout(ctx, constraints, false)
	y := 0
	for index, child := range r.Children() {
		child.Base().SetParentData(conversationParentData{Offset: ui.Offset{Y: y}})
		y += childSizes[index].Height
	}
	r.SetSize(size)
}

func (r *renderConversationLayout) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	size, _ := r.layout(ctx, constraints, true)
	return size
}

func (r *renderConversationLayout) layout(ctx ui.LayoutContext, constraints ui.Constraints, dry bool) (ui.Size, []ui.Size) {
	children := r.Children()
	childSizes := make([]ui.Size, len(children))
	if len(children) != 4 {
		return constraints.Constrain(ui.Size{}), childSizes
	}

	width := constraints.MaxWidth
	if !constraints.HasBoundedWidth() {
		width = constraints.MinWidth
	}
	if !constraints.HasBoundedHeight() {
		return r.layoutUnbounded(ctx, constraints, width, dry)
	}

	height := constraints.MaxHeight
	composerLimit := min(1, height)
	separatorHeight := 0
	if height >= 2 {
		separatorHeight = 1
	}
	activityHeight := 0
	if height >= 4 {
		activityHeight = 1
	}
	transcriptReserve := 0
	if height-activityHeight-separatorHeight-composerLimit > 0 {
		transcriptReserve = 1
	}
	if composerLimit > 0 {
		composerLimit = min(composerMaxHeight, height-activityHeight-separatorHeight-transcriptReserve)
	}
	composerConstraints := ui.Constraints{MinWidth: width, MaxWidth: width, MaxHeight: composerLimit}
	childSizes[3] = layoutConversationChild(ctx, children[3], composerConstraints, dry)

	activityRow := ui.Constraints{MinWidth: width, MaxWidth: width, MinHeight: activityHeight, MaxHeight: activityHeight}
	childSizes[1] = layoutConversationChild(ctx, children[1], activityRow, dry)
	separatorRow := ui.Constraints{MinWidth: width, MaxWidth: width, MinHeight: separatorHeight, MaxHeight: separatorHeight}
	childSizes[2] = layoutConversationChild(ctx, children[2], separatorRow, dry)

	transcriptHeight := max(0, height-childSizes[1].Height-childSizes[2].Height-childSizes[3].Height)
	childSizes[0] = layoutConversationChild(ctx, children[0], ui.Tight(ui.Size{Width: width, Height: transcriptHeight}), dry)
	return constraints.Constrain(ui.Size{Width: width, Height: height}), childSizes
}

func (r *renderConversationLayout) layoutUnbounded(ctx ui.LayoutContext, constraints ui.Constraints, width int, dry bool) (ui.Size, []ui.Size) {
	children := r.Children()
	childSizes := make([]ui.Size, len(children))
	childSizes[0] = layoutConversationChild(ctx, children[0], ui.Constraints{MinWidth: width, MaxWidth: width, MaxHeight: ui.Unbounded}, dry)
	oneRow := ui.Constraints{MinWidth: width, MaxWidth: width, MinHeight: 1, MaxHeight: 1}
	childSizes[1] = layoutConversationChild(ctx, children[1], oneRow, dry)
	childSizes[2] = layoutConversationChild(ctx, children[2], oneRow, dry)
	childSizes[3] = layoutConversationChild(ctx, children[3], ui.Constraints{MinWidth: width, MaxWidth: width, MaxHeight: composerMaxHeight}, dry)
	height := 0
	for _, size := range childSizes {
		height += size.Height
	}
	return constraints.Constrain(ui.Size{Width: width, Height: height}), childSizes
}

func layoutConversationChild(ctx ui.LayoutContext, child ui.RenderObject, constraints ui.Constraints, dry bool) ui.Size {
	if dry {
		return ui.DryLayout(ctx, child, constraints)
	}
	child.Layout(ctx, constraints)
	return child.Base().Size()
}

func (r *renderConversationLayout) Paint(painter *ui.Painter, offset ui.Offset) {
	for _, child := range r.Children() {
		data, _ := child.Base().ParentData().(conversationParentData)
		child.Paint(painter, offset.Add(data.Offset))
	}
}

func (*renderConversationLayout) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderConversationLayout) SelectionChildOffset(child ui.RenderObject) ui.Offset {
	offset := r.ChildOffset(child)
	for _, candidate := range r.Children() {
		if candidate == child {
			break
		}
		offset.Y += conversationSelectionSize(candidate).Height - candidate.Base().Size().Height
	}
	return offset
}

func (r *renderConversationLayout) SelectionSize() ui.Size {
	size := r.Size()
	height := 0
	for _, child := range r.Children() {
		childSize := conversationSelectionSize(child)
		height += childSize.Height
		size.Width = max(size.Width, childSize.Width)
	}
	size.Height = max(size.Height, height)
	return size
}

func conversationSelectionSize(renderObject ui.RenderObject) ui.Size {
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
