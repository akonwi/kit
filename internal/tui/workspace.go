package tui

import "go.rockorager.dev/vaxis/ui"

// workspaceLayoutState projects which retained content surface is visible.
type workspaceLayoutState struct {
	TranscriptVisible bool
	ActivityVisible   bool
}

const (
	workspaceTabsChild = iota
	workspaceTranscriptChild
	workspacePendingChild
	workspaceComposerSeparatorChild
	workspaceComposerChild
	workspaceActivityChild
	workspaceChildCount
)

// conversationWorkspaceHost composes one selected full-width content surface
// above the shell-owned pending area and composer. Tabs are omitted while Agent
// is the only surface.
type conversationWorkspaceHost struct {
	Open                bool
	ActivitySelected    bool
	Tabs                ui.Widget
	Transcript          ui.Widget
	Pending             ui.Widget
	PendingHeight       int
	ComposerSeparator   ui.Widget
	Composer            ui.Widget
	ComposerHeightLimit int
	Activity            ui.Widget
	LayoutState         *workspaceLayoutState
}

func (w conversationWorkspaceHost) WidgetChildren() []ui.Widget {
	return []ui.Widget{
		w.Tabs,
		w.Transcript,
		w.Pending,
		w.ComposerSeparator,
		w.Composer,
		w.Activity,
	}
}

func (w conversationWorkspaceHost) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderConversationWorkspaceHost{
		Open: w.Open, ActivitySelected: w.ActivitySelected, PendingHeight: w.PendingHeight,
		ComposerHeightLimit: w.ComposerHeightLimit, LayoutState: w.LayoutState,
	}
}

func (w conversationWorkspaceHost) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	render := renderObject.(*renderConversationWorkspaceHost)
	if render.Open != w.Open || render.ActivitySelected != w.ActivitySelected || render.PendingHeight != w.PendingHeight || render.ComposerHeightLimit != w.ComposerHeightLimit {
		render.Open = w.Open
		render.ActivitySelected = w.ActivitySelected
		render.PendingHeight = w.PendingHeight
		render.ComposerHeightLimit = w.ComposerHeightLimit
		render.MarkNeedsLayout()
	}
	render.LayoutState = w.LayoutState
}

type workspaceParentData struct{ Offset ui.Offset }

func (data workspaceParentData) RenderOffset() ui.Offset { return data.Offset }

type renderConversationWorkspaceHost struct {
	ui.MultiChildRenderObject
	Open                bool
	ActivitySelected    bool
	PendingHeight       int
	ComposerHeightLimit int
	LayoutState         *workspaceLayoutState
}

func (r *renderConversationWorkspaceHost) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	size, childLayouts := r.layout(ctx, constraints, false)
	for index, child := range r.Children() {
		child.Base().SetParentData(workspaceParentData{Offset: childLayouts[index].offset})
	}
	r.SetSize(size)
}

func (r *renderConversationWorkspaceHost) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	size, _ := r.layout(ctx, constraints, true)
	return size
}

type workspaceChildLayout struct {
	size   ui.Size
	offset ui.Offset
}

