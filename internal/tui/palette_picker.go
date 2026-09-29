package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

// palettePicker is the canonical centered modal picker, shaped like the command
// palette. Its fields are the whole contract: callers describe the title,
// query, catalog, and footer; the palette picker owns filtering, the search
// field, the frame, alignment, highlight, overflow rows, and pointer
// activation. Lists attached to the composer are inline pickers
// (inlinePicker); they share pickerItem, pickerFrame, the list, the footer,
// filterPickerItems, and pickerKeyModel with this frame.
type palettePicker struct {
	Title     string
	TitleMeta string
	// TitleMetaTone colors the title metadata; pickerToneLoading shows it
	// after the shared spinner, for example while an activation is pending.
	TitleMetaTone pickerTone
	// Query is the picker's query, owned by its pickerKeyModel. The visible
	// rows are Filter(Query, Catalog).
	Query string
	// Search shows the shared search field when non-nil. The field displays
	// Query and is display-only: query edits arrive through pickerKeyModel.
	Search *pickerSearch
	// Catalog is every item in catalog order. It also sizes the columns, so
	// filtering never shifts them.
	Catalog []pickerItem
	// Filter derives the visible rows; nil uses filterPickerItems. It must be
	// the filter the picker's key model uses.
	Filter pickerFilter
	// Selection is the highlighted key, normally the key model's Selection.
	Selection string
	// Message replaces the list for loading, empty, and error states. When
	// empty and there are no visible rows, the list reads "No results".
	Message     string
	MessageTone pickerTone
	Footer      string
	// Status is a footer message shown before the hints, such as a failure.
	Status     string
	StatusTone pickerTone
	OnActivate func(ui.EventContext, string)
	// OnToggle is called with an item key when its disclosure is clicked.
	// Clicks elsewhere on the row still activate it.
	OnToggle func(ui.EventContext, string)
	// OnKey is only for pane-owned pickers that cannot use the app input-owner
	// route. The callback must delegate meaning to pickerKeyModel.
	OnKey func(ui.Key) ui.EventResult
}

// pickerSearch configures a palette picker's search field.
type pickerSearch struct {
	Placeholder string
}

// items returns the rows a palette picker shows for its query.
func (w palettePicker) items() []pickerItem {
	return pickerFilterOrDefault(w.Filter)(w.Query, w.Catalog)
}

// searchInput builds the display-only search field configuration. The cursor
// sits at the end of the query, the only position query editing supports.
func (w palettePicker) searchInput() textInputConfig {
	cursor := len((ui.LayoutContext{}).Characters(w.Query))
	return textInputConfig{
		Value: w.Query, Placeholder: w.Search.Placeholder, CursorOffset: &cursor, AutoFocus: true, ReadOnly: true,
	}
}

func (palettePicker) CreateState() ui.State { return &palettePickerState{} }

type palettePickerState struct {
	ui.StateBase
	list pickerListState
}

func (s *palettePickerState) Build(ctx ui.BuildContext) ui.Widget {
	w := s.Widget().(palettePicker)
	theme := ui.MustDepend[ui.Theme](ctx)
	var header ui.Widget
	title := pickerTitleRow(theme, w.Title, w.TitleMeta, w.TitleMetaTone)
	switch {
	case title != nil && w.Search != nil:
		header = pickerTitledSearchField(theme, title, w.searchInput())
	case title != nil:
		header = ui.Padding(ui.Insets{Top: 1, Right: pickerContentInset, Bottom: 1, Left: pickerContentInset}, title)
	case w.Search != nil:
		header = ui.Padding(ui.Insets{Top: 1}, pickerSearchField(theme, w.searchInput()))
	}
	list := s.list.build(theme, pickerList{
		Items: w.items(), Catalog: w.Catalog, Selection: w.Selection,
		Message: w.Message, MessageTone: w.MessageTone,
		OnActivate: w.OnActivate, OnToggle: w.OnToggle,
	}, s.SetState, s.MarkNeedsBuild)
	content := pickerFrame(theme, header, list, pickerFooter(theme, w.Footer, w.Status, w.StatusTone))
	if w.OnKey != nil {
		content = pickerKeyListener{OnKey: w.OnKey, Child: content}
	}
	return pickerDialogPositioner{
		Percent: pickerWidthPercent, MinWidth: pickerMinWidth, MaxWidth: pickerMaxWidth, Height: pickerModalMinHeight,
		Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: content},
	}
}

