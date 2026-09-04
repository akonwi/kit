package tui

import "go.rockorager.dev/vaxis/ui"

const (
	workspaceWideColumns    = 125
	workspaceMinPrimary     = 70
	workspaceMinSecondary   = 53
	workspaceSecondaryRatio = 0.4
)

type workspaceLayoutState struct {
	Wide              bool
	TranscriptVisible bool
	ActivityVisible   bool
}

const (
	workspaceTabsChild = iota
	workspaceTranscriptChild
	workspacePendingChild
	workspaceComposerSeparatorChild
	workspaceComposerChild
	workspacePaneSeparatorChild
	workspaceActivityChild
	workspaceChildCount
)

// conversationWorkspaceHost owns both responsive workspace placement and the
// bottom-anchored conversation controls. In wide mode the Activity pane spans
// the full body beside the transcript, pending status, and composer column.
type conversationWorkspaceHost struct {
	Open              bool
	ActivitySelected  bool
	Tabs              ui.Widget
	Transcript        ui.Widget
	Pending           ui.Widget
	ComposerSeparator ui.Widget
	Composer          ui.Widget
	PaneSeparator     ui.Widget
	Activity          ui.Widget
	SeparatorStyle    ui.Style
	LayoutState       *workspaceLayoutState
}

func (w conversationWorkspaceHost) WidgetChildren() []ui.Widget {
	return []ui.Widget{
		w.Tabs,
		w.Transcript,
		w.Pending,
		w.ComposerSeparator,
		w.Composer,
		w.PaneSeparator,
		w.Activity,
	}
}

func (w conversationWorkspaceHost) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderConversationWorkspaceHost{
		Open: w.Open, ActivitySelected: w.ActivitySelected,
		SeparatorStyle: w.SeparatorStyle, LayoutState: w.LayoutState,
	}
}

func (w conversationWorkspaceHost) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	render := renderObject.(*renderConversationWorkspaceHost)
	if render.Open != w.Open || render.ActivitySelected != w.ActivitySelected {
		render.Open = w.Open
		render.ActivitySelected = w.ActivitySelected
		render.MarkNeedsLayout()
	}
	if render.SeparatorStyle != w.SeparatorStyle {
		render.SeparatorStyle = w.SeparatorStyle
		render.MarkNeedsPaint()
	}
	render.LayoutState = w.LayoutState
}

type workspaceParentData struct{ Offset ui.Offset }

func (data workspaceParentData) RenderOffset() ui.Offset { return data.Offset }