func (r *renderConversationWorkspaceHost) layout(ctx ui.LayoutContext, constraints ui.Constraints, dry bool) (ui.Size, []workspaceChildLayout) {
	children := r.Children()
	layouts := make([]workspaceChildLayout, len(children))
	width := constraints.MaxWidth
	if !constraints.HasBoundedWidth() {
		width = constraints.MinWidth
	}
	height := constraints.MaxHeight
	if !constraints.HasBoundedHeight() {
		height = constraints.MinHeight
	}
	size := constraints.Constrain(ui.Size{Width: width, Height: height})
	if len(children) != workspaceChildCount {
		return size, layouts
	}

	activeChild := workspaceTranscriptChild
	if r.Open && r.ActivitySelected {
		activeChild = workspaceActivityChild
	}
	if !dry && r.LayoutState != nil {
		r.LayoutState.TranscriptVisible = activeChild == workspaceTranscriptChild
		r.LayoutState.ActivityVisible = activeChild == workspaceActivityChild
	}

	layoutChild := func(index int, childConstraints ui.Constraints, offset ui.Offset) ui.Size {
		var childSize ui.Size
		if dry {
			childSize = ui.DryLayout(ctx, children[index], childConstraints)
		} else {
			children[index].Layout(ctx, childConstraints)
			childSize = children[index].Base().Size()
		}
		layouts[index] = workspaceChildLayout{size: childSize, offset: offset}
		return childSize
	}
	hide := func(index int) { layoutChild(index, ui.Tight(ui.Size{}), ui.Offset{}) }

	tabHeight := 0
	if r.Open {
		tabHeight = min(2, height)
		layoutChild(workspaceTabsChild, ui.Tight(ui.Size{Width: width, Height: tabHeight}), ui.Offset{})
	} else {
		hide(workspaceTabsChild)
	}
	if activeChild == workspaceTranscriptChild {
		hide(workspaceActivityChild)
	} else {
		hide(workspaceTranscriptChild)
	}

	if !constraints.HasBoundedHeight() {
		return r.layoutUnbounded(ctx, constraints, layouts, dry, layoutChild, width, tabHeight, activeChild)
	}
	r.layoutConversationColumn(ctx, children, layouts, dry, layoutChild, width, max(0, height-tabHeight), tabHeight, activeChild)
	return size, layouts
}

func (r *renderConversationWorkspaceHost) layoutUnbounded(
	ctx ui.LayoutContext,
	constraints ui.Constraints,
	layouts []workspaceChildLayout,
	dry bool,
	layoutChild func(int, ui.Constraints, ui.Offset) ui.Size,
	width int,
	yOffset int,
	activeChild int,
) (ui.Size, []workspaceChildLayout) {
	composerLimit := r.ComposerHeightLimit
	if composerLimit <= 0 {
		composerLimit = composerMaxHeight
	}
	composerSize := layoutChild(
		workspaceComposerChild,
		ui.Constraints{MinWidth: width, MaxWidth: width, MaxHeight: composerLimit},
		ui.Offset{},
	)
	mainSize := layoutChild(
		activeChild,
		ui.Constraints{MinWidth: width, MaxWidth: width, MaxHeight: ui.Unbounded},
		ui.Offset{Y: yOffset},
	)
	pendingY := yOffset + mainSize.Height
	pendingHeight := max(0, r.PendingHeight)
	layoutChild(workspacePendingChild, ui.Tight(ui.Size{Width: width, Height: pendingHeight}), ui.Offset{Y: pendingY})
	separatorY := pendingY + pendingHeight
	layoutChild(workspaceComposerSeparatorChild, ui.Tight(ui.Size{Width: width, Height: 1}), ui.Offset{Y: separatorY})
	layouts[workspaceComposerChild].offset = ui.Offset{Y: separatorY + 1}
	height := separatorY + 1 + composerSize.Height
	finalSize := constraints.Constrain(ui.Size{Width: width, Height: height})
	if finalSize.Height > height {
		bounded := constraints
		bounded.MinHeight = finalSize.Height
		bounded.MaxHeight = finalSize.Height
		return r.layout(ctx, bounded, dry)
	}
	return finalSize, layouts
}

