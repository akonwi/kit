package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/peer"
)

const maxPeerWorkers = 16

// StartPeerQueries reconciles interrupted work and starts bounded recipient scheduling.
func (m *Manager) StartPeerQueries(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.peerQueries == nil || m.peerStarted {
		m.mu.Unlock()
		return nil
	}
	m.peerStarted = true
	m.mu.Unlock()
	recovered, err := m.peerQueries.RecoverPeerQueries(ctx, time.Now().UTC(), m.peerLimits)
	if err != nil {
		m.mu.Lock()
		m.peerStarted = false
		m.mu.Unlock()
		return fmt.Errorf("recover peer queries: %w", err)
	}
	for _, request := range recovered {
		m.wakePeerQuery(request.RecipientSessionID)
	}
	terminal, err := m.peerQueries.TerminalPeerResults(ctx, 256)
	if err != nil {
		return fmt.Errorf("recover peer query results: %w", err)
	}
	for _, request := range terminal {
		m.deliverPeerResult(request)
	}
	m.ops.Add(1)
	go m.scanPeerQueries()
	return nil
}

func (m *Manager) scanPeerQueries() {
	defer m.ops.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(m.mailboxContext, 5*time.Second)
		ids, _ := m.peerQueries.PendingPeerRecipients(ctx, "", 128)
		for _, id := range ids {
			if !m.wakePeerQuery(id) {
				break
			}
		}
		cancel()
		select {
		case <-m.mailboxContext.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) wakePeerQuery(recipient string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	if worker := m.peerWorkers[recipient]; worker != nil {
		worker.dirty = true
		return true
	}
	if len(m.peerWorkers) >= maxPeerWorkers {
		return false
	}
	m.peerWorkers[recipient] = &mailboxReactionWorker{}
	m.ops.Add(1)
	go m.runPeerQueries(recipient)
	return true
}

func (m *Manager) runPeerQueries(recipient string) {
	defer m.ops.Done()
	defer func() { m.mu.Lock(); delete(m.peerWorkers, recipient); m.mu.Unlock() }()
	for {
		err := m.processOnePeerQuery(m.mailboxContext, recipient)
		if err != nil {
			return
		}
		m.mu.Lock()
		worker := m.peerWorkers[recipient]
		if worker != nil && worker.dirty {
			worker.dirty = false
			m.mu.Unlock()
			continue
		}
		m.mu.Unlock()
		return
	}
}

func (m *Manager) processOnePeerQuery(ctx context.Context, recipient string) error {
	select {
	case m.mailboxSlots <- struct{}{}:
		defer func() { <-m.mailboxSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	loaded, err := m.runtime(ctx, recipient)
	if err != nil {
		request, claimErr := m.peerQueries.ClaimNextPeerQuery(ctx, recipient, time.Now().UTC())
		if claimErr == nil {
			m.failPeerRequest(request, peer.StateRecipientUnavailable, err)
		}
		return err
	}
	if !loaded.admissionMu.TryLock() {
		return ErrBusy
	}
	loaded.mu.Lock()
	release := func() { loaded.mu.Unlock(); loaded.admissionMu.Unlock() }
	if m.sessionDeleting(recipient) {
		release()
		return ErrDeleteBusy
	}
	snapshot, err := loaded.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		release()
		return err
	}
	if snapshot.Active != nil {
		release()
		return ErrBusy
	}
	request, err := m.peerQueries.ClaimNextPeerQuery(ctx, recipient, time.Now().UTC())
	if err != nil {
		release()
		return err
	}
	if !m.trackPeerQuery(request) {
		release()
		m.failPeerRequest(request, peer.StateInterrupted, ErrDeleteBusy)
		return ErrDeleteBusy
	}
	defer m.untrackPeerQuery(request.ID)
	subscription, err := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{After: loaded.eventCursor, IncludeTransient: true, Buffer: 256})
	if err != nil {
		release()
		m.failPeerRequest(request, peer.StateRecipientUnavailable, err)
		return err
	}
	boundary, err := m.peerQueryBoundary(ctx, request)
	if err != nil {
		subscription.Close()
		release()
		m.failPeerRequest(request, peer.StateFailed, err)
		return err
	}
	if err = loaded.droid.Inform(ctx, boundary); err != nil {
		received, reconcileErr := loaded.droid.BoundaryReceived(context.Background(), request.ID)
		if reconcileErr != nil || !received {
			subscription.Close()
			release()
			m.failPeerRequest(request, peer.StateInterrupted, errors.Join(err, reconcileErr))
			return err
		}
	}
	handle, _, err := loaded.droid.React(ctx, "peer:"+request.ID)
	if err != nil {
		subscription.Close()
		release()
		if errors.Is(err, droids.ErrReactionLimit) {
			m.failPeerRequest(request, peer.StateInterrupted, err)
		}
		return err
	}
	bound, err := m.peerQueries.BindPeerRecipientTurn(ctx, request.ID, request.Generation, string(handle.TurnID()))
	if err != nil {
		subscription.Close()
		release()
		return err
	}
	turnID := string(handle.TurnID())
	_, err = m.launchAdmittedRunLocked(loaded, recipient, handle, subscription, true, &bound, []NewEvent{{SessionID: recipient, TurnID: turnID, RunID: turnID, Kind: EventRunStarted, Status: RunStatusRunning}})
	if err != nil {
		m.failPeerRequest(bound, peer.StateInterrupted, err)
		return err
	}
	loaded.mu.Lock()
	done := loaded.runs[turnID].done
	loaded.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (m *Manager) beginPeerAdmission(sender, recipient string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.deleting[sender] || m.deleting[recipient] {
		return false
	}
	m.peerAdmissionCounts[sender]++
	m.peerAdmissionCounts[recipient]++
	return true
}

func (m *Manager) endPeerAdmission(sender, recipient string) {
	m.mu.Lock()
	for _, sessionID := range []string{sender, recipient} {
		m.peerAdmissionCounts[sessionID]--
		if m.peerAdmissionCounts[sessionID] == 0 {
			delete(m.peerAdmissionCounts, sessionID)
		}
	}
	m.mu.Unlock()
}

func (m *Manager) trackPeerQuery(request peer.Request) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.deleting[request.SenderSessionID] || m.deleting[request.RecipientSessionID] {
		return false
	}
	m.peerActive[request.ID] = request
	return true
}

func (m *Manager) untrackPeerQuery(requestID string) {
	m.mu.Lock()
	delete(m.peerActive, requestID)
	m.mu.Unlock()
}

func (m *Manager) peerQueryBoundary(ctx context.Context, request peer.Request) (droids.BoundaryMessage, error) {
	sender, err := m.store.GetSession(ctx, request.SenderSessionID)
	if err != nil {
		return droids.BoundaryMessage{}, err
	}
	name := rendererSafePeerName(sender.Name)
	if name == "" {
		name = "unnamed session"
	}
	text := fmt.Sprintf("Peer session %s (%s) asks:\n%s", name, sender.ID, request.Message)
	details, err := droids.EncodeDetails(struct {
		Version            int    `json:"version"`
		RequestID          string `json:"requestId"`
		SenderSessionID    string `json:"senderSessionId"`
		ThreadID           string `json:"threadId,omitempty"`
		PrecedingRequestID string `json:"precedingRequestId,omitempty"`
		HopCount           int    `json:"hopCount"`
	}{1, request.ID, request.SenderSessionID, request.ThreadID, request.PrecedingRequestID, request.HopCount})
	if err != nil {
		return droids.BoundaryMessage{}, err
	}
	return droids.BoundaryMessage{ID: request.ID, Kind: "peer_query", Source: request.SenderSessionID, Content: []droids.InputContent{droids.TextInput{Text: text}}, Details: details}, nil
}

func rendererSafePeerName(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(value, "�"))
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 256 {
		return value
	}
	end := 256
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

func (m *Manager) completePeerRun(request peer.Request, result PromptResult) {
	state := peer.StateCompleted
	terminalError := result.ErrorMessage
	switch result.Status {
	case RunStatusCompleted:
	case RunStatusAborted:
		state = peer.StateAborted
	case RunStatusInterrupted:
		state = peer.StateInterrupted
	default:
		state = peer.StateFailed
	}
	completed, err := m.peerQueries.CompletePeerQuery(context.Background(), peer.Completion{ID: request.ID, RecipientTurnID: result.TurnID, Generation: request.Generation, State: state, Result: result.Text, Error: terminalError, FinishedAt: time.Now().UTC()}, m.peerLimits)
	if err == nil {
		m.deliverPeerResult(completed)
	}
	m.wakePeerQuery(request.RecipientSessionID)
}
func (m *Manager) failPeerRequest(request peer.Request, state peer.State, err error) {
	completed, e := m.peerQueries.CompletePeerQuery(context.Background(), peer.Completion{ID: request.ID, Generation: request.Generation, State: state, Error: err.Error(), FinishedAt: time.Now().UTC()}, m.peerLimits)
	if e == nil {
		m.deliverPeerResult(completed)
	}
}

func (m *Manager) deliverPeerResult(request peer.Request) {
	loaded, err := m.runtime(context.Background(), request.SenderSessionID)
	if err != nil {
		return
	}
	loaded.mu.Lock()
	if err := loaded.events.append([]NewEvent{{SessionID: request.SenderSessionID, Kind: EventPeerQueryChanged, PeerRequestID: request.ID}}); err == nil {
		loaded.signalEventChangedLocked()
	}
	text := fmt.Sprintf("Peer query %s to session %s finished with state %s.", request.ID, request.RecipientSessionID, request.State)
	if request.Result != "" {
		text += "\nResult: " + request.Result
	}
	if request.Error != "" {
		text += "\nError: " + request.Error
	}
	details, _ := json.Marshal(struct {
		Version   int        `json:"version"`
		RequestID string     `json:"requestId"`
		State     peer.State `json:"state"`
	}{1, request.ID, request.State})
	if err := loaded.droid.Inform(context.Background(), droids.BoundaryMessage{ID: request.ID, Kind: "peer_result", Source: request.RecipientSessionID, Content: []droids.InputContent{droids.TextInput{Text: text}}, Details: details}); err != nil {
		_, _ = loaded.droid.BoundaryReceived(context.Background(), request.ID)
	}
	loaded.mu.Unlock()
}

func (m *Manager) DiscoverPeers(ctx context.Context, sender string, limit int) ([]peer.Session, error) {
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	records, err := m.store.ListSessions(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]peer.Session, 0, min(limit, len(records)))
	for _, r := range records {
		if r.ID == sender || !r.Persistent || r.ArchivedAt != nil {
			continue
		}
		availability := "idle"
		m.mu.Lock()
		loaded := m.runtimes[r.ID]
		deleting := m.deleting[r.ID]
		m.mu.Unlock()
		if deleting {
			continue
		}
		if loaded != nil {
			loaded.mu.Lock()
			if loaded.activeRun != "" {
				availability = "busy"
			}
			loaded.mu.Unlock()
		}
		out = append(out, peer.Session{ID: r.ID, Name: rendererSafePeerName(r.Name), CWD: r.CWD, ParentSessionID: r.ParentSessionID, Availability: availability})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (m *Manager) SendPeerQuery(ctx context.Context, sender string, call droids.ToolContext, recipient, message, thread, preceding string) (peer.Request, error) {
	if m.peerQueries == nil {
		return peer.Request{}, peer.ErrUnavailable
	}
	if !m.beginPeerAdmission(sender, recipient) {
		return peer.Request{}, ErrDeleteBusy
	}
	defer m.endPeerAdmission(sender, recipient)
	if strings.TrimSpace(message) == "" {
		return peer.Request{}, peer.ErrInvalid
	}
	route := []string{sender, recipient}
	hop := 1
	if inbound, err := m.peerQueries.PeerQueryByRecipientTurn(ctx, sender, string(call.TurnID)); err == nil {
		route = append(append([]string(nil), inbound.Route...), recipient)
		hop = inbound.HopCount + 1
	} else if !errors.Is(err, peer.ErrNotFound) {
		return peer.Request{}, err
	}
	for _, id := range route[:len(route)-1] {
		if id == recipient {
			return peer.Request{}, fmt.Errorf("%w: peer query cycle", peer.ErrInvalid)
		}
	}
	if preceding != "" {
		prior, err := m.peerQueries.PeerQuery(ctx, preceding)
		if err != nil || prior.SenderSessionID != sender {
			return peer.Request{}, peer.ErrNotFound
		}
	}
	id, err := identifier.New("peer_")
	if err != nil {
		return peer.Request{}, err
	}
	key := string(call.TurnID) + ":" + string(call.ToolCallID)
	if call.ToolCallID == "" {
		return peer.Request{}, peer.ErrInvalid
	}
	value, created, err := m.peerQueries.AdmitPeerQuery(ctx, peer.Admission{ID: id, IdempotencyKey: key, SenderSessionID: sender, RecipientSessionID: recipient, Message: message, ThreadID: thread, PrecedingRequestID: preceding, Route: route, HopCount: hop, Now: time.Now().UTC()}, m.peerLimits)
	if err == nil && created {
		m.wakePeerQuery(recipient)
	}
	return value, err
}

func (m *Manager) InspectPeerQuery(ctx context.Context, sender, id string) (peer.Request, error) {
	value, err := m.peerQueries.PeerQuery(ctx, id)
	if err == nil && value.SenderSessionID != sender {
		err = peer.ErrNotFound
	}
	return value, err
}
func (m *Manager) WaitPeerQuery(ctx context.Context, sender, id string, timeout time.Duration) (peer.Request, bool, error) {
	if timeout <= 0 || timeout > 30*time.Second {
		return peer.Request{}, false, peer.ErrInvalid
	}
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		value, err := m.InspectPeerQuery(ctx, sender, id)
		if err != nil {
			return peer.Request{}, false, err
		}
		if value.State.Terminal() {
			return value, false, nil
		}
		select {
		case <-deadline.Done():
			value, err = m.InspectPeerQuery(context.Background(), sender, id)
			if err == nil && value.State.Terminal() {
				return value, false, nil
			}
			return value, true, err
		case <-ticker.C:
		}
	}
}

var _ peer.Service = (*Manager)(nil)
