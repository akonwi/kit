package tui

import (
	"context"
	"errors"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
)

const fileIndexRefreshInterval = 5 * time.Minute

// indexedFileSource is the app-scoped indexed project-path state shared by all
// file pickers for the attached session.
type indexedFileSource struct {
	SessionID  string
	CWD        string
	Entries    []protocol.FileIndexEntry
	Loading    bool
	Error      string
	Truncated  bool
	indexedAt  time.Time
	generation uint64
	cancel     context.CancelFunc
}

func (source *indexedFileSource) reset() {
	if source.cancel != nil {
		source.cancel()
	}
	generation := source.generation + 1
	*source = indexedFileSource{generation: generation}
}

func (source *indexedFileSource) begin(sessionID, cwd string, parent context.Context) (context.Context, uint64) {
	if source.cancel != nil {
		source.cancel()
	}
	source.generation++
	ctx, cancel := context.WithCancel(parent)
	source.SessionID, source.CWD = sessionID, cwd
	source.Loading, source.Error = true, ""
	source.cancel = cancel
	return ctx, source.generation
}

func (source indexedFileSource) shouldLoad(sessionID, cwd string, force bool, now time.Time) bool {
	if source.SessionID != sessionID || source.CWD != cwd {
		return true
	}
	if force {
		return true
	}
	if source.Loading {
		return false
	}
	return source.Entries == nil || now.Sub(source.indexedAt) >= fileIndexRefreshInterval
}

func (s *appState) ensureIndexedFiles(runtime ui.Runtime, force bool) {
	sessionID, cwd, bound := s.session.ID, s.session.CWD, s.bound
	if sessionID == "" || cwd == "" || bound == nil || s.attachmentCtx == nil {
		return
	}
	var ctx context.Context
	var generation uint64
	started := false
	s.SetState(func() {
		if !s.indexedFiles.shouldLoad(sessionID, cwd, force, time.Now()) {
			return
		}
		if s.indexedFiles.SessionID != sessionID || s.indexedFiles.CWD != cwd {
			s.indexedFiles.reset()
		}
		ctx, generation = s.indexedFiles.begin(sessionID, cwd, s.attachmentCtx)
		started = true
	})
	if !started {
		return
	}
	go func() {
		index, err := requestSessionFileIndex(ctx, bound, force)
		runtime.Dispatch(func() {
			if !s.acceptsIndexedFiles(sessionID, cwd, generation) {
				return
			}
			s.SetState(func() {
				s.indexedFiles.Loading = false
				s.indexedFiles.cancel = nil
				if err != nil {
					if !errors.Is(err, context.Canceled) {
						s.indexedFiles.Error = err.Error()
					}
					return
				}
				if index.SessionID != sessionID || index.CWD != cwd {
					s.indexedFiles.Error = "File index identity changed"
					return
				}
				s.indexedFiles.Entries = append([]protocol.FileIndexEntry{}, index.Entries...)
				s.indexedFiles.Truncated = index.Truncated
				s.indexedFiles.Error = ""
				s.indexedFiles.indexedAt = time.Now()
				s.fileMention.ensureSelection(s.indexedFiles.Entries)
				s.workspaceFilePicker.ensureSelection(s.indexedFiles)
			})
		})
	}()
}

func requestSessionFileIndex(ctx context.Context, bound sessionclient.Session, force bool) (protocol.SessionFileIndex, error) {
	if refresher, ok := bound.(sessionclient.FileIndexRefreshSession); force && ok {
		return refresher.RefreshFileIndex(ctx)
	}
	return bound.FileIndex(ctx)
}

func (s *appState) acceptsIndexedFiles(sessionID, cwd string, generation uint64) bool {
	return s.session.ID == sessionID && s.session.CWD == cwd && s.bound != nil && s.bound.ID() == sessionID &&
		s.indexedFiles.SessionID == sessionID && s.indexedFiles.CWD == cwd && s.indexedFiles.generation == generation
}

func (s *appState) invalidateIndexedFiles() {
	s.fileMention.Close()
	if s.workspaceFilePicker.Open {
		s.closeWorkspaceFilePicker()
	}
	s.indexedFiles.reset()
}
