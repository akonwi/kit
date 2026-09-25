package tui

import "go.rockorager.dev/vaxis/ui"

// markdownLinkActivator adds drag-safe mouse activation to OSC 8 links without
// making their text focusable. It belongs outside the transcript SelectionArea
// so it observes the complete press/motion/release gesture captured there.
type markdownLinkActivator struct {
	Child      ui.Widget
	GestureKey string
	Enabled    func() bool
	OpenURL    ui.TextChangedCallback
}

func (w markdownLinkActivator) WidgetChild() ui.Widget { return w.Child }

func (w markdownLinkActivator) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderMarkdownLinkActivator{GestureKey: w.GestureKey, Enabled: w.Enabled, OpenURL: w.OpenURL}
}

func (w markdownLinkActivator) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderMarkdownLinkActivator)
	if render.GestureKey != w.GestureKey {
		render.cancelGesture()
	}
	render.GestureKey = w.GestureKey
	render.Enabled = w.Enabled
	render.OpenURL = w.OpenURL
	if !render.enabled() || render.OpenURL == nil {
		render.cancelGesture()
	}
}

type renderMarkdownLinkActivator struct {
	ui.SingleChildRenderObject
	GestureKey string
	Enabled    func() bool
	OpenURL    ui.TextChangedCallback

	links      []string
	linksWidth int
	pressedAt  ui.Point
	pressedURL string
	dragged    bool
}

func (r *renderMarkdownLinkActivator) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	previous := r.Size()
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
	} else {
		r.SetSize(constraints.Constrain(ui.Size{}))
	}
	if r.Size() != previous && r.pressedURL != "" {
		r.cancelGesture()
	}
}

func (r *renderMarkdownLinkActivator) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderMarkdownLinkActivator) Paint(painter *ui.Painter, offset ui.Offset) {
	child := r.Child()
	if child == nil {
		r.links = nil
		r.linksWidth = 0
		return
	}
	child.Paint(painter, offset)

	size := r.Size()
	r.linksWidth = size.Width
	r.links = make([]string, max(0, size.Width*size.Height))
	safeTargets := make(map[string]string)
	for row := 0; row < size.Height; row++ {
		for column := 0; column < size.Width; column++ {
			cell := painter.Cell(offset.X+column, offset.Y+row)
			target, checked := safeTargets[cell.Hyperlink]
			if !checked {
				target = safeExternalHyperlink(cell.Hyperlink)
				safeTargets[cell.Hyperlink] = target
			}
			if target == "" {
				continue
			}
			width := max(1, cell.Character.Width)
			for covered := column; covered < min(size.Width, column+width); covered++ {
				r.links[row*size.Width+covered] = target
			}
		}
	}
	if r.pressedURL != "" && r.linkAt(r.pressedAt) != r.pressedURL {
		r.cancelGesture()
	}
}

func (*renderMarkdownLinkActivator) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func (r *renderMarkdownLinkActivator) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() != ui.CapturePhase {
		return ui.EventIgnored
	}
	mouse, ok := event.(ui.Mouse)
	if !ok {
		return ui.EventIgnored
	}

	point := ui.Point{X: mouse.Col, Y: mouse.Row}
	switch mouse.EventType {
	case ui.EventPress:
		r.cancelGesture()
		if mouse.Button != ui.MouseLeftButton || mouse.Modifiers != 0 {
			return ui.EventIgnored
		}
		if !r.enabled() || r.OpenURL == nil {
			return ui.EventIgnored
		}
		if target := r.linkAt(point); target != "" {
			r.pressedAt = point
			r.pressedURL = target
		}
	case ui.EventMotion:
		if r.pressedURL != "" {
			r.dragged = true
		}
	case ui.EventRelease:
		if mouse.Button != ui.MouseLeftButton || r.pressedURL == "" {
			r.cancelGesture()
			return ui.EventIgnored
		}
		target := r.pressedURL
		activate := mouse.Modifiers == 0 && !r.dragged && point == r.pressedAt && r.enabled() &&
			r.OpenURL != nil && r.linkAt(point) == target
		r.cancelGesture()
		if activate {
			r.OpenURL(ctx, target)
		}
	}
	// SelectionArea still owns selection, focus, drag, and release handling.
	return ui.EventIgnored
}

func (r *renderMarkdownLinkActivator) enabled() bool {
	return r.Enabled == nil || r.Enabled()
}

func (r *renderMarkdownLinkActivator) linkAt(point ui.Point) string {
	size := r.Size()
	if point.X < 0 || point.Y < 0 || point.X >= size.Width || point.Y >= size.Height || r.linksWidth != size.Width {
		return ""
	}
	index := point.Y*r.linksWidth + point.X
	if index < 0 || index >= len(r.links) {
		return ""
	}
	return r.links[index]
}

func (r *renderMarkdownLinkActivator) cancelGesture() {
	r.pressedAt = ui.Point{}
	r.pressedURL = ""
	r.dragged = false
}
