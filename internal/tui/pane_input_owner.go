package tui

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

func (s *appState) setPaneInputOwner(descriptor workspacePaneDescriptor, kind paneInputKind, active bool) bool {
	identity, err := workspacePaneIdentityFor(descriptor)
	if err != nil {
		return false
	}
	candidate := paneInputOwner{
		Kind: kind, SessionID: s.session.ID, WorkspaceID: s.workspaceID,
		Pane: identity, Generation: descriptor.OpenGeneration,
	}
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
		}
		s.inputGeneration++
	})
	return true
}
