package tui

import (
	"fmt"

	"go.rockorager.dev/vaxis/ui"
)

// workspaceTabStrip keeps Agent and the selected tab reachable, collapsing all
// other tabs behind a labeled overflow control when the canonical strip does
// not fit.
type workspaceTabStrip struct {
	Tabs       []workspaceTab
	Selected   int
	OnOverflow ui.VoidCallback
}

func (w workspaceTabStrip) WidgetChildren() []ui.Widget {
	variantCount := int(workspacePaneActivityCount)
	children := make([]ui.Widget, 0, len(w.Tabs)+max(0, len(w.Tabs)-1)*variantCount)
	for _, tab := range w.Tabs {
		children = append(children, tab)
	}
	for hidden := 1; hidden < len(w.Tabs); hidden++ {
		for activity := workspacePaneActivityNone; activity < workspacePaneActivityCount; activity++ {
			children = append(children, workspaceTab{
				Label: fmt.Sprintf("⋯ %d more", hidden), Activity: activity, OnSelect: w.OnOverflow,
			})
		}
	}
	return children
}

func (w workspaceTabStrip) activities() []workspacePaneActivity {
	activities := make([]workspacePaneActivity, len(w.Tabs))
	for index, tab := range w.Tabs {
		activities[index] = tab.Activity
	}
	return activities
}

func (w workspaceTabStrip) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderWorkspaceTabStrip{Selected: w.Selected, TabCount: len(w.Tabs), Activities: w.activities()}
}

func (w workspaceTabStrip) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	render := renderObject.(*renderWorkspaceTabStrip)
	activities := w.activities()
	if render.Selected != w.Selected || render.TabCount != len(w.Tabs) || !workspaceActivitiesEqual(render.Activities, activities) {
		render.Selected = w.Selected
		render.TabCount = len(w.Tabs)
		render.Activities = activities
		render.MarkNeedsLayout()
	}
}

func workspaceActivitiesEqual(left, right []workspacePaneActivity) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type workspaceTabStripParentData struct{ Offset ui.Offset }

func (data workspaceTabStripParentData) RenderOffset() ui.Offset { return data.Offset }

type renderWorkspaceTabStrip struct {
	ui.MultiChildRenderObject
	Selected   int
	TabCount   int
	Activities []workspacePaneActivity
}

func (r *renderWorkspaceTabStrip) SetupParentData(child ui.RenderObject) {
	if _, ok := child.Base().ParentData().(workspaceTabStripParentData); !ok {
		child.Base().SetParentData(workspaceTabStripParentData{})
	}
}

func (r *renderWorkspaceTabStrip) overflowIndex(hidden int, activity workspacePaneActivity) int {
	return r.TabCount + (hidden-1)*int(workspacePaneActivityCount) + int(activity)
}

func (r *renderWorkspaceTabStrip) hiddenActivity(visible []bool) workspacePaneActivity {
	activity := workspacePaneActivityNone
	for index := 1; index < min(len(visible), len(r.Activities)); index++ {
		if !visible[index] && r.Activities[index] > activity {
			activity = r.Activities[index]
		}
	}
	return activity
}

