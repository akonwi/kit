package tui

import "go.rockorager.dev/vaxis/ui"

type moveWorkspaceFilePickerIntent struct{ Delta int }

func (moveWorkspaceFilePickerIntent) IntentType() ui.IntentType { return "kit.workspace-files.move" }

type refreshWorkspaceFilePickerIntent struct{}

func (refreshWorkspaceFilePickerIntent) IntentType() ui.IntentType {
	return "kit.workspace-files.refresh"
}

type workspaceFilePickerCallbacks struct {
	QueryChanged ui.TextChangedCallback
	Move         func(ui.EventContext, int)
	Activate     func(ui.EventContext, workspaceFilePickerRow)
	Select       func(ui.EventContext, workspaceFilePickerRow)
	Refresh      ui.VoidCallback
	Close        ui.VoidCallback
}

type workspaceFilePickerSurface struct {
	Controller workspaceFilePickerController
	Source     indexedFileSource
	Scroll     *ui.ScrollController
	Callbacks  workspaceFilePickerCallbacks
}

func (w workspaceFilePickerSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	presentation := resolvePickerRowPresentation(ctx, theme)
	rows := w.Controller.rows(w.Source)
	selection := w.Controller.selectedIndex(rows)
	rowWidgets := make([]ui.Widget, 0, len(rows))
	for index, row := range rows {
		index, row := index, row
		selected := index == selection && workspaceFilePickerRowSelectable(row)
		foreground, background := presentation.ItemText, theme.Background
		rowTheme := presentation.Theme
		if selected {
			foreground, background = presentation.FocusedText, presentation.FocusedBg
		}
		if !workspaceFilePickerRowSelectable(row) {
			foreground = theme.MutedForeground
			if row.Kind == workspaceFilePickerTruncatedRow {
				foreground = theme.Warning
			}
			rowTheme.Primary, rowTheme.PrimaryHovered = theme.Background, theme.Background
		}
		label := workspaceFilePickerRowText(row)
		rowWidgets = append(rowWidgets, ui.Provider[ui.Theme]{Value: rowTheme, Child: ui.ListTile{
			Selected: selected, Disabled: !workspaceFilePickerRowSelectable(row), MinHeight: 1, Padding: ui.Insets{Left: 1, Right: 1},
			OnPressed: func(ctx ui.EventContext) {
				if w.Callbacks.Select != nil {
					w.Callbacks.Select(ctx, row)
				}
				if w.Callbacks.Activate != nil {
					w.Callbacks.Activate(ctx, row)
				}
			},
			Title: ui.Text{Value: label, Style: ui.Style{Foreground: foreground, Background: background}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis},
		}})
	}
	fieldTheme := theme
	fieldTheme.Surface, fieldTheme.SurfaceHovered = theme.Background, theme.Background
	cursor := len(w.Controller.Query)
	query := ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
		ui.Text{Value: ">", Style: ui.Style{Foreground: theme.Foreground}}, ui.SizedBox{Width: 1},
		textInput(fieldTheme, textInputConfig{
			Value: w.Controller.Query, Placeholder: "Search indexed project paths…", CursorOffset: &cursor, AutoFocus: true,
			OnChanged: w.Callbacks.QueryChanged,
			OnSubmitted: func(ctx ui.EventContext, _ string) {
				if len(rows) > 0 && workspaceFilePickerRowSelectable(rows[selection]) && w.Callbacks.Activate != nil {
					w.Callbacks.Activate(ctx, rows[selection])
				}
			},
		}),
	}}
	title := "Open file"
	body := ui.Padding(ui.Insets{Top: 1, Right: 2, Left: 2}, ui.Flex{
		Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			ui.Text{Value: title, Style: ui.Style{Foreground: theme.Foreground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis},
			ui.SizedBox{Height: 1}, query, ui.SizedBox{Height: 1},
			ui.Expanded(ui.ScrollView{Controller: w.Scroll, Child: ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rowWidgets}}),
		},
	})
	footer := ui.Text{Value: "↑↓ move · enter open · ctrl+r refresh · esc close", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}
	content := pickerDialogContent(theme, body, footer)
	actions := map[ui.IntentType]ui.ActionFunc{
		moveWorkspaceFilePickerIntent{}.IntentType(): func(ctx ui.EventContext, intent ui.Intent) ui.EventResult {
			if w.Callbacks.Move != nil {
				w.Callbacks.Move(ctx, intent.(moveWorkspaceFilePickerIntent).Delta)
			}
			return ui.EventHandled
		},
		refreshWorkspaceFilePickerIntent{}.IntentType(): func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.Refresh != nil {
				w.Callbacks.Refresh(ctx)
			}
			return ui.EventHandled
		},
		ui.DismissIntentType: func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.Close != nil {
				w.Callbacks.Close(ctx)
			}
			return ui.EventHandled
		},
	}
	content = ui.Actions{Bindings: actions, Child: keyShortcuts{Bindings: ui.ShortcutMap{
		"Up": moveWorkspaceFilePickerIntent{Delta: -1}, "Down": moveWorkspaceFilePickerIntent{Delta: 1}, "Ctrl+r": refreshWorkspaceFilePickerIntent{},
	}, Child: content}}
	return pickerDialogPositioner{Percent: 80, MinWidth: 48, MaxWidth: 110, Height: pickerModalMinHeight,
		Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: content}}
}

func workspaceFilePickerRowText(row workspaceFilePickerRow) string {
	switch row.Kind {
	case workspaceFilePickerEntryRow:
		if row.Entry.IsDir {
			return row.Entry.Path + "  directory"
		}
		return row.Entry.Path
	case workspaceFilePickerLoadingRow:
		return spinnerFrames[0] + " " + row.Text
	case workspaceFilePickerErrorRow:
		return glyphCross + " " + row.Text
	case workspaceFilePickerTruncatedRow:
		return glyphTriangleUp + " " + row.Text
	default:
		return row.Text
	}
}
