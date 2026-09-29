package tui

import (
	"fmt"

	"go.rockorager.dev/vaxis/ui"
)

type themePickerCallbacks struct {
	Select func(ui.EventContext, string)
}

type themePickerSurface struct {
	Snapshot  themePickerSnapshot
	Callbacks themePickerCallbacks
}

// Build maps themes onto the canonical picker while keeping discovery in the
// list message slot and preview feedback in the fixed footer status slot.
func (w themePickerSurface) Build(ui.BuildContext) ui.Widget {
	snapshot := w.Snapshot
	result := palettePicker{
		Title: "Theme", Query: snapshot.Query, Search: &pickerSearch{Placeholder: "Search themes…"},
		Catalog: themePickerItems(snapshot.Names, snapshot.CommittedName), Selection: snapshot.Selection,
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
