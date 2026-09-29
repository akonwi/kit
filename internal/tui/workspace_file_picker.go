package tui

import "go.rockorager.dev/vaxis/ui"

type workspaceFilePickerCallbacks struct {
	QueryChanged ui.TextChangedCallback
	Activate     func(ui.EventContext, workspaceFilePickerRow)
}

type workspaceFilePickerSurface struct {
	Controller workspaceFilePickerController
	Source     indexedFileSource
	Callbacks  workspaceFilePickerCallbacks
}

func (w workspaceFilePickerSurface) Build(ui.BuildContext) ui.Widget {
	items := w.Controller.pickerItems(w.Source, w.Controller.Query)
	catalog := w.Controller.pickerItems(w.Source, "")
	cursor := len(w.Controller.Query)
	result := picker{
		Title: "Open file",
		Search: &textInputConfig{
			Value: w.Controller.Query, Placeholder: "Search indexed project paths…", CursorOffset: &cursor,
			OnChanged: w.Callbacks.QueryChanged, AutoFocus: true,
		},
		Items: items, Catalog: catalog, Selection: workspaceFilePickerKeyString(w.Controller.Selection),
		Footer: "↑↓ move · enter open · ctrl+r refresh · esc close",
		OnActivate: func(ctx ui.EventContext, key string) {
			if row, ok := w.Controller.rowByPickerKey(w.Source, key); ok && w.Callbacks.Activate != nil {
				w.Callbacks.Activate(ctx, row)
			}
		},
	}
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
