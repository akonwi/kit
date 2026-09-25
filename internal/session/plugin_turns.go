package session

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

const (
	pluginTurnHistoryPage = 256
	pluginTurnFrameLimit  = 256 << 10
)

type pluginTurnEventBinding struct{ host PluginHost }
type pluginTurnSettlement struct {
	done      chan struct{}
	eligible  bool
	finalized bool
}
type pluginTurnEventBridge struct {
	binding    atomic.Pointer[pluginTurnEventBinding]
	droid      atomic.Pointer[droids.Droid]
	ctx        context.Context
	cancel     context.CancelFunc
	wake       chan struct{}
	done       chan struct{}
	deliveryMu sync.Mutex
	mu         sync.Mutex
	active     string
	pending    []string
	settled    map[string]*pluginTurnSettlement
	order      []string
}

func newPluginTurnEventBridge(parent context.Context) *pluginTurnEventBridge {
	ctx, cancel := context.WithCancel(parent)
	bridge := &pluginTurnEventBridge{ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), settled: make(map[string]*pluginTurnSettlement)}
	go bridge.run()
	return bridge
}

func closePluginTurnEventBridge(bridge *pluginTurnEventBridge) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = bridge.close(ctx)
}

func (b *pluginTurnEventBridge) close(ctx context.Context) error {
	b.cancel()
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *pluginTurnEventBridge) started(turnID droids.TurnID) {
	id := string(turnID)
	b.deliveryMu.Lock()
	defer b.deliveryMu.Unlock()
	b.mu.Lock()
	previous := b.active
	previousSettlement := b.settled[previous]
	previousFinalized := b.finalizeLocked(previous)
	settlement := b.settled[id]
	if settlement == nil {
		settlement = &pluginTurnSettlement{done: make(chan struct{})}
		b.settled[id] = settlement
	}
	b.active = id
	binding := b.binding.Load()
	b.mu.Unlock()
	if previousFinalized {
		if binding != nil && binding.host != nil {
			binding.host.TurnCompleted(PluginTurn{ID: previous, OmitReason: "completion projection did not settle before the next turn"})
		}
		close(previousSettlement.done)
	}
	if binding == nil || binding.host == nil {
		return
	}
	eligible := binding.host.TurnStarted(id)
	b.mu.Lock()
	settlement.eligible = eligible
	b.mu.Unlock()
}

// turnSettled only publishes bounded worker-owned state while droid authority is held.
func (b *pluginTurnEventBridge) turnSettled(turnID droids.TurnID) {
	id := string(turnID)
	b.mu.Lock()
	settlement := b.settled[id]
	if settlement == nil {
		settlement = &pluginTurnSettlement{done: make(chan struct{})}
		b.settled[id] = settlement
	}
	if settlement.finalized {
		b.mu.Unlock()
		return
	}
	if !settlement.eligible {
		b.finalizeLocked(id)
		close(settlement.done)
		b.mu.Unlock()
		return
	}
	compacted := b.pending[:0]
	for _, pendingID := range b.pending {
		if pending := b.settled[pendingID]; pending != nil && !pending.finalized {
			compacted = append(compacted, pendingID)
		}
	}
	b.pending = append(compacted, id)
	b.mu.Unlock()
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *pluginTurnEventBridge) run() {
	defer close(b.done)
	for {
		select {
		case <-b.wake:
			for b.completeNext() {
			}
		case <-b.ctx.Done():
			return
		}
	}
}

func (b *pluginTurnEventBridge) completeNext() bool {
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return false
	}
	turnID := b.pending[0]
	b.pending[0] = ""
	b.pending = b.pending[1:]
	settlement := b.settled[turnID]
	if settlement == nil || settlement.finalized {
		b.mu.Unlock()
		return true
	}
	b.mu.Unlock()

	turn := PluginTurn{ID: turnID, OmitReason: "could not read canonical messages"}
	if droid := b.droid.Load(); droid != nil {
		ctx, cancel := context.WithTimeout(b.ctx, 2*time.Second)
		turn = projectPluginTurn(ctx, droid, turnID)
		cancel()
	}
	b.deliverCompletion(turn)
	return true
}

