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
	Style  ui.Style
	Label  string
	Center bool
}

func (spinner) CreateState() ui.State { return &spinnerState{} }

type spinnerState struct {
	ui.StateBase
	index    int
	previous time.Time
}

func (s *spinnerState) Build(ui.BuildContext) ui.Widget {
	config := s.Widget().(spinner)
	value := spinnerFrames[s.index]
	if config.Label != "" {
		value += " " + config.Label
	}
	if config.Center {
		return centeredSpinnerLine{Value: value, Style: config.Style}
	}
	return ui.Text{Value: value, Style: config.Style}
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

// centeredSpinnerLine owns its row so it can center an animated spinner and
// label together instead of expanding only the label inside a horizontal Flex.
type centeredSpinnerLine struct {
	Value string
	Style ui.Style
}

func (w centeredSpinnerLine) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderCenteredSpinnerLine{value: w.Value, style: w.Style}
}

func (w centeredSpinnerLine) UpdateRenderObject(_ ui.BuildContext, renderObject ui.RenderObject) {
	render := renderObject.(*renderCenteredSpinnerLine)
	if render.value != w.Value || render.style != w.Style {
		render.value = w.Value
		render.style = w.Style
		render.MarkNeedsPaint()
	}
}

type renderCenteredSpinnerLine struct {
	ui.LeafRenderObject
	value string
	style ui.Style
}

func (r *renderCenteredSpinnerLine) Layout(_ ui.LayoutContext, constraints ui.Constraints) {
	r.SetSize(r.size(constraints))
}

func (r *renderCenteredSpinnerLine) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return r.size(constraints)
}

func (r *renderCenteredSpinnerLine) size(constraints ui.Constraints) ui.Size {
	width := len([]rune(r.value))
	if constraints.HasBoundedWidth() {
		width = constraints.MaxWidth
	}
	return constraints.Constrain(ui.Size{Width: width, Height: 1})
}

func (r *renderCenteredSpinnerLine) Paint(painter *ui.Painter, offset ui.Offset) {
	contentWidth := len([]rune(r.value))
	x := max(0, (r.Size().Width-contentWidth)/2)
	painter.DrawText(offset.Add(ui.Offset{X: x}), r.value, r.style)
}

func centeredSpinnerWithLabel(label string, style ui.Style) ui.Widget {
	return spinner{Style: style, Label: label, Center: true}
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
