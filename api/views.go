package kit

import (
	"context"
	"errors"
	"fmt"
	"io"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
)

// RevealView asks the view clients of this session that watch viewID to show
// a line or an annotation, and reports whether one was watching (ADR 0068).
// The reveal is live-only: a view that starts watching later never receives
// it.
//
// A server that predates the operation fails with an UnsupportedError.
func (c *Session) RevealView(ctx context.Context, viewID string, input protocol.ViewRevealInput) (protocol.ViewRevealResult, error) {
	if err := validateViewRequest(viewID, input); err != nil {
		return protocol.ViewRevealResult{}, err
	}
	return sessionProjectedResult(c, ctx, projectViewError("revealView"), func(operation context.Context) (protocol.ViewRevealResult, error) {
		return c.transport.RevealView(operation, c.id, viewID, input)
	})
}

// WatchViewReveals delivers the reveals sent to viewID in this session, in
// order, until cancellation or a terminal server failure. It delivers nothing
// on connect, and reveals sent while it reconnects are lost. Transient opens,
// idle timeouts, and disconnects reconnect internally with bounded backoff.
//
// A server that predates the operation fails with an UnsupportedError.
func (c *Session) WatchViewReveals(ctx context.Context, viewID string, receive func(protocol.ViewReveal)) error {
	if !protocol.ValidViewID(viewID) {
		return fmt.Errorf("invalid view id %q", viewID)
	}
	open := func(operation context.Context, sessionID string) (io.ReadCloser, error) {
		return c.transport.StreamViewReveals(operation, sessionID, viewID)
	}
	return c.watchLatest(ctx, "watchViewReveals", open, func(operation context.Context, body io.Reader) error {
		return clienttransport.ReadViewReveals(body, func(reveal protocol.ViewReveal) error {
			if reveal.SessionID != c.id || reveal.ViewID != viewID {
				return &StreamWatchTerminalError{Err: fmt.Errorf("daemon view reveal stream identity mismatch")}
			}
			if operation.Err() != nil {
				return operation.Err()
			}
			receive(reveal)
			return nil
		})
	})
}

func validateViewRequest(viewID string, input protocol.ViewRevealInput) error {
	if !protocol.ValidViewID(viewID) {
		return fmt.Errorf("invalid view id %q", viewID)
	}
	if err := input.Validate(); err != nil {
		return err
	}
	return nil
}

// projectViewError reports a server without the view routes as unsupported.
func projectViewError(capability string) func(error) error {
	return func(err error) error {
		if errors.Is(err, httpapi.ErrOperationNotServed) {
			return &UnsupportedError{Capability: capability}
		}
		return projectError(err)
	}
}
