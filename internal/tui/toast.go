package tui

import (
	"math"
	"strconv"
	"strings"
	"time"

	"go.rockorager.dev/vaxis/ui"
)

const (
	toastLifetime       = 10 * time.Second
	toastStackMax       = 5
	toastDetailMaxLines = 4
)

type toastVariant uint8

const (
	toastInfo toastVariant = iota
	toastWarning
	toastError
)

type toastInput struct {
	Title      string
	Subtitle   string
	Variant    toastVariant
	Persistent bool
}

type toastRecord struct {
	ID uint64
	toastInput
}

type toastController struct {
	nextID uint64
	items  []toastRecord
}

type toastShowResult struct {
	ID       uint64
	Retained bool
	Evicted  []uint64
}

func (c *toastController) Show(input toastInput) toastShowResult {
	input.Title = strings.TrimSpace(input.Title)
	input.Subtitle = strings.TrimSpace(input.Subtitle)
	c.nextID++
	record := toastRecord{ID: c.nextID, toastInput: input}
	c.items = append(c.items, record)
	result := toastShowResult{ID: record.ID, Retained: true}
	for len(c.items) > toastStackMax {
		victim := -1
		for index := range c.items {
			if !c.items[index].Persistent {
				victim = index
				break
			}
		}
		if victim < 0 {
			victim = 0
		}
		removed := c.items[victim].ID
		c.items = append(c.items[:victim], c.items[victim+1:]...)
		result.Evicted = append(result.Evicted, removed)
		if removed == record.ID {
			result.Retained = false
		}
	}
	return result
}

func (c *toastController) Dismiss(id uint64) bool {
	for index := range c.items {
		if c.items[index].ID != id {
			continue
		}
		c.items = append(c.items[:index], c.items[index+1:]...)
		return true
	}
	return false
}

func (c *toastController) Snapshot() []toastRecord {
	return append([]toastRecord(nil), c.items...)
}

type toastStack struct {
	Toasts    []toastRecord
	OnDismiss func(uint64)
	Animate   bool
}

func (toastStack) WidgetKey() ui.KeyValue { return "toast-stack" }

func (toastStack) CreateState() ui.State { return &toastStackState{} }

type toastStackState struct {
	ui.StateBase
	viewportWidth  int
	viewportHeight int
	displayed      map[uint64]bool
}

func (s *toastStackState) Build(ui.BuildContext) ui.Widget {
	w := s.Widget().(toastStack)
	if s.displayed == nil {
		s.displayed = make(map[uint64]bool)
	}
	retained := make(map[uint64]bool, len(w.Toasts))
	for _, record := range w.Toasts {
		retained[record.ID] = true
	}
	for id := range s.displayed {
		if !retained[id] {
			delete(s.displayed, id)
		}
	}
	visible := visibleToastRecords(w.Toasts, s.viewportWidth, s.viewportHeight)
	items := make([]ui.Widget, 0, len(visible))
	for _, record := range visible {
		record := record
		animate := toastEntryShouldAnimate(s.displayed, record.ID, w.Animate)
		items = append(items, toastItem{Toast: record, Animate: animate, OnDismiss: func(ui.EventContext) {
			if w.OnDismiss != nil {
				w.OnDismiss(record.ID)
			}
		}})
	}
	list := ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin,
		CrossAxisAlignment: ui.CrossAxisEnd, Children: items,
	}
	return toastSizeProbe{
		Child: toastPositioner{Child: list},
		OnSize: func(size ui.Size) {
			if size.Width != s.viewportWidth || size.Height != s.viewportHeight {
				s.viewportWidth = size.Width
				s.viewportHeight = size.Height
				s.MarkNeedsBuild()
			}
		},
	}
}

func toastEntryShouldAnimate(displayed map[uint64]bool, id uint64, enabled bool) bool {
	animate := enabled && !displayed[id]
	displayed[id] = true
	return animate
}

