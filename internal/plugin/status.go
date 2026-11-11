package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/api/pluginprotocol"
)

// The public status records are owned by the protocol package.
type (
	// SessionStatus is a session's model configuration, context, and usage.
	SessionStatus = pluginprotocol.SessionStatus
	// ParentSession identifies the session a session was started from.
	ParentSession = pluginprotocol.ParentSession
	// ContextStatus is a session's context use.
	ContextStatus = pluginprotocol.ContextStatus
)

// Compaction reports that a compaction of the session's conversation started
// or ended.
type Compaction struct {
	ID string
	// TurnID is empty for a compaction outside a turn.
	TurnID    string
	Completed bool
	// FailureReason is empty unless a completed compaction failed.
	FailureReason contract.CompactionFailureReason
}

const (
	statusChangedMethod       = "kit/events/session.status.changed"
	statusRequestMethod       = "kit/session/status"
	compactionStartedMethod   = "kit/events/compaction.started"
	compactionCompletedMethod = "kit/events/compaction.completed"
	// statusNotificationKey coalesces queued status notifications.
	statusNotificationKey = "session.status"
	// maxPendingCompactions bounds compaction events awaiting delivery.
	maxPendingCompactions = 16
)

// SetStatus records the session's latest status and wakes the host's status
// delivery. It takes only a leaf lock and never waits, so the session may
// call it while holding its own locks.
func (h *Host) SetStatus(status SessionStatus) {
	status = cloneSessionStatus(status)
	h.statusMu.Lock()
	h.status = &status
	h.statusMu.Unlock()
	h.signalStatus()
}

// CompactionChanged queues a compaction event for the host's status delivery.
// Like SetStatus it never waits. Events beyond a small bound are dropped and
// reported as a diagnostic.
func (h *Host) CompactionChanged(compaction Compaction) {
	h.statusMu.Lock()
	if len(h.compactions) < maxPendingCompactions {
		h.compactions = append(h.compactions, compaction)
	} else {
		h.droppedCompactions++
	}
	h.statusMu.Unlock()
	h.signalStatus()
}

// latestStatus returns a copy of the session's latest status, or nil.
func (h *Host) latestStatus() *SessionStatus {
	h.statusMu.Lock()
	defer h.statusMu.Unlock()
	if h.status == nil {
		return nil
	}
	status := cloneSessionStatus(*h.status)
	return &status
}

func (h *Host) signalStatus() {
	select {
	case h.statusWake <- struct{}{}:
	default:
	}
}

// statusLoop delivers status and compaction events to ready generations. It is
// the only sender of these notifications, so it never blocks the session.
func (h *Host) statusLoop() {
	defer close(h.statusDone)
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-h.statusWake:
			h.deliverStatus()
		}
	}
}

func (h *Host) deliverStatus() {
	h.statusMu.Lock()
	var latest *SessionStatus
	if h.status != nil {
		status := cloneSessionStatus(*h.status)
		latest = &status
	}
	compactions := h.compactions
	h.compactions = nil
	dropped := h.droppedCompactions
	h.droppedCompactions = 0
	h.statusMu.Unlock()

	var statusParams json.RawMessage
	if latest != nil {
		statusParams, _ = json.Marshal(latest)
	}
	events := make([]struct {
		method string
		params json.RawMessage
	}, 0, len(compactions))
	for _, compaction := range compactions {
		method, params, err := h.compactionNotification(compaction)
		if err == nil {
			events = append(events, struct {
				method string
				params json.RawMessage
			}{method, params})
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	if dropped > 0 {
		h.recordDiagnosticLocked(fmt.Sprintf("%d compaction events were not delivered to plugins", dropped), false)
	}
	for _, entry := range h.entries {
		if !h.currentReadyEntryLocked(entry) || entry.instance.version < 2 {
			continue
		}
		failed := false
		for _, event := range events {
			if err := entry.instance.tryNotify(event.method, event.params); err != nil {
				entry.instance.beginStop("runtime", err)
				failed = true
				break
			}
		}
		if failed || latest == nil || statusParams == nil || reflect.DeepEqual(entry.status, latest) {
			continue
		}
		if err := entry.instance.tryNotifyLatest(statusNotificationKey, statusChangedMethod, statusParams); err != nil {
			entry.instance.beginStop("runtime", err)
			continue
		}
		status := cloneSessionStatus(*latest)
		entry.status = &status
	}
}

func (h *Host) compactionNotification(compaction Compaction) (string, json.RawMessage, error) {
	var turn *string
	if compaction.TurnID != "" {
		id := compaction.TurnID
		turn = &id
	}
	if !compaction.Completed {
		params, err := json.Marshal(pluginprotocol.CompactionParams{SessionID: h.config.Session.ID, CompactionID: compaction.ID, TurnID: turn})
		return compactionStartedMethod, params, err
	}
	var reason *contract.CompactionFailureReason
	if compaction.FailureReason != "" {
		value := compaction.FailureReason
		reason = &value
	}
	params, err := json.Marshal(pluginprotocol.CompactionCompletedParams{SessionID: h.config.Session.ID, CompactionID: compaction.ID, TurnID: turn, FailureReason: reason})
	return compactionCompletedMethod, params, err
}

// handleStatusRequest answers kit/session/status with the latest status.
func (h *Host) handleStatusRequest(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	if protocolVersion(ctx) < 2 {
		return nil, rpcError(-32601, "Method not found")
	}
	if len(params) != 0 {
		return nil, rpcError(-32602, "Session status takes no params")
	}
	status := h.latestStatus()
	if status == nil {
		return nil, rpcError(-32603, "Session status is unavailable")
	}
	return json.Marshal(status)
}

func cloneSessionStatus(status SessionStatus) SessionStatus {
	if status.Parent != nil {
		parent := *status.Parent
		if parent.Name != nil {
			name := *parent.Name
			parent.Name = &name
		}
		status.Parent = &parent
	}
	if status.Context != nil {
		value := *status.Context
		status.Context = &value
	}
	return status
}
