package tui

import "go.rockorager.dev/vaxis/ui"

type workspaceFilePickerCallbacks struct {
	Activate func(ui.EventContext, workspaceFilePickerRow)
}

type workspaceFilePickerSurface struct {
	Controller workspaceFilePickerController
	Source     indexedFileSource
	Callbacks  workspaceFilePickerCallbacks
}

func (w workspaceFilePickerSurface) Build(ui.BuildContext) ui.Widget {
	model := w.Controller.keyModel()
	result := palettePicker{
		Title: "Open file",
		Query: model.Query, Search: &pickerSearch{Placeholder: "Search indexed project paths…"},
		Catalog: w.Controller.pickerCatalog(w.Source), Filter: model.Filter, Selection: model.Selection,
		Footer: "↑↓ move · enter open · ctrl+r refresh · esc close",
		OnActivate: func(ctx ui.EventContext, key string) {
			if row, ok := w.Controller.rowByPickerKey(w.Source, key); ok && w.Callbacks.Activate != nil {
				w.Callbacks.Activate(ctx, row)
			}
		},
	}
	items := result.items()
	if w.Source.Truncated && w.Controller.Workspace.WorkspaceID != "" {
		result.TitleMeta = "Showing first 4,000 indexed paths"
	}
	switch {
	case w.Controller.WorkspaceError != "":
		result.Message, result.MessageTone = w.Controller.WorkspaceError, pickerToneDanger
	case w.Controller.Workspace.WorkspaceID == "":
		result.Message, result.MessageTone = "Loading workspace…", pickerToneLoading
	case w.Source.Error != "" && len(w.Source.Entries) == 0:
		result.Message, result.MessageTone = w.Source.Error, pickerToneDanger
	case len(items) == 0 && w.Source.Loading:
		result.Message, result.MessageTone = "Loading indexed files…", pickerToneLoading
	case len(items) == 0 && w.Controller.Query != "":
		result.Message = "No matching files"
	case len(items) == 0:
		result.Message = "No indexed files"
	}
	if len(items) > 0 {
		switch {
		case w.Source.Loading:
			result.Status, result.StatusTone = "Refreshing indexed files…", pickerToneLoading
		case w.Source.Error != "":
			result.Status, result.StatusTone = w.Source.Error, pickerToneDanger
		}
	}
	return result
}
