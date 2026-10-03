package tui

import (
	"context"
	"fmt"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
)

const turnAbortTimeout = 3 * time.Second

func (s *appState) abortRunWithDispatch(dispatch func(func()), timeout time.Duration) {
	if !s.turnPending || s.turnStopping {
		return
	}
	if s.prompt != nil {
		s.prompt.abort.Store(true)
	}
	s.SetState(func() {
		s.turnStopping = true
		s.turnActivity = "Stopping…"
		s.turnThinking = ""
	})
	s.requestRunAbort(dispatch, timeout)
}

// requestRunAbort also handles a cancellation requested before prompt admission.
// An accepted abort remains stopping until the authoritative run watcher settles it.
func (s *appState) requestRunAbort(dispatch func(func()), timeout time.Duration) {
	run, turnID, bound := s.activeTurn, s.activeTurnID, s.bound
	if turnID == "" || (run == nil && bound == nil) {
		return
	}
	s.turnAbortGeneration++
	generation, operation, sessionID := s.turnAbortGeneration, s.operation, s.session.ID
	admission := s.prompt
	base := s.attachmentCtx
	if base == nil {
		base = s.ctx
	}
	streamID := s.liveStreamID
	go func() {
		// Escape is an explicit server-work cancellation request. Detaching must
		// not withdraw it; only the recovery/UI work follows attachment lifetime.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(base), timeout)
		var err error
		if run != nil {
			err = run.Abort(ctx)
		} else {
			err = bound.Abort(ctx, turnID)
		}
		cancel()
		if err == nil || base.Err() != nil {
			return
		}

		var snapshot protocol.SessionSnapshot
		snapshotErr := fmt.Errorf("session status is unavailable")
		if bound != nil {
			ctx, cancel := context.WithTimeout(base, timeout)
			snapshot, snapshotErr = bound.Snapshot(ctx)
			cancel()
		}
		if base.Err() != nil {
			return
		}
		dispatch(func() {
			if base.Err() != nil || operation != s.operation || generation != s.turnAbortGeneration || s.bound != bound || s.prompt != admission || sessionID != s.session.ID || turnID != s.activeTurnID || !s.turnPending || !s.turnStopping {
				return
			}
			// Do not replace evidence with a snapshot older than the live event stream,
			// or with an old stream after a concurrent reconnect has changed identity.
			fresh := snapshotErr == nil && snapshot.Session.ID == sessionID &&
				(s.liveStreamID == streamID || snapshot.EventStreamID == s.liveStreamID) &&
				(snapshot.EventStreamID != s.liveStreamID || snapshot.EventCursor >= s.liveSequence)
			nextTurnID := turnID
			s.SetState(func() {
				s.turnStopping = false
				if s.prompt != nil {
					s.prompt.abort.Store(false)
				}
				if fresh && snapshot.ActiveTurnID != turnID {
					s.markTerminalRunSettled(turnID)
					s.applySnapshot(snapshot)
					s.activeTurn = nil
					s.prompt = nil
					nextTurnID = snapshot.ActiveTurnID
				} else {
					if fresh {
						s.applySessionMetadataSnapshot(snapshot)
						s.pendingInteractions = append([]protocol.InteractionRequest(nil), snapshot.PendingInteractions...)
						s.agentFeedbackPending = len(s.pendingInteractions) > 0
						s.reconcileInputOwner()
						s.providerRetry = cloneProviderRetry(snapshot.ProviderRetry)
						s.activeCompactionID = ""
						if snapshot.ActiveCompaction != nil && snapshot.ActiveCompaction.TurnID == turnID {
							s.activeCompactionID = snapshot.ActiveCompaction.ID
						}
					}
					// Keep the existing transcript and watcher when this run is still active
					// or status could not be confirmed. A timeout does not prove cancellation.
					s.turnActivity = "Working…"
					if s.agentFeedbackPending {
						s.turnActivity = "Waiting for feedback…"
					} else if s.activeCompactionID != "" {
						s.turnActivity = "Compacting session…"
					}
				}
			})
			if fresh && nextTurnID == "" {
				return // It finished despite the failed RPC.
			}
			detail := fmt.Sprintf("Press Esc to retry. %v. Ctrl+C detaches without stopping the run.", err)
			if !fresh {
				reason := "a newer activity update arrived"
				if snapshotErr != nil {
					reason = snapshotErr.Error()
				}
				detail = fmt.Sprintf("Press Esc to retry.\n%v.\nRun status is unconfirmed: %s.", err, reason)
			}
			if nextTurnID != turnID {
				detail = "The previous run finished; a new run is active. Press Esc to stop the current run."
				s.watchSession(bound, operation, nextTurnID)
			}
			s.showToast(toastInput{Title: "Abort failed", Subtitle: detail, Variant: toastError, Persistent: true})
		})
	}()
}
