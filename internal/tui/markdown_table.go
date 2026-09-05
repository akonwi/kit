package tui

import "go.rockorager.dev/vaxis/ui"

// responsiveMarkdownTable mounts exactly one presentation so hidden table
// links never remain in focus traversal. Width changes schedule a second frame
// that swaps between aligned and wrapping layouts.
type responsiveMarkdownTable struct {
	Threshold   int
	BuildWide   func() ui.Widget
	BuildNarrow func() ui.Widget
}

func (responsiveMarkdownTable) CreateState() ui.State { return &responsiveMarkdownTableState{} }

type responsiveMarkdownTableState struct {
	ui.StateBase
	wide     bool
	disposed bool
}

func (s *responsiveMarkdownTableState) Build(ctx ui.BuildContext) ui.Widget {
	config := s.Widget().(responsiveMarkdownTable)
	build := config.BuildNarrow
	if s.wide {
		build = config.BuildWide
	}
	var child ui.Widget
	if build != nil {
		child = build()
	}
	runtime := ctx.Runtime()
	return markdownTableMeasure{
		Threshold: config.Threshold,
		OnMode: func(wide bool) {
			if wide == s.wide {
				return
			}
			runtime.Dispatch(func() {
				if !s.disposed && wide != s.wide {
					s.SetState(func() { s.wide = wide })
				}
			})
		},
		Child: child,
	}
}

func (s *responsiveMarkdownTableState) Dispose() { s.disposed = true }

type markdownTableMeasure struct {
	Threshold int
	OnMode    func(bool)
	Child     ui.Widget
}

func (w markdownTableMeasure) WidgetChild() ui.Widget { return w.Child }

func (w markdownTableMeasure) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderMarkdownTableMeasure{Threshold: w.Threshold, OnMode: w.OnMode}
}

func (w markdownTableMeasure) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderMarkdownTableMeasure)
	render.Threshold = w.Threshold
	render.OnMode = w.OnMode
	render.MarkNeedsLayout()
}

type renderMarkdownTableMeasure struct {
	ui.SingleChildRenderObject
	Threshold int
	OnMode    func(bool)
}

func (r *renderMarkdownTableMeasure) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	child := r.Child()
	if child == nil {
		r.SetSize(constraints.Constrain(ui.Size{}))
		return
	}
	child.Layout(ctx, constraints)
	r.SetSize(constraints.Constrain(child.Base().Size()))
	if r.OnMode != nil {
		width := constraints.MaxWidth
		if !constraints.HasBoundedWidth() {
			width = max(constraints.MinWidth, r.Threshold)
		}
		r.OnMode(width >= r.Threshold)
	}
}

func (r *renderMarkdownTableMeasure) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderMarkdownTableMeasure) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderMarkdownTableMeasure) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