func (b *pluginTurnEventBridge) deliverCompletion(turn PluginTurn) {
	b.deliveryMu.Lock()
	defer b.deliveryMu.Unlock()
	b.mu.Lock()
	if !b.finalizeLocked(turn.ID) {
		b.mu.Unlock()
		return
	}
	settlement := b.settled[turn.ID]
	binding := b.binding.Load()
	b.mu.Unlock()
	if binding != nil && binding.host != nil {
		binding.host.TurnCompleted(turn)
	}
	// Settlement includes delivery to the host, not just claiming the turn.
	close(settlement.done)
}

func (b *pluginTurnEventBridge) finalizeLocked(turnID string) bool {
	if turnID == "" {
		return false
	}
	settlement := b.settled[turnID]
	if settlement == nil || settlement.finalized {
		return false
	}
	settlement.finalized = true
	if b.active == turnID {
		b.active = ""
	}
	b.order = append(b.order, turnID)
	if len(b.order) > 256 {
		oldest := b.order[0]
		if oldest != b.active {
			delete(b.settled, oldest)
		}
		b.order = b.order[1:]
	}
	return true
}

func (b *pluginTurnEventBridge) waitCompleted(turnID string) {
	b.mu.Lock()
	settlement := b.settled[turnID]
	b.mu.Unlock()
	if settlement == nil {
		return
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-settlement.done:
		return
	case <-b.ctx.Done():
		return
	case <-timer.C:
	}
	b.deliverCompletion(PluginTurn{ID: turnID, OmitReason: "completion projection exceeded the native settlement deadline"})
}

func projectPluginTurn(ctx context.Context, droid *droids.Droid, turnID string) PluginTurn {
	result := PluginTurn{ID: turnID}
	var descending []PluginTurnMessage
	var cursor uint64
	found := false
	projectedBytes := 0
	for {
		page, err := droid.History(ctx, droids.HistoryQuery{Before: cursor, Limit: pluginTurnHistoryPage, Descending: true})
		if err != nil {
			result.OmitReason = "could not read canonical messages"
			return result
		}
		done := false
		for _, envelope := range page.Messages {
			if string(envelope.TurnID) != turnID {
				if found {
					done = true
					break
				}
				continue
			}
			found = true
			message, public := projectPluginTurnMessage(envelope.Message)
			if !public {
				continue
			}
			encoded, encodeErr := json.Marshal(message)
			if encodeErr != nil {
				result.OmitReason = "could not encode canonical messages"
				return result
			}
			projectedBytes += len(encoded) + 1
			if projectedBytes > pluginTurnFrameLimit {
				result.OmitReason = "exceeds the native 256 KiB turn-event limit"
				return result
			}
			descending = append(descending, message)
		}
		if done || !page.HasMore {
			break
		}
		cursor = page.Next
	}
	for index := len(descending) - 1; index >= 0; index-- {
		result.Messages = append(result.Messages, descending[index])
	}
	return result
}

func projectPluginTurnMessage(message droids.Message) (PluginTurnMessage, bool) {
	result := PluginTurnMessage{}
	switch value := message.(type) {
	case droids.UserMessage:
		result.Role = string(droids.RoleUser)
		for _, content := range value.Content {
			if text, ok := content.(droids.TextInput); ok {
				result.Content = append(result.Content, text.Text)
			}
		}
	case droids.AssistantMessage:
		result.Role = string(droids.RoleAssistant)
		for _, content := range value.Content {
			if text, ok := content.(droids.TextContent); ok {
				result.Content = append(result.Content, text.Text)
			}
		}
	default:
		return PluginTurnMessage{}, false
	}
	return result, true
}
