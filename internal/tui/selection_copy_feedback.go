package tui

import (
	"math"
	"sync/atomic"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const selectionCopyPulseDuration = 240 * time.Millisecond

type copySelectionIntent struct{ Observe func(string) }

func (copySelectionIntent) IntentType() ui.IntentType { return "kit.selection.copy" }

type selectionCopyPulseIntent struct{ Now time.Time }

func (selectionCopyPulseIntent) IntentType() ui.IntentType { return "kit.selection.copy-pulse" }

type selectionCopyNow func() time.Time

var selectionFeedbackMarkerSequence atomic.Uint32

// Selection markers occupy a renderer-invalid color namespace and are always
// replaced during the same paint. Their only purpose is to distinguish one
// selection domain from ordinary semantic backgrounds and nested selections.
func nextSelectionFeedbackMarker() ui.Color {
	return ui.Color(0x80000000 | selectionFeedbackMarkerSequence.Add(1))
}

type selectionFeedbackArea struct{ Child ui.Widget }

func (selectionFeedbackArea) CreateState() ui.State { return &selectionFeedbackAreaState{} }

type selectionFeedbackAreaState struct {
	ui.StateBase
	marker    ui.Color
	animation *ui.AnimationController
}

func (s *selectionFeedbackAreaState) InitState() {
	s.marker = nextSelectionFeedbackMarker()
	s.animation = s.NewAnimation(ui.AnimationOptions{Duration: selectionCopyPulseDuration, Curve: ui.EaseInOut})
}

func (s *selectionFeedbackAreaState) Build(ctx ui.BuildContext) ui.Widget {
	config := s.Widget().(selectionFeedbackArea)
	theme := ui.MustDepend[ui.Theme](ctx)
	marked := theme
	marked.Selection = s.marker
	// SelectionArea reads the marked theme while its child immediately returns
	// to the real theme, keeping nested selection domains and normal surfaces out.
	area := ui.Provider[ui.Theme]{Value: marked, Child: ui.SelectionArea{
		Child: ui.Provider[ui.Theme]{Value: theme, Child: config.Child},
	}}
	return s.pulseActions(selectionFeedbackPaint{
		Marker: s.marker, Selection: theme.Selection,
		PulseColor: selectionCopyPulseColor(theme, s.animation.Value()), Pulsing: s.animation.Running(), Child: area,
	})
}

func (s *selectionFeedbackAreaState) HandleEvent(_ ui.EventContext, event ui.Event) ui.EventResult {
	cancelSelectionPulseOnEdit(s.animation, event)
	return ui.EventIgnored
}

func (s *selectionFeedbackAreaState) pulseActions(child ui.Widget) ui.Widget {
	return ui.Actions{Bindings: map[ui.IntentType]ui.ActionFunc{
		selectionCopyPulseIntent{}.IntentType(): func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			now := intent.(selectionCopyPulseIntent).Now
			if now.IsZero() {
				now = time.Now()
			}
			s.animation.ForwardAt(now)
			return ui.EventHandled
		},
	}, Child: child}
}

// selectionFeedbackEditor marks selection styling for an editor whose native
// selection style is rebuilt from Theme.Selection on every build.
type selectionFeedbackEditor struct{ Child ui.Widget }

func (selectionFeedbackEditor) CreateState() ui.State { return &selectionFeedbackEditorState{} }

type selectionFeedbackEditorState struct {
	ui.StateBase
	marker    ui.Color
	animation *ui.AnimationController
}

func (s *selectionFeedbackEditorState) InitState() {
	s.marker = nextSelectionFeedbackMarker()
	s.animation = s.NewAnimation(ui.AnimationOptions{Duration: selectionCopyPulseDuration, Curve: ui.EaseInOut})
}

func (s *selectionFeedbackEditorState) HandleEvent(_ ui.EventContext, event ui.Event) ui.EventResult {
	cancelSelectionPulseOnEdit(s.animation, event)
	return ui.EventIgnored
}

