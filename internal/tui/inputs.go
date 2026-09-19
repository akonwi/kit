package tui

import "go.rockorager.dev/vaxis/ui"

// textInput is Kit's row-owning single-line input primitive. It fills all
// remaining horizontal space by default; use it as a direct child of a
// horizontal ui.Flex. Deliberately compact controls may use ui.TextField
// directly, but must opt into that behavior explicitly.
func textInput(theme ui.Theme, config textInputConfig) ui.Widget {
	field := ui.TextField{
		Value: config.Value, Placeholder: config.Placeholder,
		OnChanged: config.OnChanged, OnSubmitted: config.OnSubmitted,
		Padding: config.Padding, ObscureText: config.ObscureText,
		CursorOffset: config.CursorOffset, AutoFocus: config.AutoFocus,
	}
	return ui.Expanded(textInputWidget{
		Theme: theme, Field: field,
		InitialCursorOffset: config.InitialCursorOffset, InitialCursorGeneration: config.InitialCursorGeneration,
	})
}

type textInputConfig struct {
	Value                   string
	Placeholder             string
	OnChanged               ui.TextChangedCallback
	OnSubmitted             ui.TextChangedCallback
	Padding                 ui.Insets
	ObscureText             bool
	CursorOffset            *int
	InitialCursorOffset     *int
	InitialCursorGeneration uint64
	AutoFocus               bool
}

type textInputWidget struct {
	Theme                   ui.Theme
	Field                   ui.TextField
	InitialCursorOffset     *int
	InitialCursorGeneration uint64
}

func (textInputWidget) CreateState() ui.State { return &textInputState{} }

type textInputState struct {
	ui.StateBase
	width                   int
	measured                bool
	appliedCursorGeneration uint64
}

func (s *textInputState) Build(ui.BuildContext) ui.Widget {
	widget := s.Widget().(textInputWidget)
	field := widget.Field
	applyInitialCursor := widget.InitialCursorOffset != nil &&
		widget.InitialCursorGeneration != s.appliedCursorGeneration
	if applyInitialCursor {
		field.CursorOffset = widget.InitialCursorOffset
	}
	if s.measured {
		field.MinWidth = max(1, s.width)
	}
	return widthProbe{
		WidthChanged: func(width int) {
			width = max(0, width)
			changed := !s.measured || width != s.width
			if changed {
				s.width = width
				s.measured = true
			}
			if applyInitialCursor {
				s.appliedCursorGeneration = widget.InitialCursorGeneration
				changed = true
			}
			if changed {
				s.MarkNeedsBuild()
			}
		},
		Child: ui.Provider[ui.Theme]{Value: widget.Theme, Child: controlFocusScope{Child: field}},
	}
}

type widthProbe struct {
	WidthChanged func(int)
	Child        ui.Widget
}

func (w widthProbe) WidgetChild() ui.Widget { return w.Child }

func (w widthProbe) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderWidthProbe{WidthChanged: w.WidthChanged}
}

func (w widthProbe) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	object.(*renderWidthProbe).WidthChanged = w.WidthChanged
}

type renderWidthProbe struct {
	ui.SingleChildRenderObject
	WidthChanged func(int)
}

func (r *renderWidthProbe) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	width := constraints.MinWidth
	if constraints.HasBoundedWidth() {
		width = constraints.MaxWidth
	}
	if r.WidthChanged != nil {
		r.WidthChanged(width)
	}
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
		r.SetSize(constraints.Constrain(child.Base().Size()))
		return
	}
	r.SetSize(constraints.Constrain(ui.Size{}))
}

func (r *renderWidthProbe) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderWidthProbe) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderWidthProbe) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
