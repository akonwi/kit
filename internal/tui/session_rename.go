package tui

import "go.rockorager.dev/vaxis/ui"

type sessionRenameCallbacks struct {
	Changed   ui.TextChangedCallback
	Submitted ui.TextChangedCallback
}

type sessionRenameSurface struct {
	Snapshot  sessionExplorerSnapshot
	Callbacks sessionRenameCallbacks
}

func (w sessionRenameSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	fieldTheme := theme
	fieldTheme.Surface = theme.Background
	fieldTheme.SurfaceHovered = theme.Background
	targetLabel := shortSessionID(w.Snapshot.RenameSessionID)
	for _, session := range w.Snapshot.Sessions {
		if session.ID == w.Snapshot.RenameSessionID {
			targetLabel = sessionExplorerItemLabel(session)
			break
		}
	}
	cursorOffset := len((ui.LayoutContext{}).Characters(w.Snapshot.RenameText))
	field := ui.Widget(textInput(fieldTheme, textInputConfig{
		Value: w.Snapshot.RenameText, Placeholder: "Enter new session name…",
		OnChanged: w.Callbacks.Changed, OnSubmitted: w.Callbacks.Submitted,
		InitialCursorOffset: &cursorOffset, InitialCursorGeneration: w.Snapshot.RenameCursorEnd,
		AutoFocus: true,
	}))
	if w.Snapshot.RenamePending {
		field = ui.Expanded(ui.Text{
			Value: w.Snapshot.RenameText, Style: ui.Style{Foreground: theme.MutedForeground},
			Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		})
	}
	children := []ui.Widget{
		ui.Text{Value: targetLabel, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		ui.SizedBox{Height: 1},
	}
	if w.Snapshot.RenameError != "" {
		children = append(children,
			ui.Text{Value: "Rename failed: " + w.Snapshot.RenameError, Style: ui.Style{Foreground: theme.DangerText}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
			ui.SizedBox{Height: 1},
		)
	}
	children = append(children, ui.DecoratedBox(
		ui.Decoration{
			Style:  ui.Style{Background: theme.Background},
			Border: ui.BorderAll(ui.Style{Foreground: theme.PrimaryText}),
		},
		ui.Padding(ui.Insets{Top: 1, Right: 1, Bottom: 1, Left: 1}, ui.Flex{
			Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMax, Children: []ui.Widget{field},
		}),
	))
	body := ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch,
		Children: children,
	}
	var footer ui.Widget = ui.Text{
		Value: "enter save · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground},
		Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
	}
	if w.Snapshot.RenamePending {
		footer = spinnerWithLabel("Saving…", ui.Style{Foreground: theme.MutedForeground})
	}
	return dialogSurface(theme, "Rename session", "", body, footer, true)
}
