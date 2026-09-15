package tui

import (
	"context"
	"errors"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
)

type workspaceFilePickerJob struct {
	key         workspaceFileKey
	serial      uint64
	generation  uint64
	workspaceID string
	path        string
	cursor      string
	pageSize    int
}

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
	s.workspaceFilePickerContext = ctx
	s.workspaceFilePickerCancel = cancel
	s.SetState(func() {
		s.workspaceFilePicker.reset()
		s.workspaceFilePickerScroll = ui.ScrollController{}
		s.workspaceFilePickerRevealPending = false
	})
	s.configureWorkspaceFilePickerScheduler(files.WorkspaceLimits())
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
	files, ok := s.bound.(sessionclient.WorkspaceFilesSession)
	if !ok || !s.workspaceFilePicker.Open {
		return
	}
	if s.workspaceFilePickerCancel != nil {
		s.workspaceFilePickerCancel()
	}
	ctx, cancel := context.WithCancel(s.attachmentCtx)
	s.workspaceFilePickerContext = ctx
	s.workspaceFilePickerCancel = cancel
	s.SetState(func() {
		s.workspaceFilePicker.Generation++
		s.workspaceFilePicker.Workspace = protocol.WorkspaceRef{}
		s.workspaceFilePicker.activeRevision = make(map[workspaceFileKey]string)
		s.workspaceFilePicker.observations = make(map[directoryObservationKey]workspaceFilePickerObservation)
		s.workspaceFilePicker.loading = make(map[workspaceFileKey]uint64)
		s.workspaceFilePicker.errors = make(map[workspaceFileKey]string)
		s.workspaceFilePicker.workspaceError = false
	})
	s.configureWorkspaceFilePickerScheduler(files.WorkspaceLimits())
	s.loadWorkspaceFilePickerRef(ctx, files, s.workspaceFilePicker.Generation)
}

func (s *appState) closeWorkspaceFilePicker() {
	if s.workspaceFilePickerCancel != nil {
		s.workspaceFilePickerCancel()
		s.workspaceFilePickerCancel = nil
	}
	s.workspaceFilePickerContext = nil
	s.workspaceFilePickerQueue = nil
	s.workspaceFilePickerActive = 0
	s.workspaceFilePickerMaxActive = 0
	s.workspaceFilePickerMaxPending = 0
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
		s.workspaceFilePicker.Workspace.WorkspaceID != "pending" && s.workspaceFilePicker.Workspace.WorkspaceID != workspace.WorkspaceID
	if changed || pickerChanged {
		s.closeWorkspaceFilePicker()
	}
}

func (s *appState) loadWorkspaceFilePickerRef(ctx context.Context, files sessionclient.WorkspaceFilesSession, generation uint64) {
	runtime := s.Context().Runtime()
	go func() {
		workspace, err := files.Workspace(ctx)
		if ctx.Err() != nil || s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if !s.workspaceFilePicker.Open || s.workspaceFilePicker.Generation != generation {
				return
			}
			if err != nil {
				s.SetState(func() {
					s.workspaceFilePicker.Workspace = protocol.WorkspaceRef{WorkspaceID: "pending"}
					s.workspaceFilePicker.workspaceError = true
					root := workspaceFileKey{WorkspaceID: "pending"}
					s.workspaceFilePicker.errors[root] = workspaceFilePickerErrorText(err)
				})
				return
			}
			if s.workspaceID != "" && s.workspaceID != workspace.WorkspaceID {
				s.SetState(func() { s.closeWorkspaceFilePicker() })
				return
			}
			s.SetState(func() {
				s.workspaceID = workspace.WorkspaceID
				s.workspaceFilePicker.setWorkspace(workspace)
			})
			if workspace.State != protocol.WorkspaceReady {
				s.SetState(func() {
					root := workspaceFileKey{WorkspaceID: workspace.WorkspaceID}
					s.workspaceFilePicker.errors[root] = "Workspace is unavailable"
				})
				return
			}
			paths := s.workspaceFilePicker.expandedPaths()
			if len(paths) == 0 {
				paths = []string{""}
			}
			for _, path := range paths {
				s.loadWorkspaceFilePickerDirectory(path, "")
			}
		})
	}()
}

