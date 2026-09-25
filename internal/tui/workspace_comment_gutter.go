package tui

import "go.rockorager.dev/vaxis/ui"

// workspaceCommentGutter shares the code-facing comment action between normal
// files and diffs. Each viewer owns its source coordinates and range gestures.
func workspaceCommentGutter(base ui.Widget, width int, selected bool, theme ui.Theme, onPressed ui.VoidCallback, onMotion func(ui.EventContext, ui.Mouse)) ui.Widget {
	children := []ui.Widget{base}
	if selected {
		button := ui.SizedBox{Width: 3, Height: 1, Child: ui.Text{
			Value: " + ", Style: ui.Style{Foreground: theme.Background, Background: theme.Primary}, MaxLines: 1,
		}}
		children = append(children, ui.Positioned{Left: width - 3, Top: 0, Child: button})
	}
	return mouseActivator{Child: ui.SizedBox{Width: width, Height: 1, Child: ui.Stack{Children: children}}, OnPressed: onPressed, OnMotion: onMotion}
}
