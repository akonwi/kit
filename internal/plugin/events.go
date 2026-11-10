package plugin

import (
	"encoding/json"
	"fmt"

	"github.com/akonwi/kit/api/pluginprotocol"
)

const (
	turnStartedMethod     = "kit/events/agent.turn.started"
	turnCompletedMethod   = "kit/events/agent.turn.completed"
	maxTurnEventFrameSize = 256 << 10
)

// The public turn records are owned by the protocol package.
type (
	// PublicTextContent is the text-only content exposed by turn events.
	PublicTextContent = pluginprotocol.TextContent
	// PublicMessage is a text-only user or assistant message.
	PublicMessage = pluginprotocol.Message
	// PublicTurn is the completed public projection of one agent turn.
	PublicTurn = pluginprotocol.Turn
)

type turnStartedParams = pluginprotocol.TurnStartedParams

type turnCompletedParams = pluginprotocol.TurnCompletedParams

// TurnStarted publishes one live turn-start event to generations ready at the
// canonical creation boundary. Later or replacement generations do not inherit it.
func (h *Host) TurnStarted(turnID string) bool {
	params, err := json.Marshal(turnStartedParams{SessionID: h.config.Session.ID, TurnID: turnID})
	if err != nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	delivered := false
	for _, entry := range h.entries {
		if !h.currentReadyEntryLocked(entry) {
			continue
		}
		if err := entry.instance.tryNotify(turnStartedMethod, params); err != nil {
			entry.instance.beginStop("runtime", err)
			continue
		}
		if entry.turns == nil {
			entry.turns = make(map[string]struct{})
		}
		entry.turns[turnID] = struct{}{}
		delivered = true
	}
	return delivered
}

// TurnCompleted publishes the terminal text projection only to the same live
// generations that received TurnStarted. Oversized events are omitted intact.
func (h *Host) TurnCompleted(turn PublicTurn, omitReason string) {
	params, err := json.Marshal(turnCompletedParams{SessionID: h.config.Session.ID, Turn: turn})
	if err != nil {
		omitReason = "could not be encoded"
	}
	if omitReason == "" {
		encoded, encodeErr := json.Marshal(requestMessage(nil, turnCompletedMethod, params))
		if encodeErr != nil || len(encoded) > maxTurnEventFrameSize {
			omitReason = "exceeds the native 256 KiB turn-event limit"
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	hadEligible := false
	for _, entry := range h.entries {
		if _, eligible := entry.turns[turn.ID]; !eligible {
			continue
		}
		hadEligible = true
		delete(entry.turns, turn.ID)
		if omitReason != "" || !h.currentReadyEntryLocked(entry) {
			continue
		}
		if err := entry.instance.tryNotify(turnCompletedMethod, params); err != nil {
			entry.instance.beginStop("runtime", err)
		}
	}
	if omitReason != "" && hadEligible {
		h.recordDiagnosticLocked(fmt.Sprintf("turn %s completed event %s and was omitted", turn.ID, omitReason), false)
	}
}

func (h *Host) currentReadyEntryLocked(entry *hostEntry) bool {
	return entry != nil && entry.instance != nil && entry.epoch == h.view.epoch &&
		(entry.installation.Source == User || entry.projectEpoch == h.view.projectEpoch) &&
		entry.instance.Status().State == InstanceReady
}
