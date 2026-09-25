package tui

import "go.rockorager.dev/vaxis/ui"

type sessionDeleteSurface struct {
	Snapshot sessionExplorerSnapshot
}

func (w sessionDeleteSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	targetLabel := shortSessionID(w.Snapshot.DeleteSessionID)
	for _, session := range w.Snapshot.Sessions {
		if session.ID == w.Snapshot.DeleteSessionID {
			targetLabel = sessionExplorerItemLabel(session)
			break
		}
	}
	children := []ui.Widget{
		ui.Text{Value: targetLabel, Style: ui.Style{Foreground: theme.Foreground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		ui.SizedBox{Height: 1},
		ui.Text{Value: "This action cannot be undone.", Style: ui.Style{Foreground: theme.DangerText}},
	}
	if w.Snapshot.DeleteError != "" {
		children = append(children,
			ui.SizedBox{Height: 1},
			ui.Text{Value: "Delete failed: " + w.Snapshot.DeleteError, Style: ui.Style{Foreground: theme.DangerText}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		)
	}
	body := ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch,
		Children: children,
	}
	var footer ui.Widget = ui.Text{
		Value: "enter confirm · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground},
		Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
	}
	if w.Snapshot.DeletePending {
		footer = spinnerWithLabel("Deleting…", ui.Style{Foreground: theme.MutedForeground})
	} else if w.Snapshot.DeleteError != "" {
		footer = ui.Text{
			Value: "enter retry · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground},
			Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		}
	}
	return dialogSurface(theme, "Delete session?", "", body, footer, true)
}
