package tui

import "go.rockorager.dev/vaxis/ui"

// inlinePickerMaxRows bounds an inline picker's list; longer lists scroll
// behind overflow rows.
const inlinePickerMaxRows = 10

// inlinePickerChromeRows are the rows around an inline picker's list: the top
// border, the footer divider, the footer, and the bottom border.
const inlinePickerChromeRows = 4

// inlinePicker is the canonical non-modal list attached to the composer. Its
// fields are the whole contract: callers describe the query, catalog, and
// footer, and where the composer is; the inline picker owns filtering,
// placement, width, height, the frame, rows, highlight, overflow rows, the
// footer divider, and pointer activation.
//
// The composer text is the query and the composer keeps focus and the cursor,
// so an inline picker has no title or search field and never takes focus.
// Keys reach it only through pickerKeyModel.HandleNavigationKey on the app
// input-owner route. Rows, filtering, and the footer are shared with
// palettePicker.
//
// The picker is as wide as a palette picker, left-aligned with the composer.
// Its height fits the visible rows, up to inlinePickerMaxRows, and it grows
// upward: the bottom edge, next to the composer, never moves while filtering.
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
	Footer      string
	// Status is a footer message shown before the final hint.
	Status     string
	StatusTone pickerTone
	OnActivate func(ui.EventContext, string)
	// Left is the composer's left edge, in columns from the overlay's left
	// edge.
	Left int
	// BottomInset returns the rows between the overlay's bottom edge and the
	// picker's bottom edge for an overlay width, so the owner can keep the
	// picker directly above a composer whose height depends on wrapping. Nil
	// places the picker at the bottom edge.
	BottomInset func(width int) int
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
	return inlinePickerPositioner{
		Left: w.Left, BottomInset: w.BottomInset, Height: rows + inlinePickerChromeRows,
		Child: pickerFrame(theme, nil, list, pickerFooter(theme, w.Footer, w.Status, w.StatusTone)),
	}
}

// inlinePickerPositioner fills the overlay and places its child with the
// palette picker width rule at Left, bottom-aligned BottomInset rows above
// the overlay's bottom edge.
type inlinePickerPositioner struct {
	Left        int
	BottomInset func(width int) int
	Height      int
	Child       ui.Widget
}

func (w inlinePickerPositioner) WidgetChild() ui.Widget { return w.Child }

func (w inlinePickerPositioner) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderInlinePickerPositioner{Left: w.Left, BottomInset: w.BottomInset, Height: w.Height}
}

func (w inlinePickerPositioner) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderInlinePickerPositioner)
	// BottomInset is a function, so every update is laid out again.
	render.Left, render.BottomInset, render.Height = w.Left, w.BottomInset, w.Height
	render.MarkNeedsLayout()
}

type renderInlinePickerPositioner struct {
	ui.SingleChildRenderObject
	Left        int
	BottomInset func(width int) int
	Height      int
	offset      ui.Offset
}

func (r *renderInlinePickerPositioner) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	size := pickerDialogViewportSize(constraints)
	left := max(0, min(r.Left, size.Width))
	width := max(pickerMinWidth, min(pickerMaxWidth, size.Width*pickerWidthPercent/100))
	width = min(width, size.Width-left)
	inset := 0
	if r.BottomInset != nil {
		inset = max(0, r.BottomInset(size.Width))
	}
	bottom := max(0, size.Height-inset)
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
