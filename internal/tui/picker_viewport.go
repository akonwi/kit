package tui

import "go.rockorager.dev/vaxis/ui"

// pickerViewportState is the scroll position a picker keeps between builds.
// The viewport render object resolves it against the real list height.
type pickerViewportState struct {
	Start  int
	Rows   int
	Window listWindow
}

// pickerViewport lays out a bounded run of built rows inside the list area.
// Children are [above indicator, below indicator, rows for items First...].
// Only the resolved window is laid out and painted, so the list never shows a
// scrollbar and indicators take a row only when items are hidden on that side.
type pickerViewport struct {
	Count         int
	First         int
	Reveal        int
	State         *pickerViewportState
	OnRowsChanged func()
	Children      []ui.Widget
}

func (w pickerViewport) WidgetChildren() []ui.Widget { return w.Children }

func (w pickerViewport) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderPickerViewport{Count: w.Count, First: w.First, Reveal: w.Reveal, State: w.State, OnRowsChanged: w.OnRowsChanged}
}

func (w pickerViewport) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderPickerViewport)
	render.Count, render.First, render.Reveal = w.Count, w.First, w.Reveal
	render.State, render.OnRowsChanged = w.State, w.OnRowsChanged
	render.MarkNeedsLayout()
}

type pickerViewportParentData struct {
	Offset  ui.Offset
	Visible bool
}

func (data pickerViewportParentData) RenderOffset() ui.Offset { return data.Offset }

type renderPickerViewport struct {
	ui.MultiChildRenderObject
	Count         int
	First         int
	Reveal        int
	State         *pickerViewportState
	OnRowsChanged func()
}

func (r *renderPickerViewport) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	size := pickerDialogViewportSize(constraints)
	children := r.Children()
	rows := max(0, len(children)-2)
	window := resolveListWindow(r.Count, size.Height, r.State.Start, r.Reveal)
	// Clamp to the built rows; the next build supplies rows for a moved window.
	if window.Start < r.First {
		window = shapeListWindow(r.Count, size.Height, r.First)
	}
	if last := r.First + rows; window.End > last {
		window.End = max(window.Start, last)
		window.Below = window.End < r.Count
	}
	r.State.Start, r.State.Window = window.Start, window
	if r.State.Rows != size.Height {
		r.State.Rows = size.Height
		if r.OnRowsChanged != nil {
			r.OnRowsChanged()
		}
	}
	row := func(child ui.RenderObject, y int, visible bool) {
		height := 0
		if visible {
			height = 1
		}
		child.Layout(ctx, ui.Tight(ui.Size{Width: size.Width, Height: height}))
		child.Base().SetParentData(pickerViewportParentData{Offset: ui.Offset{Y: y}, Visible: visible})
	}
	y := 0
	if len(children) >= 2 {
		row(children[0], y, window.Above)
		if window.Above {
			y++
		}
	}
	for index := 2; index < len(children); index++ {
		item := r.First + index - 2
		visible := item >= window.Start && item < window.End
		row(children[index], y, visible)
		if visible {
			y++
		}
	}
	if len(children) >= 2 {
		row(children[1], y, window.Below)
	}
	r.SetSize(size)
}

func (r *renderPickerViewport) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return pickerDialogViewportSize(constraints)
}

func (r *renderPickerViewport) Paint(painter *ui.Painter, offset ui.Offset) {
	for _, child := range r.Children() {
		if data, _ := child.Base().ParentData().(pickerViewportParentData); data.Visible {
			child.Paint(painter, offset.Add(data.Offset))
		}
	}
}

func (r *renderPickerViewport) ChildOffset(child ui.RenderObject) ui.Offset {
	data, _ := child.Base().ParentData().(pickerViewportParentData)
	return data.Offset
}

func (*renderPickerViewport) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
