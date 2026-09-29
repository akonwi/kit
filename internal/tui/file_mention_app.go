package tui

import (
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

func (s *appState) loadFileMentions(runtime ui.Runtime) {
	s.ensureIndexedFiles(runtime, false)
}

func (s *appState) refreshFileIndex(runtime ui.Runtime) {
	s.ensureIndexedFiles(runtime, true)
}

func (s *appState) invalidateFileMentions() {
	s.invalidateIndexedFiles()
}

// selectFileMention inserts the indexed file at path, from Enter or a click.
func (s *appState) selectFileMention(_ ui.EventContext, path string) {
	var entry protocol.FileIndexEntry
	found := false
	for _, candidate := range s.indexedFiles.Entries {
		if candidate.Path == path {
			entry, found = candidate, true
			break
		}
	}
	if !found {
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
