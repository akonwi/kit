package tui

import "go.rockorager.dev/vaxis/ui"

type subagentDismissSurface struct {
	Name    string
	Pending bool
	Error   string
}

func (w subagentDismissSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	children := []ui.Widget{
		ui.Text{Value: w.Name, Style: ui.Style{Foreground: theme.AccentText}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		ui.SizedBox{Height: 1},
		ui.Text{Value: "Active and queued work will be aborted. This action cannot be undone.", Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true},
	}
	if w.Error != "" {
		children = append(children, ui.SizedBox{Height: 1}, ui.Text{Value: "Dismiss failed: " + w.Error, Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true})
	}
	footer := ui.Widget(ui.Text{Value: "enter confirm · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	if w.Pending {
		footer = spinnerWithLabel("Dismissing…", ui.Style{Foreground: theme.MutedForeground})
	} else if w.Error != "" {
		footer = ui.Text{Value: "enter retry · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1}
	}
	return dialogSurface(theme, "Dismiss subagent?", "", ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: children,
	}, footer, true)
}
