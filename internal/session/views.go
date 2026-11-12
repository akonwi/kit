package session

import (
	"context"
	"errors"
	"sync"
)

// ViewReveal asks the view clients watching one view identifier to show a
// line or an annotation (ADR 0068). Exactly one of Line and Annotation is set.
type ViewReveal struct {
	Line       int
	Annotation uint64
}

// ErrViewRevealCapacity indicates too many live view watchers in one session.
var ErrViewRevealCapacity = errors.New("view reveal watcher limit exceeded")

// maxViewWatchers bounds the open view reveal streams of one session, across
// every view identifier.
const maxViewWatchers = 64

// viewRevealHub relays live-only reveals to the watchers of each view
// identifier. Nothing is stored: a reveal reaches only watchers subscribed
// when it is sent. Each watcher keeps only its latest unread reveal, since a
// view shows one target at a time.
type viewRevealHub struct {
	mu       sync.Mutex
	closed   bool
	count    int
	watchers map[string]map[chan ViewReveal]struct{}
}

// reveal sends reveal to the watchers of viewID and reports whether any
// watched it.
func (h *viewRevealHub) reveal(viewID string, reveal ViewReveal) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	watchers := h.watchers[viewID]
	for watcher := range watchers {
		// Replace an unread reveal; a send never blocks the caller.
		select {
		case <-watcher:
		default:
		}
		watcher <- reveal
	}
	return len(watchers) > 0
}

// watch subscribes to the reveals of viewID. The returned function ends the
// subscription and closes its channel; closing the hub also closes it.
func (h *viewRevealHub) watch(viewID string) (<-chan ViewReveal, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nil, ErrClosed
	}
	if h.count >= maxViewWatchers {
		return nil, nil, ErrViewRevealCapacity
	}
	if h.watchers == nil {
		h.watchers = make(map[string]map[chan ViewReveal]struct{})
	}
	if h.watchers[viewID] == nil {
		h.watchers[viewID] = make(map[chan ViewReveal]struct{})
	}
	reveals := make(chan ViewReveal, 1)
	h.watchers[viewID][reveals] = struct{}{}
	h.count++
	stop := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		watchers := h.watchers[viewID]
		if _, ok := watchers[reveals]; ok {
			delete(watchers, reveals)
			if len(watchers) == 0 {
				delete(h.watchers, viewID)
			}
			h.count--
			close(reveals)
		}
	}
	return reveals, stop, nil
}

// close ends every subscription; later watches fail and reveals reach no one.
func (h *viewRevealHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, watchers := range h.watchers {
		for watcher := range watchers {
			close(watcher)
		}
	}
	h.watchers = nil
	h.count = 0
}

// WatchViewReveals observes only the reveals sent to viewID in sessionID
// after it subscribes. The caller must call the returned function; runtime
// shutdown also closes the subscription.
func (m *Manager) WatchViewReveals(ctx context.Context, sessionID, viewID string) (<-chan ViewReveal, func(), error) {
	if err := m.beginOperation(); err != nil {
		return nil, nil, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	return loaded.viewReveals.watch(viewID)
}

// RevealView sends reveal to the watchers of viewID in sessionID and reports
// whether any watched it. Nothing is kept for a later watcher.
func (m *Manager) RevealView(ctx context.Context, sessionID, viewID string, reveal ViewReveal) (bool, error) {
	if err := m.beginOperation(); err != nil {
		return false, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return false, err
	}
	return loaded.viewReveals.reveal(viewID, reveal), nil
}
