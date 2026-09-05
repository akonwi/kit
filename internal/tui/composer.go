package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

const composerMaxHeight = 10

type submitComposerIntent struct{}

func (submitComposerIntent) IntentType() ui.IntentType { return "kit.composer.submit" }

type openBashHistoryIntent struct{ Delta int }

func (openBashHistoryIntent) IntentType() ui.IntentType { return "kit.composer.bash-history" }

type messageComposer struct {
	Value               string
	Placeholder         string
	OnChanged           ui.TextChangedCallback
	OnSubmitted         ui.TextChangedCallback
	OpenPalette         ui.VoidCallback
	OpenBashHistory     func(ui.EventContext, int) bool
	CursorEndGeneration uint64
}

func (messageComposer) CreateState() ui.State { return &messageComposerState{} }

type messageComposerState struct {
	ui.StateBase
	value               string
	cursorEndGeneration uint64
}

func (s *messageComposerState) InitState() {
	s.value = s.Widget().(messageComposer).Value
}

func (s *messageComposerState) DidUpdateWidget(ui.Widget) {
	current := s.Widget().(messageComposer)
	if current.Value != s.value {
		s.value = current.Value
	}
}

func (s *messageComposerState) Build(ctx ui.BuildContext) ui.Widget {
	config := s.Widget().(messageComposer)
	theme := ui.MustDepend[ui.Theme](ctx)
	var cursorOffset *int
	if config.CursorEndGeneration != s.cursorEndGeneration {
		offset := len(s.value)
		cursorOffset = &offset
		s.cursorEndGeneration = config.CursorEndGeneration
	}
	input := ui.TextArea{
		Value:        s.value,
		CursorOffset: cursorOffset,
		OnChanged:    s.changed,
		Padding:      ui.Symmetric(1, 0),
		MinHeight:    1,
		MaxHeight:    composerMaxHeight,
		SoftWrap:     true,
		AutoFocus:    true,
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
	actions := map[ui.IntentType]ui.ActionFunc{
		submitComposerIntent{}.IntentType(): s.submit,
	}
	shortcuts := ui.ShortcutMap{
		"Enter":       submitComposerIntent{},
		"Shift+Enter": ui.InsertLineBreakIntent{},
	}
	if strings.HasPrefix(s.value, "!") && config.OpenBashHistory != nil {
		shortcuts["Up"] = openBashHistoryIntent{Delta: -1}
		shortcuts["Down"] = openBashHistoryIntent{Delta: 1}
		actions[openBashHistoryIntent{}.IntentType()] = func(ctx ui.EventContext, intent ui.Intent) ui.EventResult {
			if callback := s.Widget().(messageComposer).OpenBashHistory; callback != nil && callback(ctx, intent.(openBashHistoryIntent).Delta) {
				return ui.EventHandled
			}
			return ui.EventIgnored
		}
	}
	if s.value == "" && config.OpenPalette != nil {
		shortcuts["/"] = openPaletteIntent{}
		actions[openPaletteIntent{}.IntentType()] = func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if callback := s.Widget().(messageComposer).OpenPalette; callback != nil {
				callback(ctx)
			}
			return ui.EventHandled
		}
	}
	return ui.Actions{
		Bindings: actions,
		Child: ui.Shortcuts{
			Bindings: shortcuts,
			Child:    ui.Stack{Alignment: ui.TopLeft, Children: children},
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
