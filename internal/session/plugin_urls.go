package session

import (
	"context"
	"fmt"

	"github.com/akonwi/kit/internal/weburl"
)

// PluginURLRequest is a web address a plugin asked its owning session's
// attached clients to open.
type PluginURLRequest struct {
	URL string
}

// PluginURLHost installs the session-owned URL request port before Start.
// current reports whether the requesting plugin generation is still current;
// a revoked generation cannot publish a request.
type PluginURLHost interface {
	SetURLObserver(func(context.Context, PluginURLRequest, func() bool) error)
}

// requestPluginURL publishes a url.open.requested event on loaded's live
// stream. The request is fire-and-forget: it waits for no client, is not
// recorded in the transcript or snapshots, and is lost with the runtime.
// Clients act on it only when they receive it live (see
// protocol.URLOpenRequestedEvent).
func (m *Manager) requestPluginURL(ctx context.Context, loaded *runtime, sessionID string, request PluginURLRequest, current func() bool) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !weburl.IsCanonical(request.URL) {
		return fmt.Errorf("%w: url must be a canonical http or https address", ErrInvalidInput)
	}
	m.mu.Lock()
	owned := m.runtimes[sessionID] == loaded && !m.deleting[sessionID]
	m.mu.Unlock()
	if !owned || (current != nil && !current()) {
		return ErrClosed
	}
	return loaded.events.append([]NewEvent{{SessionID: sessionID, Kind: EventURLOpenRequested, URL: request.URL}})
}
