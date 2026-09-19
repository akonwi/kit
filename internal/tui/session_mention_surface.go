package tui

import "go.rockorager.dev/vaxis/ui"

const sessionMentionMaxVisible = 10

type sessionMentionSurface struct {
	Controller     *sessionMentionController
	Source         sessionMentionSource
	Composer       string
	BottomInset    int
	PrimaryPercent int
	OnSelect       func(ui.EventContext, string)
}

func (w sessionMentionSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	presentation := resolvePickerRowPresentation(ctx, theme)
	entries := w.Controller.filtered(w.Source.Entries)
	selected := 0
	for i, entry := range entries {
		if entry.ID == w.Controller.Selection {
			selected = i
			break
		}
	}
	if len(entries) > sessionMentionMaxVisible {
		offset := max(0, min(selected-sessionMentionMaxVisible/2, len(entries)-sessionMentionMaxVisible))
		entries = entries[offset : offset+sessionMentionMaxVisible]
	}
	rows := make([]ui.Widget, 0, len(entries)+2)
	if len(entries) == 0 {
		label := "No sessions found"
		if w.Source.Loading {
			label = "Loading sessions…"
		} else if w.Source.Error != "" {
			label = "Could not load sessions: " + w.Source.Error
		}
		rows = append(rows, ui.Text{Value: label, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis})
	}
	for _, entry := range entries {
		style := ui.Style{Foreground: presentation.ItemText}
		secondary := ui.Style{Foreground: theme.MutedForeground}
		if entry.ID == w.Controller.Selection {
			style = ui.Style{Foreground: presentation.FocusedText, Background: presentation.FocusedBg}
			secondary = style
		}
		row := ui.DecoratedBox(ui.Decoration{Style: style}, ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			ui.Expanded(ui.Text{Value: sessionMentionName(entry) + " · " + entry.CWD, Style: style, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}),
			ui.SizedBox{Width: 1}, ui.Text{Value: shortSessionID(entry.ID), Style: secondary, MaxLines: 1},
		}})
		rows = append(rows, mouseActivator{Child: ui.SizedBox{Height: 1, Child: row}, OnPressed: func(event ui.EventContext) {
			if w.OnSelect != nil {
				w.OnSelect(event, entry.ID)
			}
		}})
	}
	rows = append(rows, ui.SizedBox{Height: 1}, ui.Text{Value: "↑↓ move · enter insert · esc close", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis})
	content := ui.Padding(ui.All(1), ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows})
	anchor := w.Controller.Anchor
	return composerOverlayPositioner{BottomInset: w.BottomInset, PrimaryPercent: w.PrimaryPercent, Composer: w.Composer, Anchor: &anchor, Child: proportionalWidth{Percent: 80, Min: 48, Max: composerOverlayMaxWidth, Child: ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Foreground: theme.Foreground, Background: theme.Background}, Border: ui.BorderAll(ui.Style{Foreground: theme.Border, Background: theme.Background})}, content,
	)}}
}
