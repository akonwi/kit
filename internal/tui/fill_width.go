package tui

import "go.rockorager.dev/vaxis/ui"

// fullWidthTextField adapts ui.TextField's intrinsic-width build to the width
// available during layout. Width changes settle on the next UI frame.
type fullWidthTextField struct {
	Field ui.TextField
}

func (fullWidthTextField) CreateState() ui.State { return &fullWidthTextFieldState{} }

type fullWidthTextFieldState struct {
	ui.StateBase
	width     int
	requested int
	disposed  bool
}

func (s *fullWidthTextFieldState) Build(ctx ui.BuildContext) ui.Widget {
	config := s.Widget().(fullWidthTextField)
	field := config.Field
	if s.width > 0 {
		field.MinWidth = s.width
	}
	runtime := ctx.Runtime()
	return widthObserver{
		Child: field,
		OnWidth: func(width int) {
			if width <= 0 || width == s.width || width == s.requested {
				return
			}
			s.requested = width
			runtime.Dispatch(func() {
				if s.disposed {
					return
				}
				s.SetState(func() {
					s.width = width
					s.requested = 0
				})
			})
		},
	}
}

func (s *fullWidthTextFieldState) Dispose() { s.disposed = true }

type widthObserver struct {
	Child   ui.Widget
	OnWidth func(int)
}

func (w widthObserver) WidgetChild() ui.Widget { return w.Child }

func (w widthObserver) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderWidthObserver{OnWidth: w.OnWidth}
}

func (w widthObserver) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	renderObject.(*renderWidthObserver).OnWidth = w.OnWidth
}

type renderWidthObserver struct {
	ui.SingleChildRenderObject
	OnWidth func(int)
}

func (r *renderWidthObserver) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	child := r.Child()
	if child == nil {
		r.SetSize(constraints.Constrain(ui.Size{}))
		return
	}
	child.Layout(ctx, observedWidthConstraints(constraints))
	r.SetSize(constraints.Constrain(child.Base().Size()))
	if r.OnWidth != nil {
		r.OnWidth(r.Size().Width)
	}
}

func (r *renderWidthObserver) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	child := r.Child()
	if child == nil {
		return constraints.Constrain(ui.Size{})
	}
	return constraints.Constrain(ui.DryLayout(ctx, child, observedWidthConstraints(constraints)))
}

func (r *renderWidthObserver) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderWidthObserver) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func observedWidthConstraints(constraints ui.Constraints) ui.Constraints {
	if constraints.HasBoundedWidth() {
		constraints.MinWidth = constraints.MaxWidth
	}
	return constraints
}