func (s *selectionFeedbackEditorState) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	marked := theme
	marked.Selection = s.marker
	paint := selectionFeedbackPaint{
		Marker: s.marker, Selection: theme.Selection,
		PulseColor: selectionCopyPulseColor(theme, s.animation.Value()), Pulsing: s.animation.Running(),
		Child: ui.Provider[ui.Theme]{Value: marked, Child: s.Widget().(selectionFeedbackEditor).Child},
	}
	return ui.Actions{Bindings: map[ui.IntentType]ui.ActionFunc{
		selectionCopyPulseIntent{}.IntentType(): func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			now := intent.(selectionCopyPulseIntent).Now
			if now.IsZero() {
				now = time.Now()
			}
			s.animation.ForwardAt(now)
			return ui.EventHandled
		},
	}, Child: paint}
}

func cancelSelectionPulseOnEdit(animation *ui.AnimationController, event ui.Event) {
	if animation == nil || !animation.Running() {
		return
	}
	switch event := event.(type) {
	case ui.Mouse:
		if event.Button == ui.MouseLeftButton && (event.EventType == ui.EventPress || event.EventType == ui.EventMotion) {
			animation.Reset()
		}
	case ui.Key:
		if event.EventType != ui.EventRelease && (event.EventType == vaxis.EventPaste || !event.MatchString("Super+c")) {
			animation.Reset()
		}
	}
}

func selectionCopyPulseColor(theme ui.Theme, progress float64) ui.Color {
	selection := theme.Selection.Params()
	background := theme.Background.Params()
	if len(selection) != 3 || len(background) != 3 {
		// Indexed terminal colors cannot be interpolated. Keep the semantic
		// hover fill for most of the pulse, then settle back to selection.
		if progress < 0.85 && theme.SurfaceHovered != 0 && theme.SurfaceHovered != theme.Selection {
			return theme.SurfaceHovered
		}
		return theme.Selection
	}
	// Fade a small background tint out of the normal selection fill. Keeping
	// most of the selection color preserves text contrast across light and dark
	// themes while still making copy feedback perceptible.
	amount := 0.22 * (1 - min(1.0, max(0.0, progress)))
	blend := func(from, toward uint8) uint8 {
		return uint8(math.Round(float64(from)*(1-amount) + float64(toward)*amount))
	}
	return ui.RGB(blend(selection[0], background[0]), blend(selection[1], background[1]), blend(selection[2], background[2]))
}

type selectionFeedbackPaint struct {
	Child      ui.Widget
	Marker     ui.Color
	Selection  ui.Color
	PulseColor ui.Color
	Pulsing    bool
}

func (w selectionFeedbackPaint) WidgetChild() ui.Widget { return w.Child }

func (w selectionFeedbackPaint) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderSelectionFeedbackPaint{Marker: w.Marker, Selection: w.Selection, PulseColor: w.PulseColor, Pulsing: w.Pulsing}
}

func (w selectionFeedbackPaint) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderSelectionFeedbackPaint)
	render.Marker = w.Marker
	render.Selection = w.Selection
	render.PulseColor = w.PulseColor
	render.Pulsing = w.Pulsing
	render.MarkNeedsPaint()
}

type renderSelectionFeedbackPaint struct {
	ui.SingleChildRenderObject
	Marker     ui.Color
	Selection  ui.Color
	PulseColor ui.Color
	Pulsing    bool
}

func (r *renderSelectionFeedbackPaint) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
		return
	}
	r.SetSize(constraints.Constrain(ui.Size{}))
}

func (r *renderSelectionFeedbackPaint) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return constraints.Constrain(ui.DryLayout(ctx, r.Child(), constraints))
}

func (r *renderSelectionFeedbackPaint) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
	background := r.Selection
	if r.Pulsing && r.PulseColor != 0 {
		background = r.PulseColor
	}
	size := r.Size()
	for row := 0; row < size.Height; row++ {
		for column := 0; column < size.Width; column++ {
			point := ui.Point{X: offset.X + column, Y: offset.Y + row}
			cell := painter.Cell(point.X, point.Y)
			if cell.Background != r.Marker {
				continue
			}
			cell.Background = background
			painter.DrawCell(point, cell)
		}
	}
}

func (*renderSelectionFeedbackPaint) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