func workspaceFilePickerConcurrency(limits protocol.WorkspaceLimits) (active, pending int) {
	ceilings := protocol.DefaultWorkspaceLimits()
	active, pending = limits.MaxActiveRequests, limits.MaxPendingRequests
	if active <= 0 {
		return ceilings.MaxActiveRequests, ceilings.MaxPendingRequests
	}
	if pending < 0 {
		pending = ceilings.MaxPendingRequests
	}
	active = min(active, ceilings.MaxActiveRequests)
	pending = min(pending, ceilings.MaxPendingRequests)
	return active, pending
}

func (s *appState) configureWorkspaceFilePickerScheduler(limits protocol.WorkspaceLimits) {
	s.workspaceFilePickerMaxActive, s.workspaceFilePickerMaxPending = workspaceFilePickerConcurrency(limits)
	s.workspaceFilePickerActive = 0
	s.workspaceFilePickerQueue = nil
}

func (s *appState) requestWorkspaceFilePickerDirectory(path, cursor string) {
	if s.filePickerLoadHook != nil {
		s.filePickerLoadHook(path, cursor)
		return
	}
	s.loadWorkspaceFilePickerDirectory(path, cursor)
}

func (s *appState) loadWorkspaceFilePickerDirectory(path, cursor string) {
	files, ok := s.bound.(sessionclient.WorkspaceFilesSession)
	if !ok || !s.workspaceFilePicker.Open || s.workspaceFilePicker.Workspace.WorkspaceID == "" || s.workspaceFilePickerMaxActive == 0 {
		return
	}
	var job workspaceFilePickerJob
	s.SetState(func() {
		job.key, job.serial = s.workspaceFilePicker.beginLoad(path, cursor)
		job.generation = s.workspaceFilePicker.Generation
		job.workspaceID = s.workspaceFilePicker.Workspace.WorkspaceID
		job.path, job.cursor = path, cursor
		job.pageSize = files.WorkspaceLimits().DefaultDirectoryPageSize
		if job.pageSize <= 0 || job.pageSize > protocol.MaxDirectoryPageSize {
			job.pageSize = protocol.DefaultWorkspaceLimits().DefaultDirectoryPageSize
		}
	})
	if s.workspaceFilePickerActive < s.workspaceFilePickerMaxActive {
		s.startWorkspaceFilePickerJob(files, job)
		return
	}
	if len(s.workspaceFilePickerQueue) < s.workspaceFilePickerMaxPending {
		s.workspaceFilePickerQueue = append(s.workspaceFilePickerQueue, job)
		return
	}
	s.SetState(func() {
		s.workspaceFilePicker.applyError(job.key, job.serial, &protocol.WorkspaceError{
			Code: protocol.WorkspaceErrorCapacity, Message: "Too many directory requests", Details: map[string]string{"scope": "session"},
		})
	})
}

func (s *appState) startWorkspaceFilePickerJob(files sessionclient.WorkspaceFilesSession, job workspaceFilePickerJob) {
	ctx := s.workspaceFilePickerContext
	if ctx == nil {
		return
	}
	s.workspaceFilePickerActive++
	runtime := s.Context().Runtime()
	go func() {
		page, err := files.ListDirectory(ctx, protocol.ListDirectoryInput{
			WorkspaceID: job.workspaceID, Path: job.path, PageSize: job.pageSize, Cursor: job.cursor,
		})
		if ctx.Err() != nil || s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() { s.deliverWorkspaceFilePickerJob(files, job, page, err) })
	}()
}

func (s *appState) deliverWorkspaceFilePickerJob(files sessionclient.WorkspaceFilesSession, job workspaceFilePickerJob, page protocol.DirectoryPage, err error) {
	s.releaseWorkspaceFilePickerJob(job)
	s.completeWorkspaceFilePickerJob(job, page, err)
	s.startNextWorkspaceFilePickerJob(files, job.generation)
}

func (s *appState) releaseWorkspaceFilePickerJob(job workspaceFilePickerJob) {
	if job.generation == s.workspaceFilePicker.Generation && s.workspaceFilePickerActive > 0 {
		s.workspaceFilePickerActive--
	}
}

func (s *appState) startNextWorkspaceFilePickerJob(files sessionclient.WorkspaceFilesSession, generation uint64) {
	if generation != s.workspaceFilePicker.Generation || len(s.workspaceFilePickerQueue) == 0 || s.workspaceFilePickerActive >= s.workspaceFilePickerMaxActive {
		return
	}
	next := s.workspaceFilePickerQueue[0]
	s.workspaceFilePickerQueue = s.workspaceFilePickerQueue[1:]
	s.startWorkspaceFilePickerJob(files, next)
}

