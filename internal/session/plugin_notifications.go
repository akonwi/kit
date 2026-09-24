package session

import (
	"context"
	"errors"
	"sync"
)

// PluginToast is a live notification owned by one session plugin instance.
// Persistent notifications also remain in the host's diagnostic projection.
type PluginToast struct {
	PluginID, Instance, Title, Subtitle, Variant string
	Persistent                                   bool
}

// PluginToastHost accepts its session notification sink before Start.
type PluginToastHost interface {
	SetToastObserver(func(context.Context, PluginToast) error)
}

// ErrPluginNotificationCapacity indicates too many live subscribers in one session.
var ErrPluginNotificationCapacity = errors.New("plugin notification subscriber limit exceeded")

const pluginToastQueueSize = 16
const maxPluginToastSubscribers = 32

type pluginNotificationHub struct {
	mu          sync.Mutex
	closed      bool
	subscribers map[chan PluginToast]struct{}
}

func (h *pluginNotificationHub) publish(ctx context.Context, toast PluginToast) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if h.closed {
		return nil
	}
	// Never retain an offline backlog or let a slow renderer block the plugin.
	for subscriber := range h.subscribers {
		select {
		case subscriber <- toast:
		default:
		}
	}
	return nil
}

func (h *pluginNotificationHub) subscribe() (chan PluginToast, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nil, ErrClosed
	}
	if len(h.subscribers) >= maxPluginToastSubscribers {
		return nil, nil, ErrPluginNotificationCapacity
	}
	if h.subscribers == nil {
		h.subscribers = make(map[chan PluginToast]struct{})
	}
	messages := make(chan PluginToast, pluginToastQueueSize)
	h.subscribers[messages] = struct{}{}
	closeSubscription := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subscribers[messages]; ok {
			delete(h.subscribers, messages)
			close(messages)
		}
	}
	return messages, closeSubscription, nil
}

func (h *pluginNotificationHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for subscriber := range h.subscribers {
		delete(h.subscribers, subscriber)
		close(subscriber)
	}
}

// SubscribePluginToasts observes only notifications accepted after subscription.
// The caller must unsubscribe; runtime shutdown also closes the subscription.
func (m *Manager) SubscribePluginToasts(ctx context.Context, sessionID string) (<-chan PluginToast, func(), error) {
	if err := m.beginOperation(); err != nil {
		return nil, nil, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	return loaded.pluginNotifications.subscribe()
}
