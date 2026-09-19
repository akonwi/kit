package tui

import "go.rockorager.dev/vaxis/ui"

// shellFocusReturn is a bounded logical return address, not an overlay stack.
// A replaced session or pane must never inherit a stale focus request.
type shellFocusReturn struct {
	sessionID   string
	workspaceID string
	pane        workspacePaneIdentity
	generation  uint64
	content     bool
}

func captureShellFocus(w shellView) shellFocusReturn {
	workspace := w.workspaceSnapshot()
	target := shellFocusReturn{sessionID: w.Snapshot.Session.ID, workspaceID: w.Snapshot.CurrentWorkspaceID, pane: workspace.Selected, content: workspace.FocusOwner == workspaceFocusContent}
	if pane, ok := workspace.SelectedPane(); ok {
		target.generation = pane.OpenGeneration
		target.content = workspace.FocusOwner == workspaceFocusContent
	}
	return target
}

func (target shellFocusReturn) validContent(w shellView) bool {
	current := captureShellFocus(w)
	return target.content && target.sessionID == current.sessionID && target.workspaceID == current.workspaceID && target.pane == current.pane && target.generation == current.generation
}

func (target shellFocusReturn) valid(w shellView) bool {
	if target.sessionID != w.Snapshot.Session.ID {
		return false
	}
	return !target.content || target.validContent(w)
}

type shellFocusHost struct{ View shellView }

func (shellFocusHost) CreateState() ui.State { return &shellFocusHostState{} }

type shellFocusHostState struct {
	ui.StateBase
	trapped        bool
	returnTo       shellFocusReturn
	control        *controlFocusState
	returnControl  *controlFocusState
	previousRegion shellFocusReturn
	initialized    bool
}

func (s *shellFocusHostState) Build(ctx ui.BuildContext) ui.Widget {
	w := s.Widget().(shellFocusHost).View
	trapped := w.Snapshot.inputOwner().trapsFocus()
	region := captureShellFocus(w)
	if s.initialized && !trapped && region != s.previousRegion {
		if s.control != nil && (s.control.region.content != region.content || (region.content && s.control.region.pane != region.pane)) {
			s.control = nil
		}
		w.restoreContent = region.content
		w.restoreComposer = !region.content
	}
	s.previousRegion = region
	s.initialized = true
	if trapped && !s.trapped {
		s.returnTo = captureShellFocus(w)
		s.returnControl = s.control
		if s.control.valid(w) {
			s.returnTo = s.control.region
		}
	}
	if s.trapped && !trapped {
		w.restoreContent = s.returnTo.validContent(w)
		w.restoreComposer = !w.restoreContent
	}
	var restore *controlFocusState
	if s.trapped && !trapped && s.returnControl.valid(w) && s.returnTo.valid(w) {
		restore = s.returnControl
		w.restoreContent = false
		w.restoreComposer = false
	}
	s.trapped = trapped
	return ui.Provider[restoreControlFocus]{Value: restoreControlFocus{Target: restore}, Child: w.build(ctx)}
}

// retainedComposer keeps the editor (including its cursor and selection) mounted
// while the interaction dock temporarily replaces its visible layout slot.
type retainedComposer struct {
	Visible bool
	Child   ui.Widget
}

func (w retainedComposer) WidgetChild() ui.Widget { return w.Child }
func (w retainedComposer) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderRetainedWorkspacePane{Active: w.Visible}
}
func (w retainedComposer) UpdateRenderObject(_ ui.BuildContext, r ui.RenderObject) {
	render := r.(*renderRetainedWorkspacePane)
	if render.Active != w.Visible {
		render.Active = w.Visible
		render.MarkNeedsLayout()
	}
}

func (s *shellFocusHostState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() != ui.CapturePhase || s.Widget().(shellFocusHost).View.Snapshot.inputOwner().trapsFocus() {
		return ui.EventIgnored
	}
	ctx.Invoke(captureControlFocusIntent{Report: func(control *controlFocusState) { s.control = control }})
	return ui.EventIgnored
}
