package tui

import (
	"context"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
)

func (s *appState) loadSessionMentions(runtime ui.Runtime) {
	s.requestSessionMentions(runtime, s.Widget().(app).Options.Server)
}

func (s *appState) requestSessionMentions(runtime ui.Runtime, server sessionclient.Server) {
	if s.sessionMentionCancel != nil {
		s.sessionMentionCancel()
	}
	sessionID := s.session.ID
	attachmentContext := s.attachmentCtx
	s.SetState(func() {
		s.sessionMentions.generation++
		s.sessionMentions.Entries = nil
		s.sessionMentions.Loading = true
		s.sessionMentions.Error = ""
	})
	generation := s.sessionMentions.generation
	ctx, cancel := context.WithTimeout(attachmentContext, 10*time.Second)
	s.sessionMentionCancel = cancel
	go func() {
		defer cancel()
		entries, err := server.ListSessions(ctx, "")
		if attachmentContext.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if !s.acceptsSessionMentions(sessionID, generation) {
				return
			}
			s.SetState(func() {
				s.sessionMentions.Loading = false
				if err != nil {
					s.sessionMentions.Error = err.Error()
					return
				}
				s.sessionMentions.Entries = mentionableSessions(entries, sessionID)
				s.sessionMention.ensureSelection(s.sessionMentions.Entries)
			})
		})
	}()
}
func (s *appState) acceptsSessionMentions(sessionID string, generation uint64) bool {
	return s.session.ID == sessionID && s.sessionMention.Open && s.sessionMentions.generation == generation
}
func mentionableSessions(entries []protocol.SessionInfo, activeID string) []protocol.SessionInfo {
	result := make([]protocol.SessionInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.ID != activeID {
			result = append(result, entry)
		}
	}
	return result
}
func (s *appState) selectSessionMention(_ ui.EventContext, id string) {
	var entry protocol.SessionInfo
	found := false
	for _, candidate := range s.sessionMention.filtered(s.sessionMentions.Entries) {
		if candidate.ID == id {
			entry = candidate
			found = true
			break
		}
	}
	if !found || id == s.session.ID {
		return
	}
	s.SetState(func() {
		composer, cursor, inserted := s.sessionMention.Insert(s.composer, entry)
		if !inserted {
			return
		}
		s.closeSessionMention()
		s.composer = composer
		s.composerCursorOffset = cursor
		s.composerCursorGeneration++
	})
}

func (s *appState) closeSessionMention() {
	s.sessionMention.Close()
	if s.sessionMentionCancel != nil {
		s.sessionMentionCancel()
		s.sessionMentionCancel = nil
		s.sessionMentions.generation++
	}
	s.sessionMentions.Loading = false
}

// Higher-priority surfaces take ownership of composer input. Run during the
// existing rebuild so a transient mention never survives behind a modal.
func (s *appState) reconcileSessionMention() {
	if !s.sessionMention.Open {
		return
	}
	if s.phase != phaseReady || s.palette.Open || s.themePicker.Open || s.bashHistory.Open || s.messageHistory.Open ||
		s.configurationPicker.Mode != configurationPickerClosed || s.sessionDetailsOpen ||
		s.annotationPicker.Open || s.sessionRename.Open || s.sessionExplorer.Open ||
		s.workspaceFilePicker.Open || s.workspacePickerOpen || s.subagentsOpen ||
		s.subagentDismissID != "" || len(s.pendingInteractions) > 0 {
		s.closeSessionMention()
	}
}
