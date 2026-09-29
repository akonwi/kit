package tui

import (
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

// pickerKeyModel is the canonical keyboard model for pickers. It owns the
// query and the highlighted item key, and decides what every key means, so no
// picker handles navigation, activation, dismissal, or query editing itself.
//
// Pickers route keys to their model from the application's input owner rather
// than from the painted widget, so keys that arrive before the picker's first
// frame are applied exactly like later ones.
type pickerKeyModel struct {
	Query     string
	Selection string
}

// pickerItemsFunc returns a picker's visible items for a query.
type pickerItemsFunc func(query string) []pickerItem

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

// HandleKey applies one key. Up/Down move with wraparound, Enter activates the
// highlighted item, Escape dismisses, text and Backspace edit the query, and a
// paste is flattened into the query. Other unmodified keys are consumed so
// they cannot reach the background; keys with modifiers are left unhandled.
func (m *pickerKeyModel) HandleKey(key ui.Key, items pickerItemsFunc) pickerKeyResult {
	if key.EventType == ui.EventRelease {
		return pickerKeyResult{}
	}
	if key.EventType == vaxis.EventPaste {
		m.SetQuery(m.Query+palettePasteText(key), items(m.Query+palettePasteText(key)))
		return pickerKeyResult{Handled: true, QueryChanged: true}
	}
	switch {
	case key.MatchString("Escape"):
		return pickerKeyResult{Handled: true, Dismiss: true}
	case key.MatchString("Up"):
		m.Move(items(m.Query), -1)
		return pickerKeyResult{Handled: true}
	case key.MatchString("Down"):
		m.Move(items(m.Query), 1)
		return pickerKeyResult{Handled: true}
	case key.MatchString("Enter"):
		result := pickerKeyResult{Handled: true, Activate: true}
		if item, ok := pickerItemByKey(items(m.Query), m.Selection); ok {
			result.DisabledReason = item.DisabledReason
		}
		return result
	}
	if key.Modifiers&^(vaxis.ModShift|vaxis.ModCapsLock|vaxis.ModNumLock) != 0 {
		return pickerKeyResult{}
	}
	query := m.Query
	switch {
	case key.MatchString("Backspace"):
		runes := []rune(query)
		if len(runes) == 0 {
			return pickerKeyResult{Handled: true}
		}
		query = string(runes[:len(runes)-1])
	case key.Text != "":
		query += key.Text
	default:
		return pickerKeyResult{Handled: true}
	}
	m.SetQuery(query, items(query))
	return pickerKeyResult{Handled: true, QueryChanged: true}
}

// SetQuery replaces the query and highlights the first enabled match, or the
// first match when every match is disabled.
func (m *pickerKeyModel) SetQuery(query string, items []pickerItem) {
	m.Query = query
	m.Selection = firstEnabledPickerKey(items)
}

// Move highlights the item delta rows away, wrapping at both ends. Disabled
// items can be highlighted so their reason can be read and reported.
func (m *pickerKeyModel) Move(items []pickerItem, delta int) {
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
