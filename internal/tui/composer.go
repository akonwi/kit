package tui

import "go.rockorager.dev/vaxis/ui"

const composerMaxHeight = 10

type submitComposerIntent struct{}

func (submitComposerIntent) IntentType() ui.IntentType { return "kit.composer.submit" }

type messageComposer struct {
	Value       string
	Placeholder string
	OnChanged   ui.TextChangedCallback
	OnSubmitted ui.TextChangedCallback
}

func (messageComposer) CreateState() ui.State { return &messageComposerState{} }

type messageComposerState struct {
	ui.StateBase
	value string
}

func (s *messageComposerState) InitState() {
	s.value = s.Widget().(messageComposer).Value
}

func (s *messageComposerState) DidUpdateWidget(old ui.Widget) {
	previous := old.(messageComposer)
	current := s.Widget().(messageComposer)
	if current.Value != previous.Value {
		s.value = current.Value
	}
}

func (s *messageComposerState) Build(ctx ui.BuildContext) ui.Widget {
	config := s.Widget().(messageComposer)
	theme := ui.MustDepend[ui.Theme](ctx)
	input := ui.TextArea{
		Value:     s.value,
		OnChanged: s.changed,
		Padding:   ui.Symmetric(1, 0),
		MinHeight: 1,
		MaxHeight: composerMaxHeight,
		SoftWrap:  true,
		AutoFocus: true,
	}
	children := []ui.Widget{input}
	if s.value == "" && config.Placeholder != "" {
		children = append(children, composerPlaceholder{Child: ui.Padding(ui.Symmetric(1, 0), ui.Text{
			Value:    config.Placeholder,
			Style:    ui.Style{Foreground: theme.MutedForeground},
			Overflow: ui.TextOverflowEllipsis,
			MaxLines: 1,
		})})
	}
	return ui.Actions{
		Bindings: map[ui.IntentType]ui.ActionFunc{
			submitComposerIntent{}.IntentType(): s.submit,
		},
		Child: ui.Shortcuts{
			Bindings: ui.ShortcutMap{
				"Enter":       submitComposerIntent{},
				"Shift+Enter": ui.InsertLineBreakIntent{},
			},
			Child: ui.Stack{Alignment: ui.TopLeft, Children: children},
		},
	}
}

func (s *messageComposerState) changed(ctx ui.EventContext, value string) {
	config := s.Widget().(messageComposer)
	s.SetState(func() { s.value = value })
	if config.OnChanged != nil {
		config.OnChanged(ctx, value)
	}
}

func (s *messageComposerState) submit(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
	if callback := s.Widget().(messageComposer).OnSubmitted; callback != nil {
		callback(ctx, s.value)
	}
	return ui.EventHandled
}

type composerPlaceholder struct {
	Child ui.Widget
}

func (w composerPlaceholder) WidgetChild() ui.Widget { return w.Child }

func (composerPlaceholder) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderComposerPlaceholder{}
}

func (composerPlaceholder) UpdateRenderObject(ui.BuildContext, ui.RenderObject) {}

type renderComposerPlaceholder struct {
	ui.SingleChildRenderObject
}

func (r *renderComposerPlaceholder) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	child := r.Child()
	if child == nil {
		r.SetSize(constraints.Constrain(ui.Size{}))
		return
	}
	child.Layout(ctx, constraints)
	r.SetSize(constraints.Constrain(child.Base().Size()))
}

func (r *renderComposerPlaceholder) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if child := r.Child(); child != nil {
		return constraints.Constrain(ui.DryLayout(ctx, child, constraints))
	}
	return constraints.Constrain(ui.Size{})
}

func (r *renderComposerPlaceholder) Paint(painter *ui.Painter, offset ui.Offset) {
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderComposerPlaceholder) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
func (*renderComposerPlaceholder) HitTestSelf(ui.Point) bool                { return false }
func (*renderComposerPlaceholder) HitTestChild(ui.RenderObject) bool        { return false }
func (*renderComposerPlaceholder) SelectionDisabled() bool                  { return true }
