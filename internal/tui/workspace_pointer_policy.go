package tui

import "go.rockorager.dev/vaxis/ui"

// workspacePointerPolicy separates read-only selection/scrolling from actions
// that mutate a background pane. Resolve ownership at event time, not just at
// the frame that installed the pointer callback.
type workspacePointerPolicy struct{ Allow func(ui.EventContext) bool }

func panePointerPolicy(w shellView) workspacePointerPolicy {
	return workspacePointerPolicy{Allow: func(ctx ui.EventContext) bool {
		owner := w.Snapshot.inputOwner()
		if w.Callbacks.InputOwner != nil {
			owner = w.Callbacks.InputOwner()
		}
		return !owner.trapsFocus() || renderedInputTarget(ctx) == owner
	}}
}
