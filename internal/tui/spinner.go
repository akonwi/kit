package tui

import (
	"time"

	"go.rockorager.dev/vaxis/ui"
)

const spinnerFrameDuration = 80 * time.Millisecond

var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinner is Kit's reusable animated activity indicator. It animates only while
// mounted, so callers do not need to manage frame state or lifecycle.
type spinner struct {
	Style ui.Style
}

func (spinner) CreateState() ui.State { return &spinnerState{} }

type spinnerState struct {
	ui.StateBase
	index    int
	previous time.Time
}

func (s *spinnerState) Build(ui.BuildContext) ui.Widget {
	config := s.Widget().(spinner)
	return ui.Text{Value: spinnerFrames[s.index], Style: config.Style}
}

func (s *spinnerState) TickFrame(now time.Time) bool {
	index, tick, changed := advanceSpinner(s.index, s.previous, now)
	s.index = index
	s.previous = tick
	if changed {
		s.MarkNeedsBuild()
	}
	return true
}

func spinnerWithLabel(label string, style ui.Style) ui.Widget {
	return ui.Flex{
		Axis:               ui.Horizontal,
		CrossAxisAlignment: ui.CrossAxisStart,
		Children: []ui.Widget{
			spinner{Style: style},
			ui.SizedBox{Width: 1},
			ui.Expanded(ui.Text{Value: label, Style: style, SoftWrap: true}),
		},
	}
}

func advanceSpinner(index int, previous, now time.Time) (int, time.Time, bool) {
	if previous.IsZero() || now.Before(previous) {
		return index, now, false
	}
	steps := int(now.Sub(previous) / spinnerFrameDuration)
	if steps == 0 {
		return index, previous, false
	}
	return (index + steps) % len(spinnerFrames), previous.Add(time.Duration(steps) * spinnerFrameDuration), true
}
