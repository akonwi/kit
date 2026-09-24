package tui

import (
	"fmt"

	"go.rockorager.dev/vaxis/ui"
)

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

// transcriptLatestButton is the compact shortcut shown at the transcript's
// bottom-right corner when the latest content is scrolled out of view. It
// mirrors the macOS transcript's "Latest" resume button.
type transcriptLatestButton struct {
	OnPressed ui.VoidCallback
}

func (transcriptLatestButton) CreateState() ui.State { return &transcriptLatestButtonState{} }

type transcriptLatestButtonState struct {
	ui.StateBase
	hovered bool
}

func (s *transcriptLatestButtonState) Build(ctx ui.BuildContext) ui.Widget {
	button := s.Widget().(transcriptLatestButton)
	theme := ui.MustDepend[ui.Theme](ctx)
	style := ui.Style{Foreground: theme.Foreground, Background: theme.Surface}
	if s.hovered {
		style.Background = theme.SurfaceHovered
	}
	return mouseActivator{
		OnPressed: button.OnPressed,
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
		Child: ui.Text{Value: " " + glyphArrowDown + " Latest ", Style: style, MaxLines: 1},
	}
}

func (s *plainButtonState) Build(ctx ui.BuildContext) ui.Widget {
	config := s.Widget().(plainButton)
	theme := ui.MustDepend[ui.Theme](ctx)
	style := ui.Style{Foreground: theme.Foreground, Background: theme.Surface}
	if s.focused || s.hovered {
		style.Background = theme.SurfaceHovered
	}
	return controlFocusScope{Child: ui.RichText{Spans: []ui.TextSpan{{
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
	}}}}
}

// transcriptReadingStrip is the one-row section navigation pinned to the
// top of the transcript while a tall assistant message fills the viewport.
type transcriptReadingStrip struct {
	Reading transcriptReadingSnapshot
	OnOpen  ui.VoidCallback
	OnMove  func(ui.EventContext, int)
}

func (transcriptReadingStrip) CreateState() ui.State { return &transcriptReadingStripState{} }

type transcriptReadingStripState struct {
	ui.StateBase
	hovered bool
}

func (s *transcriptReadingStripState) Build(ctx ui.BuildContext) ui.Widget {
	strip := s.Widget().(transcriptReadingStrip)
	theme := ui.MustDepend[ui.Theme](ctx)
	muted := ui.Style{Foreground: theme.MutedForeground, Background: theme.Surface}
	titleStyle := ui.Style{Foreground: theme.PrimaryText, Background: theme.Surface}
	if s.hovered {
		titleStyle.Background = theme.SurfaceHovered
	}
	reading := strip.Reading
	position := fmt.Sprintf("%d / %d", reading.Selected+1, reading.Count)
	previous := ui.Widget(ui.Text{Value: glyphChevronUp, Style: muted, MaxLines: 1})
	next := ui.Widget(ui.Text{Value: glyphArrowDown, Style: muted, MaxLines: 1})
	if strip.OnMove != nil && reading.Selected > 0 {
		previous = mouseActivator{OnPressed: func(ctx ui.EventContext) { strip.OnMove(ctx, -1) }, Child: previous}
	}
	if strip.OnMove != nil && reading.Selected < reading.Count-1 {
		next = mouseActivator{OnPressed: func(ctx ui.EventContext) { strip.OnMove(ctx, 1) }, Child: next}
	}
	title := mouseActivator{
		OnPressed: strip.OnOpen,
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
		Child: ui.Text{Value: reading.Title, Style: titleStyle, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
	}
	return ui.DecoratedBox(ui.Decoration{Style: ui.Style{Background: theme.Surface}}, ui.Padding(ui.Insets{Left: transcriptMinMargin, Right: 1}, ui.Flex{
		Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			ui.Flexible(title),
			ui.SizedBox{Width: 1},
			ui.Text{Value: position, Style: muted, MaxLines: 1},
			ui.SizedBox{Width: 1},
			previous,
			ui.Text{Value: " ", Style: muted, MaxLines: 1},
			next,
		},
	}))
}
