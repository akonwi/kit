package tui

import (
	"context"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
)

func (s *appState) openWorkspaceFilePicker() {
	files, ok := s.bound.(sessionclient.WorkspaceFilesSession)
	if !ok {
		s.showToast(toastInput{Title: "Workspace files unavailable", Subtitle: "This session does not expose workspace files.", Variant: toastWarning})
		return
	}
	if s.workspaceFilePickerCancel != nil {
		s.workspaceFilePickerCancel()
	}
	ctx, cancel := context.WithCancel(s.attachmentCtx)
	s.workspaceFilePickerContext, s.workspaceFilePickerCancel = ctx, cancel
	s.SetState(func() {
		s.workspaceFilePicker.reset()
		s.workspaceFilePickerScroll = ui.ScrollController{}
		s.workspaceFilePickerRevealPending = false
	})
	s.ensureIndexedFiles(s.Context().Runtime(), false)
	s.loadWorkspaceFilePickerRef(ctx, files, s.workspaceFilePicker.Generation)
}

func (s *appState) requestWorkspaceFilePickerRefresh() {
	if s.filePickerRefreshHook != nil {
		s.filePickerRefreshHook()
		return
	}
	s.refreshWorkspaceFilePicker()
}

func (s *appState) refreshWorkspaceFilePicker() {
	if !s.workspaceFilePicker.Open {
		return
	}
	s.ensureIndexedFiles(s.Context().Runtime(), true)
	if s.workspaceFilePicker.Workspace.WorkspaceID == "" || s.workspaceFilePicker.WorkspaceError != "" {
		files, ok := s.bound.(sessionclient.WorkspaceFilesSession)
		if !ok {
			return
		}
		if s.workspaceFilePickerCancel != nil {
			s.workspaceFilePickerCancel()
		}
		ctx, cancel := context.WithCancel(s.attachmentCtx)
		s.workspaceFilePickerContext, s.workspaceFilePickerCancel = ctx, cancel
		s.SetState(func() {
			s.workspaceFilePicker.Generation++
			s.workspaceFilePicker.Workspace = protocol.WorkspaceRef{}
			s.workspaceFilePicker.WorkspaceError = ""
		})
		s.loadWorkspaceFilePickerRef(ctx, files, s.workspaceFilePicker.Generation)
	}
}

func (s *appState) closeWorkspaceFilePicker() {
	if s.workspaceFilePickerCancel != nil {
		s.workspaceFilePickerCancel()
	}
	s.workspaceFilePickerContext, s.workspaceFilePickerCancel = nil, nil
	s.workspaceFilePicker.close()
	s.workspaceFilePickerScroll = ui.ScrollController{}
	s.workspaceFilePickerRevealPending = false
	s.workspaceFilePickerRevealOffset = 0
}

func (s *appState) reconcileWorkspaceIdentity(workspace *protocol.WorkspaceRef) {
	if workspace == nil || workspace.WorkspaceID == "" {
		return
	}
	changed := s.workspaceID != "" && s.workspaceID != workspace.WorkspaceID
	s.workspaceID = workspace.WorkspaceID
	pickerChanged := s.workspaceFilePicker.Open && s.workspaceFilePicker.Workspace.WorkspaceID != "" &&
		s.workspaceFilePicker.Workspace.WorkspaceID != workspace.WorkspaceID
	if changed || pickerChanged {
		s.closeWorkspaceFilePicker()
	}
}

func (s *appState) loadWorkspaceFilePickerRef(ctx context.Context, files sessionclient.WorkspaceFilesSession, generation uint64) {
	runtime := s.Context().Runtime()
	sessionID, cwd := s.session.ID, s.session.CWD
	go func() {
		workspace, err := files.Workspace(ctx)
		if ctx.Err() != nil || s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if !s.workspaceFilePicker.Open || s.workspaceFilePicker.Generation != generation || s.session.ID != sessionID || s.session.CWD != cwd {
				return
			}
			s.SetState(func() {
				if err != nil {
					s.workspaceFilePicker.WorkspaceError = workspaceFilePickerErrorText(err)
					return
				}
				if workspace.State != protocol.WorkspaceReady {
					s.workspaceFilePicker.WorkspaceError = "Workspace is unavailable"
					return
				}
				if s.workspaceID != "" && s.workspaceID != workspace.WorkspaceID {
					s.closeWorkspaceFilePicker()
					return
				}
				s.workspaceID = workspace.WorkspaceID
				s.workspaceFilePicker.Workspace = workspace
				s.workspaceFilePicker.WorkspaceError = ""
				s.workspaceFilePicker.ensureSelection(s.indexedFiles)
			})
		})
	}()
}

func workspaceFilePickerErrorText(err error) string {
	if err == nil {
		return "Could not load files"
	}
	return err.Error()
}

func (s *appState) requestWorkspaceFilePickerReveal() {
	rows := s.workspaceFilePicker.rows(s.indexedFiles)
	selection := s.workspaceFilePicker.selectedIndex(rows)
	viewport := s.workspaceFilePickerScroll.Metrics().ViewportHeight
	offset := max(0, selection-5)
	if viewport > 0 {
		current := s.workspaceFilePickerScroll.Metrics().ScrollOffset
		offset = current
		if selection < current {
			offset = selection
		} else if selection >= current+viewport {
			offset = selection - viewport + 1
		}
	}
	s.workspaceFilePickerRevealOffset = max(0, offset)
	s.workspaceFilePickerRevealPending = true
}

func (s *appState) workspaceFilePickerActivationCurrent(row workspaceFilePickerRow) bool {
	return row.Key.WorkspaceID != "" && row.Key.WorkspaceID == s.workspaceID && row.Key.WorkspaceID == s.workspaceFilePicker.Workspace.WorkspaceID &&
		s.indexedFiles.SessionID == s.session.ID && s.indexedFiles.CWD == s.session.CWD
}

func (s *appState) activateWorkspaceFilePickerRow(row workspaceFilePickerRow) {
	if !s.workspaceFilePicker.Open || !workspaceFilePickerRowSelectable(row) || row.Entry.IsDir {
		return
	}
	if !s.workspaceFilePickerActivationCurrent(row) {
		s.SetState(func() { s.closeWorkspaceFilePicker() })
		s.showToast(toastInput{Title: "Workspace changed", Subtitle: "Reopen the file picker for the current workspace.", Variant: toastWarning})
		return
	}
	var openErr error
	s.SetState(func() { openErr = s.openWorkspaceFilePickerEntry(row.Entry) })
	if openErr != nil {
		s.showToast(toastInput{Title: "Could not open file", Subtitle: openErr.Error(), Variant: toastWarning})
	}
}

func (s *appState) openWorkspaceFilePickerEntry(entry protocol.FileIndexEntry) error {
	if entry.IsDir {
		return nil
	}
	_, _, err := s.workspace.Open(fileWorkspacePane(s.workspaceFilePicker.Workspace.WorkspaceID, entry.Path))
	if err != nil {
		return err
	}
	s.closeWorkspaceFilePicker()
	s.syncWorkspaceSelection()
	return nil
}
