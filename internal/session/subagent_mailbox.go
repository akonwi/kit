package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/subagent"
)

const (
	maxMailboxDeliveryBatch   = 8
	maxMailboxDeliveryGroups  = 32
	maxMailboxContextItem     = 4 << 10
	maxMailboxReactionWorkers = 16
	maxConcurrentReactions    = 4
)

// SubagentChanged publishes a lightweight session-scoped invalidation event.
// Clients reload absolute roster state from the authoritative repository.
func (m *Manager) SubagentChanged(_ context.Context, owner string, conversationID subagent.ConversationID, taskID subagent.TaskID) {
	if m == nil {
		return
	}
	if err := m.beginOperation(); err != nil {
		return
	}
	defer m.ops.Done()
	m.mu.Lock()
	loaded := m.runtimes[owner]
	deleting := m.deleting[owner]
	m.mu.Unlock()
	if loaded == nil || deleting {
		return
	}
	m.enqueueSubagentChanged(loaded, NewEvent{
		SessionID: owner, Kind: EventSubagentChanged,
		SubagentConversationID: string(conversationID), SubagentTaskID: string(taskID),
	})
}

func (m *Manager) enqueueSubagentChanged(loaded *runtime, event NewEvent) {
	loaded.subagentEventMu.Lock()
	copy := event
	loaded.subagentEventPending = &copy
	if loaded.subagentEventDraining {
		loaded.subagentEventMu.Unlock()
		return
	}
	loaded.subagentEventDraining = true
	m.ops.Add(1)
	loaded.subagentEventMu.Unlock()
	go func() {
		defer m.ops.Done()
		for {
			loaded.subagentEventMu.Lock()
			pending := loaded.subagentEventPending
			loaded.subagentEventPending = nil
			if pending == nil {
				loaded.subagentEventDraining = false
				loaded.subagentEventMu.Unlock()
				return
			}
			loaded.subagentEventMu.Unlock()
			if err := loaded.events.append([]NewEvent{*pending}); err != nil {
				loaded.events.invalidate()
			}
		}
	}()
}

// MailboxAdded wakes autonomous parent delivery after the child completion is durable.
func (m *Manager) MailboxAdded(_ context.Context, item subagent.MailboxItem) {
	if m == nil || m.mailbox == nil {
		return
	}
	m.wakeSubagentMailbox(item.OwnerSessionID)
}

func (m *Manager) scanPendingSubagentMailbox() {
	defer m.ops.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	afterOwner := ""
	for {
		ctx, cancel := context.WithTimeout(m.mailboxContext, 5*time.Second)
		for ctx.Err() == nil {
			owners, err := m.mailbox.PendingMailboxOwners(ctx, afterOwner, 256)
			if err != nil {
				break
			}
			capacityAvailable := true
			for _, owner := range owners {
				if !m.wakeSubagentMailbox(owner) {
					capacityAvailable = false
					break
				}
				afterOwner = owner
			}
			if !capacityAvailable {
				break
			}
			if len(owners) < 256 {
				afterOwner = ""
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

func (m *Manager) wakeSubagentMailbox(owner string) bool {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return false
	}
	if m.mailboxBlocked[owner] {
		m.mu.Unlock()
		return true
	}
	if worker := m.mailboxWorkers[owner]; worker != nil {
		worker.dirty = true
		m.mu.Unlock()
		return true
	}
	if len(m.mailboxWorkers) >= maxMailboxReactionWorkers {
		m.mu.Unlock()
		return false
	}
	m.mailboxWorkers[owner] = &mailboxReactionWorker{}
	m.ops.Add(1)
	m.mu.Unlock()
	go m.runSubagentMailbox(owner)
	return true
}

func (m *Manager) runSubagentMailbox(owner string) {
	m.mu.Lock()
	owned := m.mailboxWorkers[owner]
	m.mu.Unlock()
	defer m.ops.Done()
	defer func() {
		m.mu.Lock()
		if m.mailboxWorkers[owner] == owned {
			delete(m.mailboxWorkers, owner)
		}
		m.mu.Unlock()
	}()
	delay := 25 * time.Millisecond
	retries := 0
	for {
		err := m.processSubagentMailbox(m.mailboxContext, owner)
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrDeleteBusy) {
			err = nil
		}
		if errors.Is(err, droids.ErrReactionLimit) {
			err = nil
		}
		if err != nil && m.mailboxContext.Err() == nil {
			retries++
			if retries >= 8 {
				err = nil
			} else {
				select {
				case <-m.mailboxContext.Done():
					return
				case <-time.After(delay):
					if delay < 2*time.Second {
						delay *= 2
					}
					continue
				}
			}
		}
		m.mu.Lock()
		worker := m.mailboxWorkers[owner]
		if m.closed {
			delete(m.mailboxWorkers, owner)
			m.mu.Unlock()
			return
		}
		if worker != nil && worker.dirty {
			worker.dirty = false
			m.mu.Unlock()
			delay = 25 * time.Millisecond
			retries = 0
			continue
		}
		delete(m.mailboxWorkers, owner)
		m.mu.Unlock()
		return
	}
}

func (m *Manager) processSubagentMailbox(ctx context.Context, owner string) error {
	loaded, err := m.runtime(ctx, owner)
	if err != nil {
		return err
	}
	var reactionDone <-chan struct{}
	reactionSlot := false
	defer func() {
		if reactionSlot {
			<-m.mailboxSlots
		}
	}()
	for {
		items, err := m.mailbox.PendingMailbox(ctx, owner, 1)
		if err != nil || len(items) == 0 {
			if err == nil && reactionDone != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-reactionDone:
				}
			}
			return err
		}
		if err := m.deliverSubagentMailboxAutonomously(ctx, loaded, items[0], &reactionDone, &reactionSlot); err != nil {
			if errors.Is(err, ErrBusy) || errors.Is(err, droids.ErrBusy) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(10 * time.Millisecond):
					continue
				}
			}
			if reactionDone != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-reactionDone:
				}
			}
			return err
		}
	}
}

