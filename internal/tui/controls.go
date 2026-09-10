package tui

import "go.rockorager.dev/vaxis/ui"

// plainButton is a compact text action whose focus is communicated by its
// background rather than decorative characters around its label.
type plainButton struct {
	Label     string
	OnPressed ui.VoidCallback
}

func (plainButton) CreateState() ui.State { return &plainButtonState{} }

type plainButtonState struct {
	ui.StateBase
	focused bool
	hovered bool
}

// headerControl is an intrinsic-width clickable header segment. Its hit region
// is exactly its visible label and only a primary press activates it.
type headerControl struct {
	Label     string
	Primary   bool
	OnPressed ui.VoidCallback
}

func (headerControl) CreateState() ui.State { return &headerControlState{} }

type headerControlState struct {
	ui.StateBase
	hovered bool
}

func (state *headerControlState) Build(ctx ui.BuildContext) ui.Widget {
	config := state.Widget().(headerControl)
	theme := ui.MustDepend[ui.Theme](ctx)
	foreground := theme.MutedForeground
	if config.Primary {
		foreground = theme.Foreground
	}
	style := ui.Style{Foreground: foreground, Background: theme.Background}
	if state.hovered {
		style.Foreground = theme.Foreground
		style.Background = theme.SurfaceHovered
	}
	return mouseActivator{
		OnPressed: config.OnPressed,
		OnHover: func(ui.EventContext) {
			if !state.hovered {
				state.SetState(func() { state.hovered = true })
			}
		},
		OnHoverExit: func(ui.EventContext) {
			if state.hovered {
				state.SetState(func() { state.hovered = false })
			}
		},
		Child: ui.Text{Value: config.Label, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
	}
}

func (s *plainButtonState) Build(ctx ui.BuildContext) ui.Widget {
	config := s.Widget().(plainButton)
	theme := ui.MustDepend[ui.Theme](ctx)
	style := ui.Style{Foreground: theme.Foreground, Background: theme.Surface}
	if s.focused || s.hovered {
		style.Background = theme.SurfaceHovered
	}
	return ui.RichText{Spans: []ui.TextSpan{{
		Text:            " " + config.Label + " ",
		Style:           style,
		ClickAffordance: ui.ClickAffordanceNone,
		OnPressed:       config.OnPressed,
		OnHover: func(ui.EventContext) {
			if !s.hovered {
				s.SetState(func() { s.hovered = true })
			}
		},
		OnHoverExit: func(ui.EventContext) {
			if s.hovered {
				s.SetState(func() { s.hovered = false })
			}
		},
		OnFocus: func() {
			if !s.focused {
				s.SetState(func() { s.focused = true })
			}
		},
		OnFocusExit: func() {
			if s.focused {
				s.SetState(func() { s.focused = false })
			}
		},
	}}}
}
