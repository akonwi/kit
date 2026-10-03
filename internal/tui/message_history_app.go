package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

func (s *appState) recallMessageHistory() {
	if strings.TrimSpace(s.composer) != "" || s.followUps.Count > 0 || s.messageHistory.Open {
		return
	}
	// Nothing opens until history loads, and typing meanwhile belongs to the
	// composer, so admission waits for the load.
	pager, ok := s.bound.(MessagePager)
	if !ok || !s.canOpenRootModal() {
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
			// A dialog opened while loading keeps its input; otherwise the
			// picker opens filtered by whatever was typed meanwhile.
			if !s.admitRootModal() {
				return
			}
			s.SetState(func() { s.messageHistory.OpenFor(entries, s.composer) })
		})
	}()
}

// selectMessageHistory puts the entry with messageID into the composer, from
// Enter or a click.
func (s *appState) selectMessageHistory(_ ui.EventContext, messageID string) {
	var entry messageHistoryEntry
	ok := false
	for _, candidate := range s.messageHistory.Entries {
		if candidate.ID == messageID {
			entry, ok = candidate, true
			break
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
