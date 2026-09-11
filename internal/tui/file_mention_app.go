package tui

import (
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

const fileIndexRefreshInterval = 5 * time.Minute

type cachedFileIndex struct {
	entries   []protocol.FileIndexEntry
	indexedAt time.Time
}

func fileIndexCacheKey(sessionID, cwd string) string { return sessionID + "\x00" + cwd }

func (s *appState) loadFileMentions(runtime ui.Runtime) {
	cwd := s.session.CWD
	sessionID := s.session.ID
	generation := s.fileMention.BeginLoad(cwd)
	cacheKey := fileIndexCacheKey(sessionID, cwd)
	if cached, ok := s.fileIndex[cacheKey]; ok && time.Since(cached.indexedAt) < fileIndexRefreshInterval {
		s.fileMention.Loaded(generation, cwd, cached.entries)
		return
	}
	ctx := s.ctx
	bound := s.bound
	go func() {
		index, err := bound.FileIndex(ctx)
		runtime.Dispatch(func() {
			accepted := s.acceptsFileIndex(sessionID, cwd, generation)
			if err != nil {
				if accepted {
					s.SetState(func() { s.fileMention.Close() })
					s.showToast(toastInput{Title: "File mentions", Subtitle: err.Error(), Variant: toastError})
				}
				return
			}
			if !accepted || index.SessionID != sessionID || index.CWD != cwd {
				return
			}
			s.SetState(func() {
				if s.fileIndex == nil {
					s.fileIndex = make(map[string]cachedFileIndex)
				}
				s.fileIndex[cacheKey] = cachedFileIndex{
					entries: append([]protocol.FileIndexEntry(nil), index.Entries...), indexedAt: time.Now(),
				}
				s.fileMention.Loaded(generation, cwd, index.Entries)
			})
		})
	}()
}

func (s *appState) acceptsFileIndex(sessionID, cwd string, generation uint64) bool {
	return s.session.ID == sessionID && s.session.CWD == cwd && s.bound != nil && s.bound.ID() == sessionID &&
		s.fileMention.Open && s.fileMention.generation == generation && s.fileMention.cwd == cwd
}

func (s *appState) refreshFileIndex(runtime ui.Runtime) {
	sessionID, cwd, bound := s.session.ID, s.session.CWD, s.bound
	if sessionID == "" || cwd == "" || bound == nil {
		return
	}
	go func() {
		index, err := bound.FileIndex(s.ctx)
		if err != nil {
			return
		}
		runtime.Dispatch(func() {
			if s.session.ID != sessionID || s.session.CWD != cwd || s.bound == nil || s.bound.ID() != sessionID || index.SessionID != sessionID || index.CWD != cwd {
				return
			}
			s.SetState(func() {
				if s.fileIndex == nil {
					s.fileIndex = make(map[string]cachedFileIndex)
				}
				s.fileIndex[fileIndexCacheKey(sessionID, cwd)] = cachedFileIndex{entries: append([]protocol.FileIndexEntry(nil), index.Entries...), indexedAt: time.Now()}
			})
		})
	}()
}

func (s *appState) invalidateFileMentions() {
	s.fileMention.Close()
	if s.session.ID != "" {
		delete(s.fileIndex, fileIndexCacheKey(s.session.ID, s.session.CWD))
	}
}

func (s *appState) selectFileMention(_ ui.EventContext, path string) {
	entry, ok := s.fileMention.Selected()
	if path != "" && (!ok || entry.Path != path) {
		for _, candidate := range s.fileMention.Entries {
			if candidate.Path == path {
				entry, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return
	}
	s.SetState(func() {
		composer, cursor, inserted := s.fileMention.Insert(s.composer, entry)
		if !inserted {
			return
		}
		s.composer = composer
		s.composerCursorOffset = cursor
		s.composerCursorGeneration++
	})
}
