package tui

import (
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

// handleCtrlC clears the focused composer before falling back to global detach.
// A draft behind a pane, modal, or interaction is not the active input control.
func (s *appState) handleCtrlC(ctx ui.EventContext, key ui.Key) ui.EventResult {
	if key.EventType == ui.EventRelease || key.EventType == vaxis.EventRepeat {
		return ui.EventHandled
	}
	control := captureInputControl(ctx)
	if s.phase == phaseReady && s.inputOwner().permitsRoot() && (s.composer != "" || len(s.composerAttachments) > 0 || len(s.composerAttachmentIDs) > 0) &&
		control != nil && control.mounted && !control.region.content && control.region.sessionID == s.session.ID &&
		!control.Widget().(controlFocusScope).Passive {
		s.SetState(func() {
			s.composer = ""
			s.composerDraftGeneration++
			s.composerAttachments = nil
			s.composerAttachmentIDs = nil
			s.composerCursorOffset = 0
			s.composerCursorGeneration++
			s.fileMention.Close()
			s.closeSessionMention()
			s.bashHistory.Close()
			s.messageHistory.Close()
		})
		return ui.EventHandled
	}
	ctx.Quit()
	return ui.EventHandled
}
