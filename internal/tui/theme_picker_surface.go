package tui

import (
	"fmt"

	"go.rockorager.dev/vaxis/ui"
)

type themePickerCallbacks struct {
	QueryChanged ui.TextChangedCallback
	Select       func(ui.EventContext, string)
}

type themePickerSurface struct {
	Snapshot  themePickerSnapshot
	Callbacks themePickerCallbacks
}

// Build maps themes onto the canonical picker while keeping discovery in the
// list message slot and preview feedback in the fixed footer status slot.
func (w themePickerSurface) Build(ui.BuildContext) ui.Widget {
	snapshot := w.Snapshot
	catalog := themePickerItems(snapshot.Names, snapshot.CommittedName)
	items := themePickerItems(filterThemeNames(snapshot.Query, snapshot.Names), snapshot.CommittedName)
	cursor := len(snapshot.Query)
	result := palettePicker{
		Title: "Theme",
		Search: &textInputConfig{
			Value: snapshot.Query, Placeholder: "Search themes…", CursorOffset: &cursor,
			OnChanged: w.Callbacks.QueryChanged, AutoFocus: true,
		},
		Items: items, Catalog: catalog, Selection: snapshot.Selection,
		Footer: "↑↓ preview · enter use · esc cancel",
		OnActivate: func(ctx ui.EventContext, key string) {
			if w.Callbacks.Select != nil {
				w.Callbacks.Select(ctx, key)
			}
		},
	}
	switch {
	case snapshot.Loading:
		result.Message, result.MessageTone = "Loading…", pickerToneLoading
		result.Footer = "Loading… · esc cancel"
	case snapshot.Error != "" && len(snapshot.Names) == 0:
		result.Message, result.MessageTone = snapshot.Error, pickerToneDanger
	case len(snapshot.Names) == 0:
		result.Message = "No themes"
	}
	switch {
	case snapshot.Pending:
		result.Footer = "Saving… · ctrl+c force quit"
	case snapshot.Error != "" && len(snapshot.Names) > 0:
		result.Status, result.StatusTone = snapshot.Error, pickerToneDanger
	case snapshot.PreviewLoading:
		result.Status, result.StatusTone = "Loading preview…", pickerToneLoading
	case len(snapshot.Diagnostics) > 0:
		result.Status = fmt.Sprintf("%d theme value(s) were ignored", len(snapshot.Diagnostics))
	}
	return result
}
