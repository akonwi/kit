package tui

import "go.rockorager.dev/vaxis/ui"

// paneInputKind identifies a pane-local surface that temporarily owns shell
// input. It is intentionally limited to explicit pane children rather than an
// arbitrary overlay stack.
type paneInputKind uint8

const (
	paneInputNone paneInputKind = iota
	paneInputDiffTarget
)

// paneInputOwner binds pane-local ownership to one attached session and one
// retained pane incarnation. Stale notifications cannot affect a replacement
// session or pane.
type paneInputOwner struct {
	Kind        paneInputKind
	SessionID   string
	WorkspaceID string
	Pane        workspacePaneIdentity
	Generation  uint64
}

func (o paneInputOwner) active(snapshot shellSnapshot) bool {
	if snapshot.Phase != phaseReady || o.Kind == paneInputNone || o.SessionID == "" || o.SessionID != snapshot.Session.ID || o.WorkspaceID != snapshot.CurrentWorkspaceID {
		return false
	}
	if snapshot.Workspace.Selected != o.Pane {
		return false
	}
	pane, ok := snapshot.Workspace.SelectedPane()
	if !ok || pane.OpenGeneration != o.Generation {
		return false
	}
	switch o.Kind {
	case paneInputDiffTarget:
		return pane.Kind == workspacePaneDiff
	default:
		return false
	}
}

func (o paneInputOwner) samePane(other paneInputOwner) bool {
	return o.SessionID == other.SessionID && o.WorkspaceID == other.WorkspaceID && o.Pane == other.Pane && o.Generation == other.Generation
}

func (s *appState) paneInputCandidate(descriptor workspacePaneDescriptor, kind paneInputKind) (paneInputOwner, bool) {
	identity, err := workspacePaneIdentityFor(descriptor)
	if err != nil {
		return paneInputOwner{}, false
	}
	return paneInputOwner{
		Kind: kind, SessionID: s.session.ID, WorkspaceID: s.workspaceID,
		Pane: identity, Generation: descriptor.OpenGeneration,
	}, true
}

func (s *appState) setPaneInputKeyHandler(descriptor workspacePaneDescriptor, handler func(ui.Key) ui.EventResult) {
	candidate, ok := s.paneInputCandidate(descriptor, paneInputDiffTarget)
	if !ok {
		return
	}
	if handler != nil {
		if s.paneInput != candidate {
			return
		}
		s.paneInputKeyHandler, s.paneInputKeyHandlerOwner = handler, candidate
		return
	}
	if s.paneInputKeyHandlerOwner == candidate {
		s.paneInputKeyHandler, s.paneInputKeyHandlerOwner = nil, paneInputOwner{}
	}
}

func (s *appState) setPaneInputOwner(descriptor workspacePaneDescriptor, kind paneInputKind, active bool) bool {
	candidate, ok := s.paneInputCandidate(descriptor, kind)
	if !ok {
		return false
	}
	identity := candidate.Pane
	if active {
		if s.paneInput == candidate {
			return true
		}
		pane, selected := s.workspace.SelectedPane()
		if !selected || s.workspace.SelectedIdentity() != identity || pane.OpenGeneration != descriptor.OpenGeneration || pane.Kind != descriptor.Kind || !s.canOpenRootModal() {
			return false
		}
	}
	s.SetState(func() {
		if active {
			s.paneInput = candidate
		} else {
			if !s.paneInput.samePane(candidate) || s.paneInput.Kind != kind {
				return
			}
			s.paneInput = paneInputOwner{}
			if s.paneInputKeyHandlerOwner == candidate {
				s.paneInputKeyHandler, s.paneInputKeyHandlerOwner = nil, paneInputOwner{}
			}
		}
		s.inputGeneration++
	})
	return true
}
