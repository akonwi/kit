package tui

import "go.rockorager.dev/vaxis/ui"

// controlFocusScope identifies one mounted control, independently of its
// changing value. The native editor remains mounted, retaining its cursor and
// selection. Returning to the control only requests focus, never resets it.
type controlFocusScope struct {
	Child   ui.Widget
	Passive bool
}

func (controlFocusScope) CreateState() ui.State { return &controlFocusState{} }

type controlFocusState struct {
	ui.StateBase
	mounted bool
	region  shellFocusReturn
}
type restoreControlFocus struct{ Target *controlFocusState }
type controlFocusRegion struct{ Target shellFocusReturn }
type captureControlFocusIntent struct{ Report func(*controlFocusState) }

func (captureControlFocusIntent) IntentType() ui.IntentType { return "kit.capture-control-focus" }
func (s *controlFocusState) InitState()                     { s.mounted = true }
func (s *controlFocusState) Dispose()                       { s.mounted = false }
func (s *controlFocusState) Build(ctx ui.BuildContext) ui.Widget {
	if region, ok := ui.Depend[controlFocusRegion](ctx); ok {
		s.region = region.Target
	}
	restore, _ := ui.Depend[restoreControlFocus](ctx)
	return ui.Actions{Bindings: map[ui.IntentType]ui.ActionFunc{
		captureControlFocusIntent{}.IntentType(): func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			intent.(captureControlFocusIntent).Report(s)
			return ui.EventHandled
		},
	}, Child: ui.FocusScope{AutoFocus: restore.Target == s, Child: s.Widget().(controlFocusScope).Child}}
}
func (s *controlFocusState) valid(w shellView) bool {
	if s == nil || !s.mounted || s.region.sessionID != w.Snapshot.Session.ID {
		return false
	}
	if !s.region.content {
		return true
	}
	return s.region.validContent(w)
}
func focusRegionFor(w shellView, content bool) controlFocusRegion {
	target := captureShellFocus(w)
	target.content = content
	return controlFocusRegion{Target: target}
}

func focusRegionForPane(w shellView, descriptor workspacePaneDescriptor) controlFocusRegion {
	region := focusRegionFor(w, true)
	region.Target.pane, _ = workspacePaneIdentityFor(descriptor)
	region.Target.generation = descriptor.OpenGeneration
	return region
}

func captureInputControl(ctx ui.EventContext) *controlFocusState {
	var control *controlFocusState
	ctx.Invoke(captureControlFocusIntent{Report: func(c *controlFocusState) { control = c }})
	return control
}
