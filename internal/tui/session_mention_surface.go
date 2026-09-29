package tui

import (
	"time"

	"go.rockorager.dev/vaxis/ui"
)

// sessionMentionSurface maps the session mention and its session list onto
// the inline picker.
type sessionMentionSurface struct {
	Controller sessionMentionController
	Source     sessionMentionSource
	Anchor     func(ui.Size) ui.Point
	OnSelect   func(ui.EventContext, string)
}

func (w sessionMentionSurface) Build(ui.BuildContext) ui.Widget {
	catalog := sessionMentionCatalog(w.Source.Entries, time.Now())
	model := w.Controller.keys()
	if items := model.Items(catalog); pickerItemIndex(items, model.Selection) < 0 {
		model.Selection = firstEnabledPickerKey(items)
	}
	picker := inlinePicker{
		Query: model.Query, Catalog: catalog, Selection: model.Selection,
		Footer: "↑↓ move · enter insert · esc close", OnActivate: w.OnSelect,
		Anchor: w.Anchor,
	}
	if len(catalog) == 0 {
		switch {
		case w.Source.Loading:
			picker.Message, picker.MessageTone = "Loading sessions…", pickerToneLoading
		case w.Source.Error != "":
			picker.Message, picker.MessageTone = "Could not load sessions: "+w.Source.Error, pickerToneDanger
		default:
			picker.Message = "No sessions found"
		}
	}
	return picker
}