func (r *renderConversationWorkspaceHost) layoutConversationColumn(
	ctx ui.LayoutContext,
	children []ui.RenderObject,
	layouts []workspaceChildLayout,
	dry bool,
	layoutChild func(int, ui.Constraints, ui.Offset) ui.Size,
	width int,
	height int,
	yOffset int,
	mainChild int,
) {
	composerLimit := min(1, height)
	separatorHeight := 0
	if height >= 2 {
		separatorHeight = 1
	}
	pendingHeight := 0
	if height >= 4 {
		pendingHeight = min(max(0, r.PendingHeight), max(1, height-3))
	}
	mainReserve := 0
	if height-pendingHeight-separatorHeight-composerLimit > 0 {
		mainReserve = 1
	}
	if composerLimit > 0 {
		configuredLimit := r.ComposerHeightLimit
		if configuredLimit <= 0 {
			configuredLimit = composerMaxHeight
		}
		composerLimit = min(configuredLimit, height-pendingHeight-separatorHeight-mainReserve)
	}
	composerSize := layoutChild(
		workspaceComposerChild,
		ui.Constraints{MinWidth: width, MaxWidth: width, MaxHeight: composerLimit},
		ui.Offset{},
	)
	mainHeight := max(0, height-pendingHeight-separatorHeight-composerSize.Height)
	layoutChild(mainChild, ui.Tight(ui.Size{Width: width, Height: mainHeight}), ui.Offset{Y: yOffset})
	layoutChild(
		workspacePendingChild,
		ui.Tight(ui.Size{Width: width, Height: pendingHeight}),
		ui.Offset{Y: yOffset + mainHeight},
	)
	separatorY := yOffset + mainHeight + pendingHeight
	layoutChild(
		workspaceComposerSeparatorChild,
		ui.Tight(ui.Size{Width: width, Height: separatorHeight}),
		ui.Offset{Y: separatorY},
	)
	layouts[workspaceComposerChild].offset = ui.Offset{Y: separatorY + separatorHeight}
}

func (r *renderConversationWorkspaceHost) visibleChildren() []int {
	activeChild := workspaceTranscriptChild
	if r.Open && r.ActivitySelected {
		activeChild = workspaceActivityChild
	}
	order := make([]int, 0, 5)
	if r.Open {
		order = append(order, workspaceTabsChild)
	}
	return append(order, activeChild, workspacePendingChild, workspaceComposerSeparatorChild, workspaceComposerChild)
}

func (r *renderConversationWorkspaceHost) VisitChildren(visit func(ui.RenderObject)) {
	children := r.Children()
	if len(children) != workspaceChildCount {
		return
	}
	for _, index := range r.visibleChildren() {
		visit(children[index])
	}
}

func (r *renderConversationWorkspaceHost) Paint(painter *ui.Painter, offset ui.Offset) {
	for _, index := range r.visibleChildren() {
		child := r.Children()[index]
		size := child.Base().Size()
		if size.Width == 0 || size.Height == 0 {
			continue
		}
		data, _ := child.Base().ParentData().(workspaceParentData)
		childOffset := offset.Add(data.Offset)
		painter.PushClip(ui.Rect{X: childOffset.X, Y: childOffset.Y, Width: size.Width, Height: size.Height})
		child.Paint(painter, childOffset)
		painter.PopClip()
	}
}

func (*renderConversationWorkspaceHost) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderConversationWorkspaceHost) ChildOffset(child ui.RenderObject) ui.Offset {
	data, _ := child.Base().ParentData().(workspaceParentData)
	return data.Offset
}

func (r *renderConversationWorkspaceHost) SelectionChildOffset(child ui.RenderObject) ui.Offset {
	offset := r.ChildOffset(child)
	children := r.Children()
	target := -1
	for index, candidate := range children {
		if candidate == child {
			target = index
			break
		}
	}
	for _, index := range r.visibleChildren() {
		if index == target {
			break
		}
		candidate := children[index]
		offset.Y += workspaceSelectionSize(candidate).Height - candidate.Base().Size().Height
	}
	return offset
}

func (r *renderConversationWorkspaceHost) SelectionSize() ui.Size {
	size := r.Size()
	children := r.Children()
	height := 0
	for _, index := range r.visibleChildren() {
		height += workspaceSelectionSize(children[index]).Height
	}
	size.Height = max(size.Height, height)
	return size
}

func workspaceSelectionSize(renderObject ui.RenderObject) ui.Size {
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
