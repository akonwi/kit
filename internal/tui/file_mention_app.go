package tui

import "go.rockorager.dev/vaxis/ui"

func (s *appState) loadFileMentions(runtime ui.Runtime) {
	s.ensureIndexedFiles(runtime, false)
}

func (s *appState) refreshFileIndex(runtime ui.Runtime) {
	s.ensureIndexedFiles(runtime, true)
}

func (s *appState) invalidateFileMentions() {
	s.invalidateIndexedFiles()
}

func (s *appState) selectFileMention(_ ui.EventContext, path string) {
	entry, ok := s.fileMention.Selected(s.indexedFiles.Entries)
	if path != "" && (!ok || entry.Path != path) {
		for _, candidate := range s.indexedFiles.Entries {
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
