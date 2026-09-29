package tui

import "go.rockorager.dev/vaxis/ui"

// inlinePickerMaxRows bounds an inline picker's list; longer lists scroll
// behind overflow rows.
const inlinePickerMaxRows = 10

// inlinePickerBorderRows are the top and bottom border rows around an inline
// picker's list; inlinePickerStatusRows are the divider and status row added
// while a status is shown.
const (
	inlinePickerBorderRows = 2
	inlinePickerStatusRows = 2
)

// inlinePicker is the canonical non-modal list attached to the composer. Its
// fields are the whole contract: callers describe the query, catalog, any
// status, and where the composer is; the inline picker owns filtering,
// placement, width, height, the frame, rows, highlight, overflow rows, the
// status divider, and pointer activation.
//
// The composer text is the query and the composer keeps focus and the cursor,
// so an inline picker has no title, search field, or footer hints, and never
// takes focus. Keys reach it only through pickerKeyModel.HandleNavigationKey
// on the app input-owner route. Rows, filtering, and the frame are shared
// with palettePicker.
//
// The picker is as wide as a palette picker and rests on the composer line
// where it was opened, aligned with that point and moved left only as far as
// it must to stay on screen. Its height fits the visible rows, up to
// inlinePickerMaxRows, and it grows upward: the bottom edge, next to the
// composer, never moves while filtering.
type inlinePicker struct {
	// Query is the picker's query, normally derived from the composer text.
	// The visible rows are Filter(Query, Catalog).
	Query string
	// Catalog is every item in catalog order. It also sizes the columns, so
	// filtering never shifts them.
	Catalog []pickerItem
	// Filter derives the visible rows; nil uses filterPickerItems. It must be
	// the filter the picker's key model uses.
	Filter pickerFilter
	// Selection is the highlighted key, normally the key model's Selection.
	Selection string
	// Message replaces the list with one row for loading, empty, and error
	// states. When empty and there are no visible rows, the row reads
	// "No results".
	Message     string
	MessageTone pickerTone
	// Status is a message, such as a failed refresh, shown below the list
	// behind a full-width divider. Inline pickers have no footer hints; the
	// keys are the composer's familiar ones.
	Status     string
	StatusTone pickerTone
	OnActivate func(ui.EventContext, string)
	// Anchor returns, for the overlay size, the cell in the composer where
	// the picker was opened, such as a mention's trigger. The picker's left
	// edge sits at its column and its bottom edge on the row above it, so the
	// picker rests on that line. Nil anchors the bottom-left corner.
	Anchor func(ui.Size) ui.Point
}

// items returns the rows an inline picker shows for its query.
func (w inlinePicker) items() []pickerItem {
	return pickerFilterOrDefault(w.Filter)(w.Query, w.Catalog)
}

func (inlinePicker) CreateState() ui.State { return &inlinePickerState{} }

type inlinePickerState struct {
	ui.StateBase
	list pickerListState
	// selectionIndex is where the selection was last shown, so rows added
	// above a kept selection still scroll it into view.
	selectionIndex int
}

func (s *inlinePickerState) Build(ctx ui.BuildContext) ui.Widget {
	w := s.Widget().(inlinePicker)
	theme := ui.MustDepend[ui.Theme](ctx)
	items := w.items()
	rows := 1
	if w.Message == "" && len(items) > 0 {
		rows = min(len(items), inlinePickerMaxRows)
	}
	index := pickerItemIndex(items, w.Selection)
	moved := index != s.selectionIndex
	s.selectionIndex = index
	list := s.list.build(theme, pickerList{
		Items: items, Catalog: w.Catalog, Selection: w.Selection,
		Message: w.Message, MessageTone: w.MessageTone,
		Rows: rows, Reveal: moved, OnActivate: w.OnActivate,
	}, s.SetState, s.MarkNeedsBuild)
	height := rows + inlinePickerBorderRows
	var footer ui.Widget
	if w.Status != "" {
		height += inlinePickerStatusRows
		footer = pickerFooter(theme, "", w.Status, w.StatusTone)
	}
	return inlinePickerPositioner{
		Anchor: w.Anchor, Height: height,
		Child: pickerFrame(theme, nil, list, footer),
	}
}

// inlinePickerPositioner fills the overlay and places its child with the
// palette picker width rule, its left edge at the anchor's column (clamped
// on screen) and its bottom edge on the row above the anchor.
type inlinePickerPositioner struct {
	Anchor func(ui.Size) ui.Point
	Height int
	Child  ui.Widget
}

func (w inlinePickerPositioner) WidgetChild() ui.Widget { return w.Child }

func (w inlinePickerPositioner) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderInlinePickerPositioner{Anchor: w.Anchor, Height: w.Height}
}

func (w inlinePickerPositioner) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderInlinePickerPositioner)
	// Anchor is a function, so every update is laid out again.
	render.Anchor, render.Height = w.Anchor, w.Height
	render.MarkNeedsLayout()
}

type renderInlinePickerPositioner struct {
	ui.SingleChildRenderObject
	Anchor func(ui.Size) ui.Point
	Height int
	offset ui.Offset
}

func (r *renderInlinePickerPositioner) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	size := pickerDialogViewportSize(constraints)
	anchor := ui.Point{Y: size.Height}
	if r.Anchor != nil {
		anchor = r.Anchor(size)
	}
	width := min(size.Width, max(pickerMinWidth, min(pickerMaxWidth, size.Width*pickerWidthPercent/100)))
	left := max(0, min(anchor.X, size.Width-width))
	bottom := max(0, min(anchor.Y, size.Height))
	height := min(r.Height, bottom)
	if child := r.Child(); child != nil {
		child.Layout(ctx, ui.Tight(ui.Size{Width: width, Height: height}))
		r.offset = ui.Offset{X: left, Y: bottom - height}
	}
	r.SetSize(size)
}

func (r *renderInlinePickerPositioner) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return pickerDialogViewportSize(constraints)
}

func (r *renderInlinePickerPositioner) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset.Add(r.offset))
	}
}

func (r *renderInlinePickerPositioner) ChildOffset(ui.RenderObject) ui.Offset { return r.offset }

func (*renderInlinePickerPositioner) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
