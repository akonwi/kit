package tui

import "go.rockorager.dev/vaxis/ui"

// retainedWorkspacePane gives each mounted pane a stable keyed subtree while
// removing hidden panes from layout, paint, hit testing, and selection.
type retainedWorkspacePane struct {
	Identity workspacePaneIdentity
	Active   bool
	Child    ui.Widget
}

func (w retainedWorkspacePane) WidgetKey() ui.KeyValue { return ui.KeyValue(w.Identity) }

func (w retainedWorkspacePane) WidgetChild() ui.Widget { return w.Child }

func (w retainedWorkspacePane) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderRetainedWorkspacePane{Active: w.Active}
}

func (w retainedWorkspacePane) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	render := renderObject.(*renderRetainedWorkspacePane)
	if render.Active != w.Active {
		render.Active = w.Active
		render.MarkNeedsLayout()
	}
}

type renderRetainedWorkspacePane struct {
	ui.SingleChildRenderObject
	Active bool
}

func (r *renderRetainedWorkspacePane) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if child := r.Child(); child != nil {
		if !r.Active {
			// Keep the child's last layout intact while removing this wrapper from
			// its parent layout. Relaying scrollable children at zero size can
			// rewrite their retained viewport and scroll offset.
			r.SetSize(ui.Size{})
			return
		}
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
		return
	}
	r.SetSize(constraints.Constrain(ui.Size{}))
}

func (r *renderRetainedWorkspacePane) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if !r.Active {
		return ui.Size{}
	}
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderRetainedWorkspacePane) Paint(painter *ui.Painter, offset ui.Offset) {
	if r.Active {
		if child := r.Child(); child != nil {
			child.Paint(painter, offset)
		}
	}
}

func (*renderRetainedWorkspacePane) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderRetainedWorkspacePane) SelectionDisabled() bool { return !r.Active }

type retainedWorkspacePaneStack struct {
	Panes []ui.Widget
}

func (w retainedWorkspacePaneStack) WidgetChildren() []ui.Widget { return w.Panes }

func (retainedWorkspacePaneStack) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderRetainedWorkspacePaneStack{}
}

func (retainedWorkspacePaneStack) UpdateRenderObject(ui.BuildContext, ui.RenderObject) {}

type renderRetainedWorkspacePaneStack struct{ ui.MultiChildRenderObject }

func (r *renderRetainedWorkspacePaneStack) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	size := ui.Size{}
	for _, child := range r.Children() {
		child.Layout(ctx, constraints)
		childSize := child.Base().Size()
		size.Width = max(size.Width, childSize.Width)
		size.Height = max(size.Height, childSize.Height)
	}
	r.SetSize(constraints.Constrain(size))
}

func (r *renderRetainedWorkspacePaneStack) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	size := ui.Size{}
	for _, child := range r.Children() {
		childSize := ui.DryLayout(ctx, child, constraints)
		size.Width = max(size.Width, childSize.Width)
		size.Height = max(size.Height, childSize.Height)
	}
	return constraints.Constrain(size)
}

func (r *renderRetainedWorkspacePaneStack) VisitChildren(visit func(ui.RenderObject)) {
	for _, child := range r.Children() {
		if child.Base().Size().Width > 0 && child.Base().Size().Height > 0 {
			visit(child)
		}
	}
}

func (r *renderRetainedWorkspacePaneStack) Paint(painter *ui.Painter, offset ui.Offset) {
	for _, child := range r.Children() {
		if child.Base().Size().Width > 0 && child.Base().Size().Height > 0 {
			child.Paint(painter, offset)
		}
	}
}

func (*renderRetainedWorkspacePaneStack) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderRetainedWorkspacePaneStack) SelectionSize() ui.Size {
	size := r.Size()
	for _, child := range r.Children() {
		if child.Base().Size().Width > 0 && child.Base().Size().Height > 0 {
			return workspaceSelectionSize(child)
		}
	}
	return size
}