// Every palette picker shares one size; inline pickers share its width.
const (
	pickerWidthPercent = 80
	pickerMinWidth     = 48
	pickerMaxWidth     = 96
)

// pickerFrame is the bordered structure shared by palette and inline
// pickers: a header (a blank row under the top border when nil), the list
// inset one cell from each border, and the footer below a full-width divider
// that joins the borders.
func pickerFrame(theme ui.Theme, header, list, footer ui.Widget) ui.Widget {
	if header == nil {
		header = ui.SizedBox{Height: 1}
	}
	return pickerDialogContent(theme, ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		header,
		ui.Expanded(ui.Padding(ui.Insets{Right: 1, Left: 1}, list)),
	}}, footer)
}

// pickerList describes the rows of one picker list build.
type pickerList struct {
	// Items are the visible rows in display order.
	Items []pickerItem
	// Catalog sizes the columns, so filtering never shifts them.
	Catalog   []pickerItem
	Selection string
	// Message replaces the rows; when empty and there are no Items, the list
	// reads "No results".
	Message     string
	MessageTone pickerTone
	// Rows is the list height when the frame knows it; zero uses the height
	// the viewport measured last frame.
	Rows int
	// Reveal scrolls the selection into view even when its key is unchanged,
	// for frames whose rows can move under a kept selection.
	Reveal     bool
	OnActivate func(ui.EventContext, string)
	OnToggle   func(ui.EventContext, string)
}

// pickerListState is the scroll position a picker frame keeps for its list
// between builds. Its list rendering, highlight, overflow rows, wheel
// scrolling, and pointer activation are shared by every picker frame.
type pickerListState struct {
	viewport      pickerViewportState
	lastSelection string
	hasSelection  bool
}

// build returns the list for one frame. setState applies scroll changes and
// rebuild is called when the viewport height changes.
func (s *pickerListState) build(theme ui.Theme, w pickerList, setState func(func()), rebuild func()) ui.Widget {
	items := w.Items
	if w.Message != "" || len(items) == 0 {
		message := w.Message
		if message == "" {
			message = "No results"
		}
		return ui.Padding(ui.Insets{Left: 1}, pickerMessage(theme, message, w.MessageTone))
	}
	selection := pickerItemIndex(items, w.Selection)
	reveal := -1
	if !s.hasSelection || w.Selection != s.lastSelection || w.Reveal {
		reveal = selection
		s.lastSelection, s.hasSelection = w.Selection, true
	}
	// Build rows for the window this height produced last frame; the viewport
	// resolves the real height and a changed height rebuilds the run.
	rows := w.Rows
	if rows <= 0 {
		rows = s.viewport.Rows
	}
	if rows <= 0 {
		rows = pickerModalMinHeight
	}
	window := resolveListWindow(len(items), rows, s.viewport.Start, reveal)
	first := window.Start
	last := min(len(items), first+rows)
	columns := measurePickerColumns(w.Catalog)
	scrollBy := func(delta int) {
		setState(func() { s.viewport.Start = max(0, s.viewport.Start+delta) })
	}
	scrollPage := func(direction int) {
		page := max(1, s.viewport.Window.End-s.viewport.Window.Start-1)
		scrollBy(direction * page)
	}
	built := make([]ui.Widget, 0, last-first+2)
	built = append(built, pickerOverflowRow{OnPressed: func(ui.EventContext) { scrollPage(-1) }})
	built = append(built, pickerOverflowRow{OnPressed: func(ui.EventContext) { scrollPage(1) }})
	for index := first; index < last; index++ {
		item := items[index]
		var activate ui.VoidCallback
		if w.OnActivate != nil {
			key := item.Key
			activate = func(ctx ui.EventContext) { w.OnActivate(ctx, key) }
		}
		var toggle ui.VoidCallback
		if w.OnToggle != nil && item.Disclosure != pickerDisclosureNone {
			key := item.Key
			toggle = func(ctx ui.EventContext) { w.OnToggle(ctx, key) }
		}
		built = append(built, pickerRow{Item: item, Columns: columns, Selected: index == selection, OnActive: activate, OnToggle: toggle})
	}
	return mouseActivator{
		OnScroll: func(_ ui.EventContext, mouse ui.Mouse) ui.EventResult {
			switch mouse.Button {
			case ui.MouseWheelUp:
				scrollBy(-1)
			case ui.MouseWheelDown:
				scrollBy(1)
			default:
				return ui.EventIgnored
			}
			return ui.EventHandled
		},
		Child: pickerViewport{
			Count: len(items), First: first, Reveal: reveal, State: &s.viewport,
			OnRowsChanged: rebuild, Children: built,
		},
	}
}

