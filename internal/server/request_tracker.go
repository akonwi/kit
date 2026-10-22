package server

import (
	"context"
	"net/http"
	"sync"

	"github.com/akonwi/kit/internal/httpapi"
)

// requestTracker counts running HTTP handlers so daemon teardown can wait for
// them. http.Server.Close does not wait for handlers, so without this, a
// handler that outlives Shutdown's deadline could still be using the store
// after it closes or the lifetime lock is released.
type requestTracker struct {
	mu     sync.Mutex
	closed bool
	active int
	idle   chan struct{}
}

func newRequestTracker() *requestTracker {
	return &requestTracker{idle: make(chan struct{})}
}

// Wrap admits each request under the tracker. Requests that arrive after
// Close are refused, so none can start once teardown begins waiting.
func (t *requestTracker) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			httpapi.WriteError(w, httpapi.NewAPIError(http.StatusServiceUnavailable, httpapi.ErrorUnavailable, "server is shutting down", nil))
			return
		}
		t.active++
		t.mu.Unlock()
		defer t.finish()
		next.ServeHTTP(w, r)
	})
}

func (t *requestTracker) finish() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active--
	if t.closed && t.active == 0 {
		close(t.idle)
	}
}

// Close stops admitting requests and waits until every running handler has
// returned or ctx ends. It is safe to call more than once.
func (t *requestTracker) Close(ctx context.Context) error {
	t.mu.Lock()
	if !t.closed {
		t.closed = true
		if t.active == 0 {
			close(t.idle)
		}
	}
	t.mu.Unlock()
	select {
	case <-t.idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