func (m *Manager) deliverSubagentMailboxAutonomously(ctx context.Context, loaded *runtime, item subagent.MailboxItem, reactionDone *<-chan struct{}, reactionSlot *bool) error {
	loaded.mu.Lock()
	snapshot, err := loaded.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		loaded.mu.Unlock()
		return err
	}
	status, err := loaded.droid.BoundaryStatus(ctx, item.ID)
	if err != nil {
		loaded.mu.Unlock()
		return err
	}
	if status.TurnID != "" {
		if snapshot.Active != nil && snapshot.Active.TurnID == status.TurnID {
			if run := loaded.runs[string(status.TurnID)]; run != nil && run.autonomous {
				*reactionDone = run.done
			}
		}
		err = m.markSubagentMailboxDelivered(ctx, []subagent.MailboxItem{item})
		loaded.mu.Unlock()
		return err
	}
	if snapshot.Active != nil {
		if err := m.informSubagentMailboxItems(ctx, loaded.droid, []subagent.MailboxItem{item}); err != nil {
			loaded.mu.Unlock()
			return err
		}
		status, err = loaded.droid.BoundaryStatus(ctx, item.ID)
		if err != nil {
			loaded.mu.Unlock()
			return err
		}
		if status.TurnID != "" {
			if run := loaded.runs[string(status.TurnID)]; run != nil && run.autonomous {
				*reactionDone = run.done
			}
			err = m.markSubagentMailboxDelivered(ctx, []subagent.MailboxItem{item})
			loaded.mu.Unlock()
			return err
		}
		if status.Pending {
			loaded.mu.Unlock()
			return ErrBusy
		}
	}
	loaded.mu.Unlock()

	if !*reactionSlot {
		select {
		case m.mailboxSlots <- struct{}{}:
			*reactionSlot = true
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	releaseSlot := func() {
		if *reactionSlot {
			<-m.mailboxSlots
			*reactionSlot = false
		}
	}
	if !loaded.admissionMu.TryLock() {
		releaseSlot()
		return ErrBusy
	}
	loaded.mu.Lock()
	release := func() {
		loaded.mu.Unlock()
		loaded.admissionMu.Unlock()
		releaseSlot()
	}
	if m.sessionDeleting(item.OwnerSessionID) {
		release()
		return ErrDeleteBusy
	}
	snapshot, err = loaded.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		release()
		return err
	}
	if snapshot.Active != nil {
		release()
		return ErrBusy
	}
	subscription, err := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{
		After: loaded.eventCursor, IncludeTransient: true, Buffer: 256,
	})
	if err != nil {
		release()
		return err
	}
	if err := m.touchSessionActivity(ctx, item.OwnerSessionID, time.Now().UTC()); err != nil {
		subscription.Close()
		release()
		return err
	}
	if err := m.informSubagentMailboxItems(ctx, loaded.droid, []subagent.MailboxItem{item}); err != nil {
		subscription.Close()
		release()
		return err
	}
	handle, replayed, err := loaded.droid.React(ctx, "mailbox:"+item.ID)
	if err != nil {
		if errors.Is(err, droids.ErrReactionLimit) {
			m.mu.Lock()
			m.mailboxBlocked[item.OwnerSessionID] = true
			m.mu.Unlock()
		}
		subscription.Close()
		release()
		if errors.Is(err, droids.ErrBusy) {
			return ErrBusy
		}
		return err
	}
	if replayed {
		subscription.Close()
		release()
		return m.markSubagentMailboxDelivered(ctx, []subagent.MailboxItem{item})
	}
	turnID := string(handle.TurnID())
	if _, err := m.launchAdmittedRunLocked(loaded, item.OwnerSessionID, handle, subscription, true, []NewEvent{{
		SessionID: item.OwnerSessionID, TurnID: turnID, RunID: turnID, Kind: EventRunStarted, Status: RunStatusRunning,
	}}); err != nil {
		releaseSlot()
		return err
	}
	loaded.mu.Lock()
	if run := loaded.runs[turnID]; run != nil {
		*reactionDone = run.done
	}
	loaded.mu.Unlock()
	return m.markSubagentMailboxDelivered(ctx, []subagent.MailboxItem{item})
}