func pickerTitleRow(theme ui.Theme, title, meta string, tone pickerTone) ui.Widget {
	if title == "" {
		return nil
	}
	children := []ui.Widget{ui.Expanded(ui.Text{Value: title, Style: ui.Style{Foreground: theme.Foreground, Attribute: ui.AttrBold}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1})}
	if meta != "" {
		style := ui.Style{Foreground: theme.MutedForeground}
		// The spinner animates itself while mounted, so no caller ticks it.
		metaWidget := ui.Widget(ui.Text{Value: meta, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1})
		switch tone {
		case pickerToneDanger:
			style.Foreground = theme.DangerText
			metaWidget = ui.Text{Value: meta, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}
		case pickerToneLoading:
			metaWidget = spinner{Style: style, Label: meta}
		}
		children = append(children, ui.SizedBox{Width: pickerColumnGap}, metaWidget)
	}
	return ui.Flex{Axis: ui.Horizontal, Children: children}
}

func pickerMessage(theme ui.Theme, message string, tone pickerTone) ui.Widget {
	style := ui.Style{Foreground: theme.MutedForeground}
	switch tone {
	case pickerToneDanger:
		style.Foreground = theme.DangerText
	case pickerToneLoading:
		return ui.Align{Alignment: ui.TopLeft, Child: spinnerWithLabel(message, style)}
	}
	return ui.Text{Value: message, Style: style, SoftWrap: true}
}

// pickerFooter shows hints, or a status message followed by the final hint
// (normally how to close) so the status keeps the space it needs.
func pickerFooter(theme ui.Theme, hints, status string, tone pickerTone) ui.Widget {
	muted := ui.Style{Foreground: theme.MutedForeground}
	if status == "" {
		return ui.Text{Value: hints, Style: muted, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}
	}
	style := muted
	if tone == pickerToneDanger {
		style.Foreground = theme.DangerText
	}
	children := []ui.Widget{ui.Expanded(ui.Text{Value: status, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1})}
	if index := strings.LastIndex(hints, " "+glyphMiddleDot+" "); index >= 0 {
		hints = hints[index+len(" "+glyphMiddleDot+" "):]
	}
	if hints != "" {
		children = append(children, ui.SizedBox{Width: pickerColumnGap}, ui.Text{Value: hints, Style: muted, MaxLines: 1})
	}
	return ui.Flex{Axis: ui.Horizontal, Children: children}
}

// palettePickerPrompt is the canonical single-value form in the palette picker frame: a
// title, the shared input field and divider, then an optional hint and error.
type palettePickerPrompt struct {
	Title         string
	TitleMeta     string
	TitleMetaTone pickerTone
	Input         textInputConfig
	Hint          string
	Error         string
	Footer        string
}

func (w palettePickerPrompt) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	lines := []ui.Widget{}
	if w.Hint != "" {
		lines = append(lines, ui.Text{Value: w.Hint, Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true})
	}
	if w.Error != "" {
		lines = append(lines, ui.Text{Value: w.Error, Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true})
	}
	title := pickerTitleRow(theme, w.Title, w.TitleMeta, w.TitleMetaTone)
	if title == nil {
		title = ui.SizedBox{}
	}
	body := ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		pickerTitledSearchField(theme, title, w.Input),
		ui.Expanded(ui.Padding(ui.Insets{Right: pickerContentInset, Left: pickerContentInset}, ui.Flex{
			Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: lines,
		})),
	}}
	return pickerDialogPositioner{
		Percent: pickerWidthPercent, MinWidth: pickerMinWidth, MaxWidth: pickerMaxWidth, Height: pickerModalMinHeight,
		Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: pickerDialogContent(theme, body, pickerFooter(theme, w.Footer, "", pickerToneMuted))},
	}
}
