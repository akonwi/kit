package tui

import (
	"fmt"
	"strings"
	"time"

	"go.rockorager.dev/vaxis/ui"
)

const toolOutputMaxRows = 14

type toolOutputWell struct {
	Key          activityToolKey
	Output       string
	OuterScroll  *ui.ScrollController
	StickyBottom bool
}

func (w toolOutputWell) WidgetKey() ui.KeyValue {
	return ui.KeyValue("tool-output:" + w.Key.TurnID + ":" + w.Key.ToolCallID)
}

func (toolOutputWell) CreateState() ui.State { return &toolOutputWellState{} }

type toolOutputWellState struct {
	ui.StateBase
	scroll       ui.ScrollController
	viewportRows int
	contentRows  int
	needsMeasure bool
	needsStart   bool
	needsEnd     bool
}

func (s *toolOutputWellState) InitState() {
	well := s.Widget().(toolOutputWell)
	columns := 80
	if well.OuterScroll != nil && well.OuterScroll.Attached() {
		columns = max(1, well.OuterScroll.Metrics().ViewportWidth-4)
	}
	s.contentRows = estimateToolOutputRows(well.Output, columns)
	s.viewportRows = min(toolOutputMaxRows, s.contentRows)
	s.needsMeasure = true
	s.needsStart = !well.StickyBottom
	s.needsEnd = well.StickyBottom
}

func (s *toolOutputWellState) DidUpdateWidget(old ui.Widget) {
	previous := old.(toolOutputWell)
	well := s.Widget().(toolOutputWell)
	if previous.Key != well.Key {
		s.needsStart = !well.StickyBottom
		s.needsEnd = well.StickyBottom
		s.needsMeasure = true
	}
	if previous.Output != well.Output {
		columns := 80
		if well.OuterScroll != nil && well.OuterScroll.Attached() {
			columns = max(1, well.OuterScroll.Metrics().ViewportWidth-4)
		}
		s.contentRows = estimateToolOutputRows(well.Output, columns)
		s.viewportRows = min(toolOutputMaxRows, s.contentRows)
		s.needsMeasure = true
		if previous.StickyBottom || well.StickyBottom {
			s.needsEnd = true
		}
	}
	if !previous.StickyBottom && well.StickyBottom {
		s.needsEnd = true
	}
}

func (s *toolOutputWellState) TickFrame(_ time.Time) bool {
	if !s.scroll.Attached() {
		return s.needsMeasure || s.needsStart || s.needsEnd
	}
	metrics := s.scroll.Metrics()
	contentRows := max(1, metrics.ContentHeight)
	viewportRows := min(toolOutputMaxRows, contentRows)
	s.needsMeasure = false
	if contentRows != s.contentRows || viewportRows != s.viewportRows {
		s.SetState(func() {
			s.contentRows = contentRows
			s.viewportRows = viewportRows
		})
		return true
	}
	if s.needsStart {
		s.scroll.ScrollToStart()
		s.needsStart = false
	}
	if s.needsEnd {
		s.scroll.ScrollToEnd()
		s.needsEnd = false
	}
	return false
}

func (s *toolOutputWellState) Build(ctx ui.BuildContext) ui.Widget {
	well := s.Widget().(toolOutputWell)
	theme := ui.MustDepend[ui.Theme](ctx)
	lineCount := len(strings.Split(well.Output, "\n"))
	overflowing := lineCount > toolOutputMaxRows || s.contentRows > toolOutputMaxRows
	children := []ui.Widget{ui.SizedBox{Height: s.viewportRows, Child: nestedScrollHandoff{
		Inner: &s.scroll, Outer: well.OuterScroll,
		Child: ui.Scrollbar{Child: ui.CustomScrollView{
			Controller: &s.scroll, FollowOutput: well.StickyBottom,
			Slivers: []ui.Widget{ui.SliverToBox{Child: ui.Padding(ui.Symmetric(1, 0), ui.Text{
				Value: well.Output, Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true,
			})}},
		}},
	}}}
	if overflowing {
		unit := "lines"
		if lineCount == 1 {
			unit = "line"
		}
		metadata := fmt.Sprintf("%d %s", lineCount, unit)
		if s.contentRows > lineCount {
			metadata += " " + glyphMiddleDot + " wrapped"
		}
		children = append(children, ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Text{
			Value: metadata, Style: ui.Style{Foreground: theme.MutedForeground},
			Align: ui.TextAlignRight, MaxLines: 1,
		})})
	}
	return ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: theme.Surface}},
		ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: children},
	)
}

type nestedScrollHandoff struct {
	Inner *ui.ScrollController
	Outer *ui.ScrollController
	Child ui.Widget
}

func (w nestedScrollHandoff) WidgetChild() ui.Widget { return w.Child }

func (w nestedScrollHandoff) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderNestedScrollHandoff{Inner: w.Inner, Outer: w.Outer}
}

func (w nestedScrollHandoff) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	render := renderObject.(*renderNestedScrollHandoff)
	render.Inner = w.Inner
	render.Outer = w.Outer
}

type renderNestedScrollHandoff struct {
	ui.SingleChildRenderObject
	Inner *ui.ScrollController
	Outer *ui.ScrollController
}

func (r *renderNestedScrollHandoff) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
		return
	}
	r.SetSize(constraints.Constrain(ui.Size{}))
}

func (r *renderNestedScrollHandoff) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderNestedScrollHandoff) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderNestedScrollHandoff) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderNestedScrollHandoff) HandleEvent(_ ui.EventContext, event ui.Event) ui.EventResult {
	mouse, ok := event.(ui.Mouse)
	if !ok || mouse.EventType != ui.EventPress {
		return ui.EventIgnored
	}
	delta := 0
	switch mouse.Button {
	case ui.MouseWheelUp:
		delta = -1
	case ui.MouseWheelDown:
		delta = 1
	default:
		return ui.EventIgnored
	}
	if scrollControllerCanMove(r.Inner, delta) {
		r.Inner.ScrollByLines(delta)
		return ui.EventHandled
	}
	if r.Outer != nil && r.Outer.Attached() {
		r.Outer.ScrollByLines(delta)
	}
	return ui.EventHandled
}

func estimateToolOutputRows(output string, columns int) int {
	columns = max(1, columns)
	rows := 0
	for _, line := range strings.Split(output, "\n") {
		width := workspaceTextWidth(strings.ReplaceAll(line, "\t", "  "))
		rows += max(1, (width+columns-1)/columns)
	}
	return max(1, rows)
}

func scrollControllerCanMove(controller *ui.ScrollController, delta int) bool {
	if controller == nil || !controller.Attached() || delta == 0 {
		return false
	}
	metrics := controller.Metrics()
	return delta < 0 && metrics.ScrollOffset > 0 || delta > 0 && metrics.ScrollOffset < metrics.MaxScrollOffset
}