func visibleToastRecords(records []toastRecord, viewportWidth, viewportHeight int) []toastRecord {
	if len(records) == 0 {
		return nil
	}
	available := max(1, viewportHeight-2)
	start, used := len(records), 0
	overlayWidth := toastOverlayWidth(viewportWidth)
	for index := len(records) - 1; index >= 0; index-- {
		height := 3
		if records[index].Subtitle != "" {
			detailWidth := max(1, overlayWidth-2)
			if records[index].Persistent {
				detailWidth = max(1, detailWidth-2)
			}
			layout := ui.LayoutText(
				[]ui.TextSpan{{Text: records[index].Subtitle}},
				ui.Constraints{MaxWidth: detailWidth, MaxHeight: ui.Unbounded},
				ui.TextLayoutOptions{SoftWrap: true, Overflow: ui.TextOverflowEllipsis, MaxLines: toastDetailMaxLines},
			)
			height += max(1, layout.Size.Height)
		}
		if used > 0 && used+height > available {
			break
		}
		start = index
		used += height
	}
	return records[start:]
}

type toastSizeProbe struct {
	Child  ui.Widget
	OnSize func(ui.Size)
}

func (w toastSizeProbe) WidgetChild() ui.Widget { return w.Child }

func (w toastSizeProbe) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderToastSizeProbe{OnSize: w.OnSize}
}

func (w toastSizeProbe) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	object.(*renderToastSizeProbe).OnSize = w.OnSize
}

type renderToastSizeProbe struct {
	ui.SingleChildRenderObject
	OnSize func(ui.Size)
}

func (r *renderToastSizeProbe) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
	} else {
		r.SetSize(constraints.Constrain(ui.Size{}))
	}
	if r.OnSize != nil {
		r.OnSize(r.Size())
	}
}

func (r *renderToastSizeProbe) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderToastSizeProbe) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderToastSizeProbe) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
func (*renderToastSizeProbe) HitTestSelf(ui.Point) bool                { return false }

func (r *renderToastSizeProbe) SelectionDisabled() bool { return true }

type toastPositioner struct{ Child ui.Widget }

func (w toastPositioner) WidgetChild() ui.Widget { return w.Child }

func (toastPositioner) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderToastPositioner{}
}

func (toastPositioner) UpdateRenderObject(ui.BuildContext, ui.RenderObject) {}

type renderToastPositioner struct {
	ui.SingleChildRenderObject
	childOffset ui.Offset
}

func (r *renderToastPositioner) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	child := r.Child()
	if child == nil {
		r.SetSize(constraints.Constrain(ui.Size{}))
		return
	}
	width := toastOverlayWidth(constraints.MaxWidth)
	maxHeight := constraints.MaxHeight
	if maxHeight != ui.Unbounded {
		maxHeight = max(0, maxHeight-2)
	}
	child.Layout(ctx, ui.Constraints{MaxWidth: width, MaxHeight: maxHeight})
	size := constraints.Constrain(ui.Size{Width: constraints.MaxWidth, Height: constraints.MaxHeight})
	if constraints.MaxWidth == ui.Unbounded {
		size.Width = child.Base().Size().Width + 2
	}
	if constraints.MaxHeight == ui.Unbounded {
		size.Height = child.Base().Size().Height + 2
	}
	r.childOffset = ui.Offset{X: max(0, size.Width-child.Base().Size().Width-2), Y: min(2, max(0, size.Height-child.Base().Size().Height))}
	r.SetSize(size)
}

func (r *renderToastPositioner) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		width := toastOverlayWidth(constraints.MaxWidth)
		childSize := ui.DryLayout(ctx, child, ui.Constraints{MaxWidth: width, MaxHeight: constraints.MaxHeight})
		return constraints.Constrain(ui.Size{Width: constraints.MaxWidth, Height: childSize.Height + 2})
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderToastPositioner) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset.Add(r.childOffset))
	}
}

func (*renderToastPositioner) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
func (*renderToastPositioner) HitTestSelf(ui.Point) bool                { return false }
func (*renderToastPositioner) SelectionDisabled() bool                  { return true }

func (r *renderToastPositioner) ChildOffset(ui.RenderObject) ui.Offset { return r.childOffset }

func toastOverlayWidth(available int) int {
	if available == ui.Unbounded {
		return 64
	}
	return max(1, available*3/5)
}

type toastItem struct {
	Toast     toastRecord
	OnDismiss ui.VoidCallback
	Animate   bool
}

func (w toastItem) WidgetKey() ui.KeyValue {
	return ui.KeyValue("toast:" + strconv.FormatUint(w.Toast.ID, 10))
}

func (toastItem) CreateState() ui.State { return &toastItemState{} }