func (m *Manager) informPendingSubagentMailbox(ctx context.Context, sessionID string, droid *droids.Droid) ([]subagent.MailboxItem, error) {
	if m.mailbox == nil {
		return nil, nil
	}
	items, err := m.mailbox.PendingMailbox(ctx, sessionID, maxMailboxDeliveryBatch*maxMailboxDeliveryGroups)
	if err != nil {
		return nil, fmt.Errorf("load parent subagent mailbox: %w", err)
	}
	for start := 0; start < len(items); start += maxMailboxDeliveryBatch {
		end := min(start+maxMailboxDeliveryBatch, len(items))
		if err := m.informSubagentMailboxItems(ctx, droid, items[start:end]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (m *Manager) acknowledgeConsumedSubagentMailbox(ctx context.Context, droid *droids.Droid, turnID string, items []subagent.MailboxItem) error {
	if len(items) == 0 {
		return nil
	}
	for _, item := range items {
		status, err := droid.BoundaryStatus(ctx, item.ID)
		if err != nil {
			return err
		}
		if string(status.TurnID) != turnID {
			return fmt.Errorf("subagent mailbox item %q is not associated with parent turn %q", item.ID, turnID)
		}
	}
	return m.markSubagentMailboxDelivered(ctx, items)
}

func (m *Manager) informSubagentMailboxItems(ctx context.Context, droid *droids.Droid, items []subagent.MailboxItem) error {
	message, err := mailboxBoundary(items)
	if err != nil {
		return err
	}
	if err := droid.Inform(ctx, message); err != nil {
		received, reconcileErr := droid.BoundaryReceived(context.Background(), items[0].ID)
		if reconcileErr != nil || !received {
			return fmt.Errorf("deliver subagent mailbox item %q: %w", items[0].ID, errors.Join(err, reconcileErr))
		}
	}
	return nil
}

func (m *Manager) markSubagentMailboxDelivered(ctx context.Context, items []subagent.MailboxItem) error {
	if len(items) == 0 {
		return errors.New("subagent mailbox batch is empty")
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.Generation != items[0].Generation {
			return errors.New("subagent mailbox batch has mixed generations")
		}
		ids = append(ids, item.ID)
	}
	if err := m.mailbox.MarkMailboxDelivered(ctx, ids, items[0].Generation, time.Now().UTC()); err != nil {
		return fmt.Errorf("mark subagent mailbox item %q delivered: %w", items[0].ID, err)
	}
	return nil
}

func mailboxBoundary(items []subagent.MailboxItem) (droids.BoundaryMessage, error) {
	if len(items) == 0 {
		return droids.BoundaryMessage{}, errors.New("subagent mailbox batch is empty")
	}
	type detailItem struct {
		Agent string `json:"agent"`
		State string `json:"state"`
	}
	var text strings.Builder
	detailsItems := make([]detailItem, 0, len(items))
	receipts := make([]string, 0, len(items))
	for index, item := range items {
		if index > 0 {
			text.WriteString("\n\n")
		}
		fmt.Fprintf(&text, "Subagent %s finished with state %s.", item.AgentName, item.State)
		if summary := boundedMailboxContext(item.Summary); summary != "" {
			text.WriteString("\nResult summary: ")
			text.WriteString(summary)
		}
		if terminalError := boundedMailboxContext(subagent.RedactInternalIdentities(item.Error)); terminalError != "" {
			text.WriteString("\nError: ")
			text.WriteString(terminalError)
		}
		detailsItems = append(detailsItems, detailItem{Agent: item.AgentName, State: string(item.State)})
		receipts = append(receipts, item.ID)
	}
	details, err := droids.EncodeDetails(struct {
		Version int          `json:"version"`
		Items   []detailItem `json:"items"`
	}{Version: 2, Items: detailsItems})
	if err != nil {
		return droids.BoundaryMessage{}, err
	}
	if !json.Valid(details) {
		return droids.BoundaryMessage{}, errors.New("encode subagent mailbox details")
	}
	return droids.BoundaryMessage{
		ID: items[0].ID, ReceiptIDs: receipts, Kind: "subagent_result", Source: "subagent",
		Content: []droids.InputContent{droids.TextInput{Text: text.String()}}, Details: details,
	}, nil
}

func boundedMailboxContext(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxMailboxContextItem {
		return value
	}
	value = value[:maxMailboxContextItem]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "…"
}

var _ subagent.EventSink = (*Manager)(nil)
