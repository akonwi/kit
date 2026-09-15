package tui

import (
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

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
	Scroll     *ui.ScrollController
	Callbacks  workspaceFilePickerCallbacks
}

func (w workspaceFilePickerSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	presentation := resolvePickerRowPresentation(ctx, theme)
	rows := w.Controller.rows()
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
			rowTheme.Primary = theme.Background
			rowTheme.PrimaryHovered = theme.Background
		}
		prefix, label := workspaceFilePickerRowText(w.Controller, row)
		child := ui.Provider[ui.Theme]{Value: rowTheme, Child: ui.ListTile{
			Selected: selected, Disabled: !workspaceFilePickerRowSelectable(row), MinHeight: 1, Padding: ui.Insets{Left: 1, Right: 1},
			OnPressed: func(ctx ui.EventContext) {
				if w.Callbacks.Select != nil {
					w.Callbacks.Select(ctx, row)
				}
				if w.Callbacks.Activate != nil {
					w.Callbacks.Activate(ctx, row)
				}
			},
			Title: ui.Text{Value: strings.Repeat("  ", row.Depth) + prefix + label, Style: ui.Style{Foreground: foreground, Background: background}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis},
		}}
		rowWidgets = append(rowWidgets, child)
	}
	if len(rowWidgets) == 0 && w.Controller.Workspace.WorkspaceID != "" {
		rowWidgets = append(rowWidgets, ui.Text{Value: "No matching files", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	}
	if w.Controller.Workspace.WorkspaceID == "" && len(rowWidgets) == 0 {
		rowWidgets = append(rowWidgets, ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			spinner{Style: ui.Style{Foreground: theme.MutedForeground}}, ui.SizedBox{Width: 1},
			ui.Text{Value: "Loading workspace…", Style: ui.Style{Foreground: theme.MutedForeground}},
		}})
	}

	fieldTheme := theme
	fieldTheme.Surface, fieldTheme.SurfaceHovered = theme.Background, theme.Background
	cursor := len(w.Controller.Query)
	query := ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
		ui.Text{Value: ">", Style: ui.Style{Foreground: theme.Foreground}}, ui.SizedBox{Width: 1},
		textInput(fieldTheme, textInputConfig{
			Value: w.Controller.Query, Placeholder: "Filter workspace paths…", CursorOffset: &cursor, AutoFocus: true,
			OnChanged: w.Callbacks.QueryChanged,
			OnSubmitted: func(ctx ui.EventContext, _ string) {
				if len(rows) > 0 && workspaceFilePickerRowSelectable(rows[selection]) && w.Callbacks.Activate != nil {
					w.Callbacks.Activate(ctx, rows[selection])
				}
			},
		}),
	}}
	title := "Open file"
	if cwd := strings.TrimSpace(w.Controller.Workspace.CWD); cwd != "" {
		title += "  " + cwd
	}
	body := ui.Padding(ui.Insets{Top: 1, Right: 2, Left: 2}, ui.Flex{
		Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			ui.Text{Value: title, Style: ui.Style{Foreground: theme.Foreground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis},
			ui.SizedBox{Height: 1}, query, ui.SizedBox{Height: 1},
			ui.Expanded(ui.ScrollView{Controller: w.Scroll, Child: ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rowWidgets}}),
		},
	})
	footer := ui.Text{Value: "↑↓ move · enter open/expand · ctrl+r refresh · esc close", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}
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

func workspaceFilePickerRowText(controller workspaceFilePickerController, row workspaceFilePickerRow) (string, string) {
	switch row.Kind {
	case workspaceFilePickerEntryRow:
		switch row.Entry.Kind {
		case protocol.WorkspaceEntryDirectory:
			if controller.expanded[row.Key] {
				return glyphTriangleDown + " ", row.Entry.Name + "/"
			}
			return glyphTriangleRight + " ", row.Entry.Name + "/"
		case protocol.WorkspaceEntryFile:
			return "  ", row.Entry.Name
		case protocol.WorkspaceEntrySymlink:
			return "  ", row.Entry.Name + "  symlink"
		default:
			return "  ", row.Entry.Name + "  unsupported"
		}
	case workspaceFilePickerLoadingRow:
		return spinnerFrames[0] + " ", row.Text
	case workspaceFilePickerMoreRow:
		return "  ", row.Text
	case workspaceFilePickerErrorRow:
		return glyphCross + " ", row.Text + " · enter retry"
	case workspaceFilePickerTruncatedRow:
		return glyphTriangleUp + " ", row.Text
	default:
		return "  ", row.Text
	}
}
