package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

// pickerItem is one picker row. Every row is a single line in uniform columns:
// label, optional hint, optional description, and optional trailing metadata.
// Current marks the active value; DisabledReason makes the row unavailable.
//
// Hierarchical pickers set Depth to indent the label two cells per level, and
// Disclosure with ChildCount to show "▸ N" or "▾ N" at the start of the hint
// column, followed by Hint when both are present.
type pickerItem struct {
	Key            string
	Label          string
	Hint           string
	Description    string
	Meta           string
	Current        bool
	DisabledReason string
	Depth          int
	Disclosure     pickerDisclosure
	ChildCount     int
}

// pickerDisclosure is the expand state of an item that has children.
type pickerDisclosure uint8

const (
	pickerDisclosureNone pickerDisclosure = iota
	pickerDisclosureCollapsed
	pickerDisclosureExpanded
)

// pickerTone colors picker messages and footer status.
type pickerTone uint8

const (
	pickerToneMuted pickerTone = iota
	pickerToneDanger
	pickerToneLoading
)

// picker is the canonical modal picker. Its fields are the whole contract:
// callers describe the title, search, items, and footer; the picker owns the
// frame, alignment, highlight, overflow rows, and pointer activation.
type picker struct {
	Title     string
	TitleMeta string
	// TitleMetaTone colors the title metadata; pickerToneLoading shows it
	// after the shared spinner, for example while an activation is pending.
	TitleMetaTone pickerTone
	// Search adds the shared search field when non-nil.
	Search *textInputConfig
	// Items are the visible rows in order.
	Items []pickerItem
	// Catalog sizes the columns. It defaults to Items; pass the unfiltered
	// catalog so filtering never shifts columns.
	Catalog   []pickerItem
	Selection string
	// Message replaces the list for loading, empty, and error states. When
	// empty and there are no items, the list reads "No results".
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

func (picker) CreateState() ui.State { return &pickerState{} }

type pickerState struct {
	ui.StateBase
	viewport      pickerViewportState
	lastSelection string
	hasSelection  bool
}

func (s *pickerState) Build(ctx ui.BuildContext) ui.Widget {
	w := s.Widget().(picker)
	theme := ui.MustDepend[ui.Theme](ctx)
	children := make([]ui.Widget, 0, 3)
	title := pickerTitleRow(theme, w.Title, w.TitleMeta, w.TitleMetaTone)
	switch {
	case title != nil && w.Search != nil:
		children = append(children, pickerTitledSearchField(theme, title, *w.Search))
	case title != nil:
		children = append(children, ui.Padding(ui.Insets{Top: 1, Right: pickerContentInset, Bottom: 1, Left: pickerContentInset}, title))
	case w.Search != nil:
		children = append(children, ui.Padding(ui.Insets{Top: 1}, pickerSearchField(theme, *w.Search)))
	default:
		children = append(children, ui.SizedBox{Height: 1})
	}
	children = append(children, ui.Expanded(ui.Padding(ui.Insets{Right: 1, Left: 1}, s.list(theme, w))))
	content := ui.Widget(pickerDialogContent(theme, ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}, pickerFooter(theme, w.Footer, w.Status, w.StatusTone)))
	if w.OnKey != nil {
		content = pickerKeyListener{OnKey: w.OnKey, Child: content}
	}
	return pickerDialogPositioner{
		Percent: pickerWidthPercent, MinWidth: pickerMinWidth, MaxWidth: pickerMaxWidth, Height: pickerModalMinHeight,
		Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: content},
	}
}

// Every picker currently shares one size.
const (
	pickerWidthPercent = 80
	pickerMinWidth     = 48
	pickerMaxWidth     = 96
)

func (s *pickerState) list(theme ui.Theme, w picker) ui.Widget {
	if w.Message != "" || len(w.Items) == 0 {
		message := w.Message
		if message == "" {
			message = "No results"
		}
		return ui.Padding(ui.Insets{Left: 1}, pickerMessage(theme, message, w.MessageTone))
	}
	selection := -1
	for index, item := range w.Items {
		if item.Key == w.Selection {
			selection = index
			break
		}
	}
	reveal := -1
	if !s.hasSelection || w.Selection != s.lastSelection {
		reveal = selection
		s.lastSelection, s.hasSelection = w.Selection, true
	}
	// Build rows for the window this height produced last frame; the viewport
	// resolves the real height and a changed height rebuilds the run.
	rows := s.viewport.Rows
	if rows <= 0 {
		rows = pickerModalMinHeight
	}
	window := resolveListWindow(len(w.Items), rows, s.viewport.Start, reveal)
	first := window.Start
	last := min(len(w.Items), first+rows)
	catalog := w.Catalog
	if catalog == nil {
		catalog = w.Items
	}
	columns := measurePickerColumns(catalog)
	built := make([]ui.Widget, 0, last-first+2)
	built = append(built, pickerOverflowRow{OnPressed: func(ui.EventContext) { s.scrollPage(-1) }})
	built = append(built, pickerOverflowRow{OnPressed: func(ui.EventContext) { s.scrollPage(1) }})
	for index := first; index < last; index++ {
		item := w.Items[index]
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
				s.scrollBy(-1)
			case ui.MouseWheelDown:
				s.scrollBy(1)
			default:
				return ui.EventIgnored
			}
			return ui.EventHandled
		},
		Child: pickerViewport{
			Count: len(w.Items), First: first, Reveal: reveal, State: &s.viewport,
			OnRowsChanged: s.MarkNeedsBuild, Children: built,
		},
	}
}

func (s *pickerState) scrollPage(direction int) {
	page := max(1, s.viewport.Window.End-s.viewport.Window.Start-1)
	s.scrollBy(direction * page)
}

func (s *pickerState) scrollBy(delta int) {
	s.SetState(func() { s.viewport.Start = max(0, s.viewport.Start+delta) })
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

// pickerPrompt is the canonical single-value form in the picker frame: a
// title, the shared input field and divider, then an optional hint and error.
type pickerPrompt struct {
	Title         string
	TitleMeta     string
	TitleMetaTone pickerTone
	Input         textInputConfig
	Hint          string
	Error         string
	Footer        string
}

func (w pickerPrompt) Build(ctx ui.BuildContext) ui.Widget {
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
