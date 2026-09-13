package tui

import (
	"fmt"

	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis/ui"
)

type themePickerCallbacks struct {
	Select func(ui.EventContext, int)
}

type themePickerSurface struct {
	Snapshot  themePickerSnapshot
	Callbacks themePickerCallbacks
}

func (w themePickerSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	rowPresentation := resolvePickerRowPresentation(ctx, theme)
	const visibleRows = 9
	start := 0
	if w.Snapshot.Selection >= visibleRows {
		start = w.Snapshot.Selection - visibleRows + 1
	}
	end := min(len(w.Snapshot.Names), start+visibleRows)
	rows := make([]ui.Widget, 0, end-start)
	for index := start; index < end; index++ {
		name := w.Snapshot.Names[index]
		index, name := index, name
		label := name
		if name == kittheme.SystemName {
			label = "System (terminal colors)"
		}
		selected := index == w.Snapshot.Selection
		style := ui.Style{Foreground: rowPresentation.ItemText, Background: theme.Background}
		if selected {
			style = ui.Style{Foreground: rowPresentation.FocusedText, Background: rowPresentation.FocusedBg}
		}
		rows = append(rows, ui.Provider[ui.Theme]{Value: rowPresentation.Theme, Child: ui.ListTile{
			Title:    ui.Text{Value: label, Style: style, MaxLines: 1, Overflow: ui.TextOverflowEllipsis},
			Selected: selected,
			OnPressed: func(event ui.EventContext) {
				if w.Callbacks.Select != nil {
					w.Callbacks.Select(event, index)
				}
			},
			Padding: ui.Insets{Right: 1}, MinHeight: 1,
		}})
	}
	if len(rows) == 0 {
		message := "No themes available"
		if w.Snapshot.Loading {
			message = "Loading themes…"
		}
		rows = append(rows, ui.Text{Value: message, Style: ui.Style{Foreground: theme.MutedForeground}})
	}
	messages := make([]ui.Widget, 0, 3)
	if w.Snapshot.Pending {
		messages = append(messages, ui.Text{Value: "Saving theme…", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	} else if w.Snapshot.Loading && len(w.Snapshot.Names) > 0 {
		messages = append(messages, ui.Text{Value: "Loading preview…", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	}
	if w.Snapshot.Error != "" {
		messages = append(messages, ui.Text{Value: w.Snapshot.Error, Style: ui.Style{Foreground: theme.DangerText}, MaxLines: 2, Overflow: ui.TextOverflowEllipsis})
	}
	if count := len(w.Snapshot.Diagnostics); count > 0 {
		messages = append(messages, ui.Text{Value: fmt.Sprintf("%d theme value(s) were ignored", count), Style: ui.Style{Foreground: theme.WarningText}, MaxLines: 1})
	}
	bodyChildren := []ui.Widget{ui.Text{Value: "Theme", Style: ui.Style{Foreground: theme.Foreground, Attribute: ui.AttrBold}}, ui.SizedBox{Height: 1}, ui.Expanded(ui.ScrollView{Child: ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows}})}
	bodyChildren = append(bodyChildren, messages...)
	body := ui.Padding(ui.Insets{Top: 1, Right: 2, Left: 2}, ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: bodyChildren})
	footerText := "↑↓ preview · enter use · esc cancel"
	if w.Snapshot.Loading {
		footerText = "Loading… · esc cancel"
	}
	if w.Snapshot.Pending {
		footerText = "Saving… · ctrl+c force quit"
	}
	footer := ui.Text{Value: footerText, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}
	return pickerDialogPositioner{Percent: 60, MinWidth: 36, MaxWidth: 72, Height: pickerModalMinHeight,
		Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: pickerDialogContent(theme, body, footer)}}
}