type toastItemState struct {
	ui.StateBase
	entry *ui.AnimationController
}

func (s *toastItemState) InitState() {
	duration := time.Duration(0)
	if s.Widget().(toastItem).Animate {
		duration = 300 * time.Millisecond
	}
	s.entry = s.NewAnimation(ui.AnimationOptions{Duration: duration, Curve: toastEaseOutCirc})
	s.entry.Forward()
}

func (s *toastItemState) Build(ctx ui.BuildContext) ui.Widget {
	w := s.Widget().(toastItem)
	theme := ui.MustDepend[ui.Theme](ctx)
	color := toastAppearance(theme, w.Toast.Variant)
	text := []ui.Widget{ui.Text{
		Value: w.Toast.Title, Style: ui.Style{Foreground: color},
		Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
	}}
	if w.Toast.Subtitle != "" {
		text = append(text, ui.Text{
			Value: w.Toast.Subtitle, Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true,
			Overflow: ui.TextOverflowEllipsis, MaxLines: toastDetailMaxLines,
		})
	}
	children := []ui.Widget{ui.Flexible(ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin,
		CrossAxisAlignment: ui.CrossAxisStart, Children: text,
	})}
	if w.Toast.Persistent && w.OnDismiss != nil {
		children = append(children, ui.SizedBox{Width: 1}, toastClose{OnPressed: w.OnDismiss})
	}
	content := ui.Flex{
		Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMin,
		CrossAxisAlignment: ui.CrossAxisStart, Children: children,
	}
	card := mouseActivator{
		Child: ui.DecoratedBox(
			ui.Decoration{
				Style:  ui.Style{Background: theme.Background},
				Border: ui.BorderLine(color),
			},
			ui.Padding(ui.All(1), content),
		),
		OnPressed: func(ui.EventContext) {}, DefaultMouseShape: true,
	}
	offset := int(math.Round(20 * (1 - s.entry.Value())))
	return toastSlide{Offset: offset, Child: card}
}

func toastEaseOutCirc(value float64) float64 {
	value = max(0.0, min(1.0, value))
	return math.Sqrt(1 - math.Pow(value-1, 2))
}

type toastSlide struct {
	Offset int
	Child  ui.Widget
}

func (w toastSlide) WidgetChild() ui.Widget { return w.Child }

func (w toastSlide) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderToastSlide{offset: w.Offset}
}

func (w toastSlide) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderToastSlide)
	if render.offset != w.Offset {
		render.offset = w.Offset
		render.MarkNeedsPaint()
	}
}

type renderToastSlide struct {
	ui.SingleChildRenderObject
	offset int
}

func (r *renderToastSlide) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
		return
	}
	r.SetSize(constraints.Constrain(ui.Size{}))
}

func (r *renderToastSlide) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderToastSlide) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset.Add(ui.Offset{X: r.offset}))
	}
}

func (*renderToastSlide) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
func (*renderToastSlide) HitTestSelf(ui.Point) bool                { return false }

func (r *renderToastSlide) ChildOffset(ui.RenderObject) ui.Offset {
	return ui.Offset{X: r.offset}
}

type toastClose struct{ OnPressed ui.VoidCallback }

func (toastClose) CreateState() ui.State { return &toastCloseState{} }

type toastCloseState struct {
	ui.StateBase
	hovered bool
}

func (s *toastCloseState) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	style := ui.Style{Foreground: theme.MutedForeground}
	if s.hovered {
		style.Background = theme.SurfaceHovered
		style.Foreground = theme.Foreground
	}
	return mouseActivator{
		Child:     ui.DecoratedBox(ui.Decoration{Style: style}, ui.Text{Value: glyphTimes, Style: style, MaxLines: 1}),
		OnPressed: s.Widget().(toastClose).OnPressed,
		OnHover: func(ui.EventContext) {
			if !s.hovered {
				s.SetState(func() { s.hovered = true })
			}
		},
		OnHoverExit: func(ui.EventContext) {
			if s.hovered {
				s.SetState(func() { s.hovered = false })
			}
		},
	}
}

func toastAppearance(theme ui.Theme, variant toastVariant) ui.Color {
	switch variant {
	case toastError:
		return theme.DangerText
	case toastWarning:
		return theme.WarningText
	default:
		return theme.AccentText
	}
}
