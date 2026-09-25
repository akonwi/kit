package tui

import (
	"strings"

	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
)

func (s *appState) recallMessageHistory() {
	if strings.TrimSpace(s.composer) != "" || s.followUps.Count > 0 || s.messageHistory.Open {
		return
	}
	pager, ok := s.bound.(sessionclient.MessagePager)
	if !ok || !s.admitRootModal() {
		return
	}
	bound, operation := s.bound, s.operation
	ctx, runtime := s.ctx, s.Context().Runtime()
	go func() {
		entries, err := loadMessageHistory(ctx, pager)
		runtime.Dispatch(func() {
			if s.operation != operation || s.bound != bound {
				return
			}
			if err != nil {
				if ctx.Err() == nil {
					s.showToast(toastInput{Title: "Could not load message history", Subtitle: err.Error(), Variant: toastError})
				}
				return
			}
			if len(entries) == 0 {
				s.showToast(toastInput{Title: "No message history", Variant: toastInfo})
				return
			}
			s.SetState(func() { s.messageHistory.OpenFor(entries) })
		})
	}()
}

func (s *appState) selectMessageHistory(_ ui.EventContext, messageID string) {
	entry, ok := s.messageHistory.Selected()
	if messageID != "" && (!ok || entry.ID != messageID) {
		for _, candidate := range s.messageHistory.Entries {
			if candidate.ID == messageID {
				entry, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return
	}
	s.SetState(func() {
		s.composer = entry.Text
		s.composerCursorEndGeneration++
		s.messageHistory.Close()
	})
}