type renderConversationWorkspaceHost struct {
	ui.MultiChildRenderObject
	Open                 bool
	ActivitySelected     bool
	SeparatorStyle       ui.Style
	LayoutState          *workspaceLayoutState
	wide                 bool
	splitX               int
	primarySeparatorY    int
	paneHeaderSeparatorY int
	paneFooterSeparatorY int
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
	boundedHeight := constraints.HasBoundedHeight()
	height := constraints.MaxHeight
	if !boundedHeight {
		height = constraints.MinHeight
	}
	size := constraints.Constrain(ui.Size{Width: width, Height: height})
	if !dry && r.LayoutState != nil {
		r.LayoutState.Wide = width >= workspaceWideColumns
		r.LayoutState.TranscriptVisible = true
		r.LayoutState.ActivityVisible = false
	}
	if len(children) != workspaceChildCount {
		return size, layouts
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

	if !dry {
		r.wide = false
		r.splitX = 0
		r.primarySeparatorY = -1
		r.paneHeaderSeparatorY = -1
		r.paneFooterSeparatorY = -1
	}

	if !boundedHeight {
		return r.layoutUnbounded(ctx, constraints, layouts, dry, layoutChild, hide, width)
	}

	if !r.Open {
		hide(workspaceTabsChild)
		hide(workspacePaneSeparatorChild)
		hide(workspaceActivityChild)
		r.layoutConversationColumn(ctx, children, layouts, dry, layoutChild, width, height, 0, workspaceTranscriptChild)
		return size, layouts
	}

	if width >= workspaceWideColumns {
		if !dry && r.LayoutState != nil {
			r.LayoutState.ActivityVisible = true
		}
		hide(workspaceTabsChild)
		available := max(0, width-1)
		secondary := int(float64(available)*workspaceSecondaryRatio + 0.5)
		secondary = max(workspaceMinSecondary, min(secondary, max(0, available-workspaceMinPrimary)))
		primary := max(0, available-secondary)
		separatorWidth := min(1, width)

		separatorY, primarySeparatorHeight := r.layoutConversationColumn(
			ctx, children, layouts, dry, layoutChild, primary, height, 0, workspaceTranscriptChild,
		)
		layoutChild(
			workspacePaneSeparatorChild,
			ui.Tight(ui.Size{Width: separatorWidth, Height: height}),
			ui.Offset{X: primary},
		)
		layoutChild(
			workspaceActivityChild,
			ui.Tight(ui.Size{Width: secondary, Height: height}),
			ui.Offset{X: primary + separatorWidth},
		)
		if !dry {
			r.wide = separatorWidth > 0
			r.splitX = primary
			if primarySeparatorHeight > 0 {
				r.primarySeparatorY = separatorY
			}
			if height >= 2 {
				r.paneHeaderSeparatorY = 1
			}
			if height >= 4 {
				r.paneFooterSeparatorY = height - 2
			}
		}
		return size, layouts
	}

	tabHeight := min(1, height)
	layoutChild(
		workspaceTabsChild,
		ui.Tight(ui.Size{Width: width, Height: tabHeight}),
		ui.Offset{},
	)
	hide(workspacePaneSeparatorChild)
	mainChild := workspaceTranscriptChild
	hiddenChild := workspaceActivityChild
	if r.ActivitySelected {
		mainChild, hiddenChild = workspaceActivityChild, workspaceTranscriptChild
		if !dry && r.LayoutState != nil {
			r.LayoutState.TranscriptVisible = false
			r.LayoutState.ActivityVisible = true
		}
	}
	hide(hiddenChild)
	r.layoutConversationColumn(ctx, children, layouts, dry, layoutChild, width, max(0, height-tabHeight), tabHeight, mainChild)
	return size, layouts
}

func (r *renderConversationWorkspaceHost) layoutUnbounded(
	ctx ui.LayoutContext,
	constraints ui.Constraints,
	layouts []workspaceChildLayout,
	dry bool,
	layoutChild func(int, ui.Constraints, ui.Offset) ui.Size,
	hide func(int),
	width int,
) (ui.Size, []workspaceChildLayout) {
	mainChild := workspaceTranscriptChild
	yOffset := 0
	primary := width
	secondary := 0
	separatorWidth := 0

	if !r.Open {
		hide(workspaceTabsChild)
		hide(workspacePaneSeparatorChild)
		hide(workspaceActivityChild)
	} else if width >= workspaceWideColumns {
		if !dry && r.LayoutState != nil {
			r.LayoutState.ActivityVisible = true
		}
		hide(workspaceTabsChild)
		available := max(0, width-1)
		secondary = int(float64(available)*workspaceSecondaryRatio + 0.5)
		secondary = max(workspaceMinSecondary, min(secondary, max(0, available-workspaceMinPrimary)))
		primary = max(0, available-secondary)
		separatorWidth = min(1, width)
	} else {
		yOffset = 1
		layoutChild(workspaceTabsChild, ui.Tight(ui.Size{Width: width, Height: 1}), ui.Offset{})
		hide(workspacePaneSeparatorChild)
		if r.ActivitySelected {
			mainChild = workspaceActivityChild
			if !dry && r.LayoutState != nil {
				r.LayoutState.TranscriptVisible = false
				r.LayoutState.ActivityVisible = true
			}
			hide(workspaceTranscriptChild)
		} else {
			hide(workspaceActivityChild)
		}
	}

	composerSize := layoutChild(
		workspaceComposerChild,
		ui.Constraints{MinWidth: primary, MaxWidth: primary, MaxHeight: composerMaxHeight},
		ui.Offset{},
	)
	mainSize := layoutChild(
		mainChild,
		ui.Constraints{MinWidth: primary, MaxWidth: primary, MaxHeight: ui.Unbounded},
		ui.Offset{Y: yOffset},
	)
	pendingY := yOffset + mainSize.Height
	layoutChild(workspacePendingChild, ui.Tight(ui.Size{Width: primary, Height: 1}), ui.Offset{Y: pendingY})
	separatorY := pendingY + 1
	layoutChild(workspaceComposerSeparatorChild, ui.Tight(ui.Size{Width: primary, Height: 1}), ui.Offset{Y: separatorY})
	layouts[workspaceComposerChild].offset = ui.Offset{Y: separatorY + 1}
	height := separatorY + 1 + composerSize.Height

	if r.Open && width >= workspaceWideColumns {
		layoutChild(workspacePaneSeparatorChild, ui.Tight(ui.Size{Width: separatorWidth, Height: height}), ui.Offset{X: primary})
		layoutChild(workspaceActivityChild, ui.Tight(ui.Size{Width: secondary, Height: height}), ui.Offset{X: primary + separatorWidth})
		if !dry {
			r.wide = separatorWidth > 0
			r.splitX = primary
			r.primarySeparatorY = separatorY
			if height >= 2 {
				r.paneHeaderSeparatorY = 1
			}
			if height >= 4 {
				r.paneFooterSeparatorY = height - 2
			}
		}
	}
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
) (separatorY int, separatorHeight int) {
	composerLimit := min(1, height)
	if height >= 2 {
		separatorHeight = 1
	}
	pendingHeight := 0
	if height >= 4 {
		pendingHeight = 1
	}
	mainReserve := 0
	if height-pendingHeight-separatorHeight-composerLimit > 0 {
		mainReserve = 1
	}
	if composerLimit > 0 {
		composerLimit = min(composerMaxHeight, height-pendingHeight-separatorHeight-mainReserve)
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
	separatorY = yOffset + mainHeight + pendingHeight
	layoutChild(
		workspaceComposerSeparatorChild,
		ui.Tight(ui.Size{Width: width, Height: separatorHeight}),
		ui.Offset{Y: separatorY},
	)
	layouts[workspaceComposerChild].offset = ui.Offset{Y: separatorY + separatorHeight}
	return separatorY, separatorHeight
}

func (r *renderConversationWorkspaceHost) VisitChildren(visit func(ui.RenderObject)) {
	children := r.Children()
	if len(children) != workspaceChildCount || !r.Open || r.wide || !r.ActivitySelected {
		for _, child := range children {
			visit(child)
		}
		return
	}
	for _, index := range []int{
		workspaceTabsChild,
		workspaceTranscriptChild,
		workspaceActivityChild,
		workspacePendingChild,
		workspaceComposerSeparatorChild,
		workspaceComposerChild,
		workspacePaneSeparatorChild,
	} {
		visit(children[index])
	}
}

func (r *renderConversationWorkspaceHost) Paint(painter *ui.Painter, offset ui.Offset) {
	for _, child := range r.Children() {
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
	if !r.wide {
		return
	}
	junctions := make(map[int]string, 3)
	if r.paneHeaderSeparatorY >= 0 {
		junctions[r.paneHeaderSeparatorY] = glyphTeeRight
	}
	if r.paneFooterSeparatorY >= 0 {
		junctions[r.paneFooterSeparatorY] = glyphTeeRight
	}
	if r.primarySeparatorY >= 0 {
		if _, crossesRight := junctions[r.primarySeparatorY]; crossesRight {
			junctions[r.primarySeparatorY] = glyphCrossJunction
		} else {
			junctions[r.primarySeparatorY] = glyphTeeLeft
		}
	}
	for row, glyph := range junctions {
		painter.DrawText(offset.Add(ui.Offset{X: r.splitX, Y: row}), glyph, r.SeparatorStyle)
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
	if target < 0 || target == workspaceTabsChild || target == workspacePaneSeparatorChild || target == workspaceActivityChild && r.wide {
		return offset
	}
	mainChild := workspaceTranscriptChild
	order := make([]int, 0, 5)
	if r.Open && !r.wide {
		order = append(order, workspaceTabsChild)
		if r.ActivitySelected {
			mainChild = workspaceActivityChild
		}
	}
	order = append(order, mainChild, workspacePendingChild, workspaceComposerSeparatorChild, workspaceComposerChild)
	for _, index := range order {
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
	mainChild := workspaceTranscriptChild
	height := 0
	if r.Open && !r.wide {
		height += workspaceSelectionSize(children[workspaceTabsChild]).Height
		if r.ActivitySelected {
			mainChild = workspaceActivityChild
		}
	}
	for _, index := range []int{mainChild, workspacePendingChild, workspaceComposerSeparatorChild, workspaceComposerChild} {
		height += workspaceSelectionSize(children[index]).Height
	}
	if r.wide {
		height = max(height, workspaceSelectionSize(children[workspaceActivityChild]).Height)
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
