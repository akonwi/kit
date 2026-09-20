package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/akonwi/kit/internal/protocol"
)

const runAbortTimeout = 3 * time.Second

func (s *appState) abortRunWithDispatch(dispatch func(func()), timeout time.Duration) {
	if !s.runPending || s.runStopping {
		return
	}
	if s.prompt != nil {
		s.prompt.abort.Store(true)
	}
	s.SetState(func() {
		s.runStopping = true
		s.turnActivity = "Stopping…"
		s.turnThinking = ""
		s.status = "esc abort · ctrl+c detach"
	})
	s.requestRunAbort(dispatch, timeout)
}

// requestRunAbort also handles a cancellation requested before prompt admission.
// An accepted abort remains stopping until the authoritative run watcher settles it.
func (s *appState) requestRunAbort(dispatch func(func()), timeout time.Duration) {
	run, runID, bound := s.activeRun, s.activeRunID, s.bound
	if runID == "" || (run == nil && bound == nil) {
		return
	}
	s.runAbortGeneration++
	generation, operation, sessionID := s.runAbortGeneration, s.operation, s.session.ID
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
			err = bound.Abort(ctx, runID)
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
			if base.Err() != nil || operation != s.operation || generation != s.runAbortGeneration || s.bound != bound || s.prompt != admission || sessionID != s.session.ID || runID != s.activeRunID || !s.runPending || !s.runStopping {
				return
			}
			// Do not replace evidence with a snapshot older than the live event stream,
			// or with an old stream after a concurrent reconnect has changed identity.
			fresh := snapshotErr == nil && snapshot.Session.ID == sessionID &&
				(s.liveStreamID == streamID || snapshot.EventStreamID == s.liveStreamID) &&
				(snapshot.EventStreamID != s.liveStreamID || snapshot.EventCursor >= s.liveSequence)
			nextRunID := runID
			s.SetState(func() {
				s.runStopping = false
				if s.prompt != nil {
					s.prompt.abort.Store(false)
				}
				if fresh && snapshot.ActiveRunID != runID {
					s.markTerminalRunSettled(runID)
					s.applySnapshot(snapshot)
					s.activeRun = nil
					s.prompt = nil
					nextRunID = snapshot.ActiveRunID
				} else {
					if fresh {
						s.applySessionMetadataSnapshot(snapshot)
						s.pendingInteractions = append([]protocol.InteractionRequest(nil), snapshot.PendingInteractions...)
						s.agentFeedbackPending = len(s.pendingInteractions) > 0
						s.reconcileInputOwner()
						s.providerRetry = cloneProviderRetry(snapshot.ProviderRetry)
						s.activeCompactionID = ""
						if snapshot.ActiveCompaction != nil && snapshot.ActiveCompaction.RunID == runID {
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
				if s.runPending {
					s.status = "esc abort · ctrl+c detach"
				}
			})
			if fresh && nextRunID == "" {
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
			if nextRunID != runID {
				detail = "The previous run finished; a new run is active. Press Esc to stop the current run."
				s.watchSession(bound, operation, nextRunID)
			}
			s.showToast(toastInput{Title: "Abort failed", Subtitle: detail, Variant: toastError})
		})
	}()
}