func (s *appState) completeWorkspaceFilePickerJob(job workspaceFilePickerJob, page protocol.DirectoryPage, err error) {
	if !s.workspaceFilePickerRequestCurrent(job.key, job.serial, job.generation) {
		return
	}
	if err != nil {
		switch workspaceFilePickerErrorCode(err) {
		case protocol.WorkspaceErrorStaleWorkspace:
			s.requestWorkspaceFilePickerRefresh()
			return
		case protocol.WorkspaceErrorStaleCursor:
			if job.cursor != "" {
				s.requestWorkspaceFilePickerDirectory(job.path, "")
				return
			}
		}
		s.SetState(func() { s.workspaceFilePicker.applyError(job.key, job.serial, err) })
		return
	}
	if job.cursor != "" && s.workspaceFilePicker.activeRevision[job.key] != page.Revision {
		s.SetState(func() { delete(s.workspaceFilePicker.loading, job.key) })
		s.requestWorkspaceFilePickerDirectory(job.path, "")
		return
	}
	s.SetState(func() {
		s.workspaceFilePicker.applyPage(job.key, job.serial, job.cursor, page)
		s.requestWorkspaceFilePickerReveal()
	})
}

func (s *appState) workspaceFilePickerRequestCurrent(key workspaceFileKey, serial, generation uint64) bool {
	return s.workspaceFilePicker.Open && s.workspaceFilePicker.Generation == generation && s.workspaceFilePicker.loading[key] == serial
}

func (s *appState) requestWorkspaceFilePickerReveal() {
	rows := s.workspaceFilePicker.rows()
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

func workspaceFilePickerErrorCode(err error) protocol.WorkspaceErrorCode {
	var workspaceErr *protocol.WorkspaceError
	if errors.As(err, &workspaceErr) {
		return workspaceErr.Code
	}
	return ""
}

func (s *appState) workspaceFilePickerActivationCurrent(row workspaceFilePickerRow) bool {
	return row.Key.WorkspaceID == s.workspaceID && row.Key.WorkspaceID == s.workspaceFilePicker.Workspace.WorkspaceID
}

func (s *appState) activateWorkspaceFilePickerRow(row workspaceFilePickerRow) {
	if !s.workspaceFilePicker.Open || !workspaceFilePickerRowSelectable(row) {
		return
	}
	if row.Kind == workspaceFilePickerErrorRow && s.workspaceFilePicker.workspaceError {
		s.requestWorkspaceFilePickerRefresh()
		return
	}
	if !s.workspaceFilePickerActivationCurrent(row) {
		s.SetState(func() { s.closeWorkspaceFilePicker() })
		s.showToast(toastInput{Title: "Workspace changed", Subtitle: "Reopen the file picker for the current workspace.", Variant: toastWarning})
		return
	}
	switch row.Kind {
	case workspaceFilePickerMoreRow:
		if observation, ok := s.workspaceFilePicker.observation(row.Key); ok && observation.NextCursor != "" {
			s.loadWorkspaceFilePickerDirectory(row.Key.Path, observation.NextCursor)
		}
	case workspaceFilePickerErrorRow:
		s.loadWorkspaceFilePickerDirectory(row.Key.Path, "")
	case workspaceFilePickerEntryRow:
		switch row.Entry.Kind {
		case protocol.WorkspaceEntryDirectory:
			load := false
			s.SetState(func() { load = s.workspaceFilePicker.toggleDirectory(row.Entry) })
			if load {
				s.loadWorkspaceFilePickerDirectory(row.Entry.Path, "")
			}
		case protocol.WorkspaceEntryFile:
			descriptor := fileWorkspacePane(row.Key.WorkspaceID, row.Entry.Path)
			var openErr error
			s.SetState(func() {
				_, _, openErr = s.workspace.Open(descriptor)
				if openErr == nil {
					s.closeWorkspaceFilePicker()
					s.syncWorkspaceSelection()
				}
			})
			if openErr != nil {
				s.showToast(toastInput{Title: "Could not open file", Subtitle: openErr.Error(), Variant: toastWarning})
			}
		default:
			s.showToast(toastInput{Title: "Cannot open entry", Subtitle: "Only regular files can be opened.", Variant: toastWarning})
		}
	}
}
