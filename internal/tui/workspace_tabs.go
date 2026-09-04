package tui

import (
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	workspaceTabMinWidth = 6
	workspaceTabMaxWidth = 24
)

type workspaceTab struct {
	Label    string
	Selected bool
	Closable bool
	OnSelect ui.VoidCallback
	OnClose  ui.VoidCallback
}

func (workspaceTab) CreateState() ui.State { return &workspaceTabState{} }

type workspaceTabState struct {
	ui.StateBase
	hovered bool
}

func (s *workspaceTabState) Build(ctx ui.BuildContext) ui.Widget {
	tab := s.Widget().(workspaceTab)
	theme := ui.MustDepend[ui.Theme](ctx)
	tabWidth := workspaceTabWidth(tab.Label, tab.Closable)
	labelWidth := tabWidth
	if tab.Closable {
		labelWidth -= 2
	}
	displayLabel := truncateWorkspaceTabLabel(tab.Label, max(1, labelWidth-2))
	foreground := theme.MutedForeground
	if tab.Selected || s.hovered {
		foreground = theme.Foreground
	}
	background := theme.Background
	if s.hovered {
		background = theme.SurfaceHovered
	}
	hover := func(ui.EventContext) {
		if !s.hovered {
			s.SetState(func() { s.hovered = true })
		}
	}
	exit := func(ui.EventContext) {
		if s.hovered {
			s.SetState(func() { s.hovered = false })
		}
	}
	children := []ui.Widget{mouseActivator{
		OnPressed: tab.OnSelect, OnHover: hover, OnHoverExit: exit,
		Child: ui.SizedBox{Width: labelWidth, Height: 1, Child: ui.Text{
			Value: " " + displayLabel + " ", Style: ui.Style{Foreground: foreground, Background: background},
			Overflow: ui.TextOverflowClip, MaxLines: 1,
		}},
	}}
	if tab.Closable {
		closeForeground := theme.MutedForeground
		if tab.Selected || s.hovered {
			closeForeground = theme.Foreground
		}
		children = append(children, mouseActivator{
			OnPressed: tab.OnClose, OnHover: hover, OnHoverExit: exit,
			Child: ui.SizedBox{Width: 2, Height: 1, Child: ui.Text{
				Value: " " + glyphTimes, Style: ui.Style{Foreground: closeForeground, Background: background},
				Overflow: ui.TextOverflowClip, MaxLines: 1,
			}},
		})
	}
	return ui.SizedBox{Width: tabWidth, Height: 1, Child: ui.Flex{Axis: ui.Horizontal, Children: children}}
}

func workspaceTabWidth(label string, closable bool) int {
	chromeWidth := 2
	if closable {
		chromeWidth = 4
	}
	return max(workspaceTabMinWidth, min(workspaceTabMaxWidth, workspaceTextWidth(label)+chromeWidth))
}

func workspaceTextWidth(value string) int {
	width := 0
	for _, character := range vaxis.Characters(value) {
		width += character.Width
	}
	return width
}

func truncateWorkspaceTabLabel(label string, width int) string {
	if workspaceTextWidth(label) <= width {
		return label
	}
	if width <= 1 {
		return "…"
	}
	characters := vaxis.Characters(label)
	used := 0
	visible := make([]vaxis.Character, 0, len(characters))
	for _, character := range characters {
		if used+character.Width > width-1 {
			break
		}
		visible = append(visible, character)
		used += character.Width
	}
	result := ""
	for _, character := range visible {
		result += character.Grapheme
	}
	return result + "…"
}
