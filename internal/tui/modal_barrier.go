package tui

import "go.rockorager.dev/vaxis/ui"

// clearModalBarrier blocks pointer interaction with the shell without dimming
// or recoloring the viewport behind a modal surface.
// modalFocusAnchor is the final zero-size focus target in a modal scope. It is
// used only when the modal body has no interactive descendant.
type modalFocusAnchor struct{}

func (modalFocusAnchor) CreateState() ui.State { return &modalFocusAnchorState{} }

type modalFocusAnchorState struct {
	ui.StateBase
	node ui.FocusNode
}

func (s *modalFocusAnchorState) Build(ui.BuildContext) ui.Widget {
	return ui.Focus(&s.node, ui.SizedBox{Width: 0, Height: 0})
}

type clearModalBarrier struct{}

func (clearModalBarrier) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderClearModalBarrier{}
}

func (clearModalBarrier) UpdateRenderObject(ui.BuildContext, ui.RenderObject) {}

type renderClearModalBarrier struct{ ui.LeafRenderObject }

func (r *renderClearModalBarrier) Layout(_ ui.LayoutContext, constraints ui.Constraints) {
	size := ui.Size{}
	if constraints.HasBoundedWidth() {
		size.Width = constraints.MaxWidth
	}
	if constraints.HasBoundedHeight() {
		size.Height = constraints.MaxHeight
	}
	r.SetSize(constraints.Constrain(size))
}

func (r *renderClearModalBarrier) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	size := ui.Size{}
	if constraints.HasBoundedWidth() {
		size.Width = constraints.MaxWidth
	}
	if constraints.HasBoundedHeight() {
		size.Height = constraints.MaxHeight
	}
	return constraints.Constrain(size)
}

func (*renderClearModalBarrier) Paint(*ui.Painter, ui.Offset) {}

func (*renderClearModalBarrier) HitTest(*ui.HitTestResult, ui.Point) bool { return true }