func (r *renderWorkspaceTabStrip) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	children := r.Children()
	width := constraints.MaxWidth
	if !constraints.HasBoundedWidth() {
		width = constraints.MinWidth
	}
	height := min(1, constraints.MaxHeight)
	if !constraints.HasBoundedHeight() {
		height = 1
	}
	r.SetSize(constraints.Constrain(ui.Size{Width: width, Height: height}))
	expectedChildren := r.TabCount + max(0, r.TabCount-1)*int(workspacePaneActivityCount)
	if len(children) != expectedChildren || r.TabCount == 0 || width <= 0 || height <= 0 {
		for _, child := range children {
			child.Layout(ctx, ui.Tight(ui.Size{}))
		}
		return
	}

	loose := ui.Constraints{MaxWidth: width, MaxHeight: height}
	desired := make([]int, len(children))
	total := 0
	for index, child := range children {
		desired[index] = ui.DryLayout(ctx, child, loose).Width
		if index < r.TabCount {
			total += desired[index]
		}
	}
	if total <= width {
		x := 0
		for index := 0; index < r.TabCount; index++ {
			x += r.layoutChild(ctx, children[index], x, min(desired[index], max(0, width-x)), height)
		}
		for index := r.TabCount; index < len(children); index++ {
			children[index].Layout(ctx, ui.Tight(ui.Size{}))
		}
		return
	}

	visible := make([]bool, r.TabCount)
	visible[0] = true
	visibleCount := 1
	visibleWidth := desired[0]
	if r.Selected > 0 && r.Selected < r.TabCount {
		visible[r.Selected] = true
		visibleCount++
		visibleWidth += desired[r.Selected]
	}
	for index := 1; index < r.TabCount; index++ {
		if visible[index] {
			continue
		}
		proposedHidden := r.TabCount - visibleCount - 1
		visible[index] = true
		overflowWidth := 0
		if proposedHidden > 0 {
			overflowWidth = desired[r.overflowIndex(proposedHidden, r.hiddenActivity(visible))]
		}
		if visibleWidth+desired[index]+overflowWidth <= width {
			visibleCount++
			visibleWidth += desired[index]
		} else {
			visible[index] = false
		}
	}
	hidden := r.TabCount - visibleCount
	overflowIndex := r.overflowIndex(hidden, r.hiddenActivity(visible))
	for index, child := range children {
		show := index < r.TabCount && visible[index]
		show = show || hidden > 0 && index == overflowIndex
		if !show {
			child.Layout(ctx, ui.Tight(ui.Size{}))
		}
	}
	overflowWidth := desired[overflowIndex]
	if visibleWidth+overflowWidth <= width {
		x := 0
		for index := 0; index < r.TabCount; index++ {
			if visible[index] {
				x += r.layoutChild(ctx, children[index], x, desired[index], height)
			}
		}
		r.layoutChild(ctx, children[overflowIndex], x, overflowWidth, height)
		return
	}

	// At extremely narrow widths, reserve the labeled overflow control and the
	// selected tab before giving Agent the remaining cells.
	for index := 1; index < r.TabCount; index++ {
		if index != r.Selected {
			children[index].Layout(ctx, ui.Tight(ui.Size{}))
		}
	}
	overflowWidth = min(overflowWidth, width)
	remaining := max(0, width-overflowWidth)
	x := 0
	if r.Selected > 0 && r.Selected < r.TabCount {
		agentWidth := min(desired[0], min(workspaceTabMinWidth, remaining))
		selectedWidth := min(desired[r.Selected], max(0, remaining-agentWidth))
		if selectedWidth < min(workspaceTabMinWidth, remaining) {
			selectedWidth = min(workspaceTabMinWidth, remaining)
			agentWidth = max(0, remaining-selectedWidth)
		}
		x += r.layoutChild(ctx, children[0], x, agentWidth, height)
		x += r.layoutChild(ctx, children[r.Selected], x, selectedWidth, height)
	} else {
		x += r.layoutChild(ctx, children[0], x, remaining, height)
	}
	r.layoutChild(ctx, children[overflowIndex], x, max(0, width-x), height)
}

func (r *renderWorkspaceTabStrip) layoutChild(ctx ui.LayoutContext, child ui.RenderObject, x, width, height int) int {
	width = max(0, width)
	child.Layout(ctx, ui.Tight(ui.Size{Width: width, Height: height}))
	child.Base().SetParentData(workspaceTabStripParentData{Offset: ui.Offset{X: x}})
	return child.Base().Size().Width
}

func (r *renderWorkspaceTabStrip) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	width := constraints.MaxWidth
	if !constraints.HasBoundedWidth() {
		width = constraints.MinWidth
	}
	return constraints.Constrain(ui.Size{Width: width, Height: 1})
}

func (r *renderWorkspaceTabStrip) VisitChildren(visit func(ui.RenderObject)) {
	for _, child := range r.Children() {
		if child.Base().Size().Width > 0 && child.Base().Size().Height > 0 {
			visit(child)
		}
	}
}

func (r *renderWorkspaceTabStrip) Paint(painter *ui.Painter, offset ui.Offset) {
	for _, child := range r.Children() {
		if child.Base().Size().Width == 0 || child.Base().Size().Height == 0 {
			continue
		}
		data, _ := child.Base().ParentData().(workspaceTabStripParentData)
		childOffset := offset.Add(data.Offset)
		size := child.Base().Size()
		painter.PushClip(ui.Rect{X: childOffset.X, Y: childOffset.Y, Width: size.Width, Height: size.Height})
		child.Paint(painter, childOffset)
		painter.PopClip()
	}
}

func (*renderWorkspaceTabStrip) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderWorkspaceTabStrip) SelectionSize() ui.Size { return r.Size() }
