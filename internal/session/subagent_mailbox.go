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
	maxMailboxDeliveryBatch  = 8
	maxMailboxDeliveryGroups = 32
	maxMailboxContextItem    = 4 << 10
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

// MailboxAdded injects a newly completed child result only when the parent is
// already active. It never loads an idle parent or starts a model call.
func (m *Manager) MailboxAdded(_ context.Context, item subagent.MailboxItem) {
	if m == nil || m.mailbox == nil {
		return
	}
	if err := m.beginOperation(); err != nil {
		return
	}
	defer m.ops.Done()
	m.mu.Lock()
	loaded := m.runtimes[item.OwnerSessionID]
	deleting := m.deleting[item.OwnerSessionID]
	m.mu.Unlock()
	if loaded == nil || deleting {
		return
	}
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	if loaded.activeRun == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.deliverSubagentMailboxItems(ctx, loaded.droid, []subagent.MailboxItem{item})
}

func (m *Manager) deliverPendingSubagentMailbox(ctx context.Context, sessionID string, droid *droids.Droid) error {
	if m.mailbox == nil {
		return nil
	}
	for range maxMailboxDeliveryGroups {
		items, err := m.mailbox.PendingMailbox(ctx, sessionID, maxMailboxDeliveryBatch)
		if err != nil {
			return fmt.Errorf("load parent subagent mailbox: %w", err)
		}
		if len(items) == 0 {
			return nil
		}
		if err := m.deliverSubagentMailboxItems(ctx, droid, items); err != nil {
			return err
		}
	}
	return fmt.Errorf("parent subagent mailbox exceeds one-turn delivery capacity")
}

func (m *Manager) deliverSubagentMailboxItems(ctx context.Context, droid *droids.Droid, items []subagent.MailboxItem) error {
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
		ConversationID string `json:"conversationId"`
		TaskID         string `json:"taskId"`
		Agent          string `json:"agent"`
		State          string `json:"state"`
	}
	var text strings.Builder
	detailsItems := make([]detailItem, 0, len(items))
	receipts := make([]string, 0, len(items))
	for index, item := range items {
		if index > 0 {
			text.WriteString("\n\n")
		}
		fmt.Fprintf(&text, "Subagent %s task %s finished with state %s.", item.AgentName, item.TaskID, item.State)
		if summary := boundedMailboxContext(item.Summary); summary != "" {
			text.WriteString("\nResult summary: ")
			text.WriteString(summary)
		}
		if terminalError := boundedMailboxContext(item.Error); terminalError != "" {
			text.WriteString("\nError: ")
			text.WriteString(terminalError)
		}
		detailsItems = append(detailsItems, detailItem{
			ConversationID: string(item.ConversationID), TaskID: string(item.TaskID),
			Agent: item.AgentName, State: string(item.State),
		})
		receipts = append(receipts, item.ID)
	}
	details, err := droids.EncodeDetails(struct {
		Version int          `json:"version"`
		Items   []detailItem `json:"items"`
	}{Version: 1, Items: detailsItems})
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
