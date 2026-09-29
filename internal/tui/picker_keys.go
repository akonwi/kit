package tui

import (
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

// pickerKeyModel is the canonical keyboard model for pickers. It owns the
// query and the highlighted item key, and decides what every key means, so no
// picker handles navigation, activation, dismissal, or query editing itself.
// It is the only route that edits a picker's query: the search field a
// palette picker shows is display-only.
//
// Pickers route keys to their model from the application's input owner rather
// than from the painted widget, so keys that arrive before the picker's first
// frame are applied exactly like later ones.
//
// Callers pass the unfiltered catalog; the model derives visible items with
// Filter, which must be the same filter the picker renders with.
type pickerKeyModel struct {
	Query     string
	Selection string
	// Filter derives the visible items; nil uses filterPickerItems.
	Filter pickerFilter
}

// pickerKeyResult reports what one key did to a picker.
type pickerKeyResult struct {
	// Handled is true when the key belongs to the picker, even if it changed
	// nothing; unhandled keys may be offered to other shortcuts.
	Handled bool
	// Dismiss asks the caller to close the picker.
	Dismiss bool
	// Activate asks the caller to act on Selection. DisabledReason explains
	// why the highlighted item cannot run so the caller can report it.
	Activate       bool
	DisabledReason string
	// QueryChanged reports that typing, deletion, or paste edited the query.
	QueryChanged bool
}

// Items returns the catalog items visible for the current query.
func (m pickerKeyModel) Items(catalog []pickerItem) []pickerItem {
	return m.itemsFor(m.Query, catalog)
}

func (m pickerKeyModel) itemsFor(query string, catalog []pickerItem) []pickerItem {
	return pickerFilterOrDefault(m.Filter)(query, catalog)
}

func pickerFilterOrDefault(filter pickerFilter) pickerFilter {
	if filter == nil {
		return filterPickerItems
	}
	return filter
}

// HandleKey applies one key. Up/Down move with wraparound, Enter activates the
// highlighted item, Escape dismisses, text and Backspace edit the query,
// Ctrl+Backspace deletes the previous word, and a paste is flattened into the
// query. Other unmodified keys are consumed so they cannot reach the
// background; other keys with modifiers are left unhandled.
func (m *pickerKeyModel) HandleKey(key ui.Key, catalog []pickerItem) pickerKeyResult {
	if key.EventType == ui.EventRelease {
		return pickerKeyResult{}
	}
	if key.EventType == vaxis.EventPaste {
		m.SetQuery(m.Query+palettePasteText(key), catalog)
		return pickerKeyResult{Handled: true, QueryChanged: true}
	}
	switch {
	case key.MatchString("Escape"):
		return pickerKeyResult{Handled: true, Dismiss: true}
	case key.MatchString("Up"):
		m.Move(catalog, -1)
		return pickerKeyResult{Handled: true}
	case key.MatchString("Down"):
		m.Move(catalog, 1)
		return pickerKeyResult{Handled: true}
	case key.MatchString("Enter"):
		result := pickerKeyResult{Handled: true, Activate: true}
		if item, ok := pickerItemByKey(m.Items(catalog), m.Selection); ok {
			result.DisabledReason = item.DisabledReason
		}
		return result
	case key.MatchString("Ctrl+Backspace"):
		if m.Query != "" {
			m.SetQuery(deletePickerWordBackward(m.Query), catalog)
			return pickerKeyResult{Handled: true, QueryChanged: true}
		}
		return pickerKeyResult{Handled: true}
	}
	query := m.Query
	switch {
	case key.Keycode == vaxis.KeyBackspace && key.Modifiers&^(vaxis.ModShift|vaxis.ModAlt|vaxis.ModCapsLock|vaxis.ModNumLock) == 0:
		// Alt+Backspace deletes one character, as it does in a text field.
		runes := []rune(query)
		if len(runes) == 0 {
			return pickerKeyResult{Handled: true}
		}
		query = string(runes[:len(runes)-1])
	case key.Modifiers&^(vaxis.ModShift|vaxis.ModCapsLock|vaxis.ModNumLock) != 0:
		return pickerKeyResult{}
	case key.Text != "":
		query += key.Text
	default:
		return pickerKeyResult{Handled: true}
	}
	m.SetQuery(query, catalog)
	return pickerKeyResult{Handled: true, QueryChanged: true}
}

// SetQuery replaces the query and highlights the first enabled match, or the
// first match when every match is disabled.
func (m *pickerKeyModel) SetQuery(query string, catalog []pickerItem) {
	m.Query = query
	m.Selection = firstEnabledPickerKey(m.Items(catalog))
}

// Move highlights the visible item delta rows away, wrapping at both ends.
// Disabled items can be highlighted so their reason can be read and reported.
func (m *pickerKeyModel) Move(catalog []pickerItem, delta int) {
	items := m.Items(catalog)
	if len(items) == 0 || delta == 0 {
		return
	}
	index := pickerItemIndex(items, m.Selection)
	if index < 0 {
		m.Selection = items[0].Key
		return
	}
	index = (index + delta) % len(items)
	if index < 0 {
		index += len(items)
	}
	m.Selection = items[index].Key
}

// deletePickerWordBackward removes the word before the end of query with the
// text buffer's word boundaries: trailing spaces, then one run of word or
// punctuation characters.
func deletePickerWordBackward(query string) string {
	buffer := ui.NewTextBuffer(query)
	buffer.SetCursorOffset(buffer.Len())
	buffer.DeleteWordBackward()
	return buffer.Text()
}

func firstEnabledPickerKey(items []pickerItem) string {
	for _, item := range items {
		if item.DisabledReason == "" {
			return item.Key
		}
	}
	if len(items) > 0 {
		return items[0].Key
	}
	return ""
}

func pickerItemIndex(items []pickerItem, key string) int {
	for index, item := range items {
		if item.Key == key {
			return index
		}
	}
	return -1
}

func pickerItemByKey(items []pickerItem, key string) (pickerItem, bool) {
	if index := pickerItemIndex(items, key); index >= 0 {
		return items[index], true
	}
	return pickerItem{}, false
}

// pickerKeyListener gives pane-owned pickers the same raw-key route used by
// app-owned pickers. It is intentionally generic: meaning remains exclusively
// in pickerKeyModel and the owning controller.
type pickerKeyListener struct {
	OnKey func(ui.Key) ui.EventResult
	Child ui.Widget
}

func (w pickerKeyListener) WidgetChild() ui.Widget { return w.Child }

func (w pickerKeyListener) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderPickerKeyListener{OnKey: w.OnKey}
}

func (w pickerKeyListener) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	renderObject.(*renderPickerKeyListener).OnKey = w.OnKey
}

type renderPickerKeyListener struct {
	ui.SingleChildRenderObject
	OnKey func(ui.Key) ui.EventResult
}

func (r *renderPickerKeyListener) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
		return
	}
	r.SetSize(constraints.Constrain(ui.Size{}))
}

func (r *renderPickerKeyListener) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderPickerKeyListener) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderPickerKeyListener) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderPickerKeyListener) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() != ui.CapturePhase || r.OnKey == nil {
		return ui.EventIgnored
	}
	key, ok := event.(ui.Key)
	if !ok {
		return ui.EventIgnored
	}
	return r.OnKey(key)
}
