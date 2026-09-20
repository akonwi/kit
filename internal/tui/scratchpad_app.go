package tui

import (
	"context"
	"errors"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

const scratchpadAutosaveDelay = 250 * time.Millisecond

type scratchpadWriteOutcome struct {
	record protocol.Scratchpad
	err    error
}

func (s *appState) cacheScratchpadDraft(sessionID string) {
	if sessionID == "" {
		return
	}
	if s.scratchpad.Initialized && s.scratchpad.State != scratchpadSaved {
		if s.scratchpadDrafts == nil {
			s.scratchpadDrafts = make(map[string]scratchpadEditorState)
		}
		draft := s.scratchpad
		if draft.State == scratchpadSaving {
			draft.State = scratchpadUnsaved
		}
		s.scratchpadDrafts[sessionID] = draft
		return
	}
	delete(s.scratchpadDrafts, sessionID)
}

func (s *appState) restoreScratchpadDraft(sessionID string, record *protocol.Scratchpad) {
	s.scratchpad.reset(record)
	if draft, ok := s.scratchpadDrafts[sessionID]; ok {
		delete(s.scratchpadDrafts, sessionID)
		s.scratchpad = draft
		s.reconcileScratchpad(record)
	}
}

func (s *appState) openScratchpad() {
	if _, ok := s.bound.(sessionclient.ScratchpadSession); !ok {
		return
	}
	var err error
	s.SetState(func() {
		_, _, err = s.workspace.Open(scratchpadWorkspacePane())
		if err == nil {
			s.syncWorkspaceSelection()
		}
	})
	if err != nil {
		s.showToast(toastInput{Title: "Could not open scratchpad", Subtitle: err.Error(), Variant: toastWarning})
	}
}

func (s *appState) cancelScratchpadDebounce() {
	if s.scratchpadDebounceCancel != nil {
		s.scratchpadDebounceCancel()
		s.scratchpadDebounceCancel = nil
	}
}

func (s *appState) changeScratchpad(value string) {
	if !s.scratchpad.Initialized || value == s.scratchpad.Draft {
		return
	}
	s.cancelScratchpadDebounce()
	s.SetState(func() {
		s.scratchpad.edit(value)
		s.scratchpadClosePending = false
	})
	if s.scratchpad.State == scratchpadUnsaved {
		s.scheduleScratchpadSave()
	}
}

func (s *appState) scheduleScratchpadSave() {
	if s.scratchpad.State != scratchpadUnsaved || s.scratchpadWritePending {
		return
	}
	parent := s.attachmentCtx
	if parent == nil {
		parent = s.ctx
	}
	ctx, cancel := context.WithCancel(parent)
	s.scratchpadDebounceCancel = cancel
	dispatch := s.scratchpadDispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	generation := s.scratchpadSaveGeneration
	go func() {
		timer := time.NewTimer(scratchpadAutosaveDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			dispatch(func() {
				if ctx.Err() == nil && generation == s.scratchpadSaveGeneration {
					s.scratchpadDebounceCancel = nil
					s.saveScratchpad(false, nil)
				}
			})
		}
	}()
}

// saveScratchpad starts one serialized compare-and-swap. An explicit expected
// record is used by conflict replacement so the reviewed revision is guarded.
func (s *appState) saveScratchpad(closeAfter bool, expected *protocol.Scratchpad) {
	service, ok := s.bound.(sessionclient.ScratchpadSession)
	if !ok || !s.scratchpad.Initialized {
		return
	}
	if s.scratchpadWritePending {
		s.scratchpadClosePending = s.scratchpadClosePending || closeAfter
		return
	}
	if expected == nil && s.scratchpad.State == scratchpadConflict {
		return
	}
	record := s.scratchpad.Authoritative
	if expected != nil {
		record = *expected
	}
	draft := s.scratchpad.Draft
	if draft == record.Content {
		s.SetState(func() {
			s.scratchpad.Authoritative = record
			s.scratchpad.State = scratchpadSaved
			s.scratchpad.Conflict = nil
			s.scratchpad.Review = false
		})
		if closeAfter {
			s.closeScratchpadNow()
		}
		return
	}
	s.cancelScratchpadDebounce()
	s.scratchpadSaveGeneration++
	generation := s.scratchpadSaveGeneration
	bound := s.bound
	sessionID := s.session.ID
	done := make(chan scratchpadWriteOutcome, 1)
	s.SetState(func() {
		s.scratchpadWritePending = true
		s.scratchpadWriteContent = draft
		s.scratchpadWriteDone = done
		s.scratchpadClosePending = closeAfter
		s.scratchpad.State = scratchpadSaving
		s.scratchpad.Review = false
		s.scratchpad.Failure = ""
	})
	dispatch := s.scratchpadDispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	requestContext := s.attachmentCtx
	go func() {
		result, err := service.UpdateScratchpad(requestContext, protocol.UpdateScratchpadInput{ExpectedRevision: record.Revision, Content: draft})
		done <- scratchpadWriteOutcome{record: result, err: err}
		if requestContext.Err() != nil {
			return
		}
		dispatch(func() {
			if generation != s.scratchpadSaveGeneration || s.bound != bound || s.session.ID != sessionID {
				return
			}
			effectiveResult, effectiveErr := result, err
			if effectiveErr != nil && s.scratchpad.Authoritative.Revision > record.Revision && s.scratchpad.Authoritative.Content == draft {
				effectiveResult, effectiveErr = s.scratchpad.Authoritative, nil
			}
			if effectiveErr != nil {
				var scratchErr *protocol.ScratchpadError
				if errors.As(effectiveErr, &scratchErr) && scratchErr.Code == protocol.ScratchpadRevisionConflict && scratchErr.Current != nil && scratchErr.Current.Content == draft {
					effectiveResult, effectiveErr = *scratchErr.Current, nil
				}
			}
			closeCommitted := false
			continueClose := false
			s.SetState(func() {
				if effectiveErr != nil {
					s.scratchpadWritePending = false
					s.scratchpadWriteContent = ""
					s.scratchpadWriteDone = nil
					var scratchErr *protocol.ScratchpadError
					if errors.As(effectiveErr, &scratchErr) && scratchErr.Code == protocol.ScratchpadRevisionConflict && scratchErr.Current != nil {
						s.scratchpad.reconcile(*scratchErr.Current)
						s.scratchpad.Review = true
					} else {
						s.scratchpad.State = scratchpadSaveFailed
						s.scratchpad.Failure = effectiveErr.Error()
					}
					s.scratchpadClosePending = false
					return
				}
				sameDraft := s.scratchpad.Draft == draft
				s.reconcileScratchpad(&effectiveResult)
				s.scratchpadWritePending = false
				s.scratchpadWriteContent = ""
				s.scratchpadWriteDone = nil
				if s.scratchpad.Authoritative.Content == s.scratchpad.Draft {
					s.scratchpad.State = scratchpadSaved
					s.scratchpad.Conflict = nil
					s.scratchpad.Review = false
					closeCommitted = sameDraft && s.scratchpadClosePending
				} else if s.scratchpad.State != scratchpadConflict {
					s.scratchpad.State = scratchpadUnsaved
					continueClose = s.scratchpadClosePending
				}
				s.scratchpadClosePending = false
			})
			if closeCommitted {
				s.closeScratchpadNow()
			} else if continueClose {
				s.saveScratchpad(true, nil)
			} else if s.scratchpad.State == scratchpadUnsaved {
				s.scheduleScratchpadSave()
			}
		})
	}()
}

func (s *appState) closeScratchpad() {
	switch s.scratchpad.State {
	case scratchpadUnsaved, scratchpadSaveFailed:
		s.saveScratchpad(true, nil)
	case scratchpadSaving:
		s.scratchpadClosePending = true
	case scratchpadConflict:
		s.SetState(func() { s.scratchpad.Review = true })
	default:
		s.closeScratchpadNow()
	}
}

func (s *appState) closeScratchpadNow() {
	s.SetState(func() {
		s.workspace.Close(workspacePaneIdentity("scratchpad"))
		s.syncWorkspaceSelection()
	})
}

func (s *appState) useSharedScratchpad() {
	s.cancelScratchpadDebounce()
	s.SetState(func() { s.scratchpad.useShared() })
}

func (s *appState) replaceSharedScratchpad() {
	if s.scratchpad.Conflict == nil {
		return
	}
	expected := *s.scratchpad.Conflict
	s.saveScratchpad(false, &expected)
}

func (s *appState) bestEffortSaveScratchpad(timeout time.Duration) {
	service, ok := s.bound.(sessionclient.ScratchpadSession)
	if !ok || !s.scratchpad.Initialized || s.scratchpad.Conflict != nil || s.scratchpad.Draft == s.scratchpad.Authoritative.Content {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	base := s.scratchpad.Authoritative
	if s.scratchpadWritePending && s.scratchpadWriteDone != nil {
		select {
		case outcome := <-s.scratchpadWriteDone:
			if outcome.err == nil {
				base = outcome.record
			} else if s.scratchpad.Authoritative.Content == s.scratchpadWriteContent {
				base = s.scratchpad.Authoritative
			} else {
				var scratchErr *protocol.ScratchpadError
				if !errors.As(outcome.err, &scratchErr) || scratchErr.Code != protocol.ScratchpadRevisionConflict || scratchErr.Current == nil || scratchErr.Current.Content != s.scratchpadWriteContent {
					return
				}
				base = *scratchErr.Current
			}
		case <-ctx.Done():
			return
		}
	}
	if s.scratchpad.Draft == base.Content {
		return
	}
	_, _ = service.UpdateScratchpad(ctx, protocol.UpdateScratchpadInput{
		ExpectedRevision: base.Revision,
		Content:          s.scratchpad.Draft,
	})
}

func (s *appState) reconcileScratchpad(record *protocol.Scratchpad) {
	if record == nil {
		return
	}
	if s.scratchpadWritePending && record.Content == s.scratchpadWriteContent && (!s.scratchpad.Initialized || record.Revision > s.scratchpad.Authoritative.Revision) {
		s.scratchpad.Authoritative = *record
		s.scratchpad.Conflict = nil
		s.scratchpad.Review = false
		s.scratchpad.Failure = ""
		if s.scratchpad.Draft == record.Content {
			s.scratchpad.State = scratchpadSaved
		} else {
			s.scratchpad.State = scratchpadUnsaved
		}
		return
	}
	previous := s.scratchpad.State
	if !s.scratchpad.reconcile(*record) {
		return
	}
	if s.scratchpad.State == scratchpadConflict || s.scratchpad.State == scratchpadSaved {
		s.cancelScratchpadDebounce()
	}
	if previous == scratchpadSaving {
		// The response remains authoritative for its own request; this event only
		// updates the shared baseline and never starts a second writer.
		return
	}
}
