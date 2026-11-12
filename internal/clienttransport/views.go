package clienttransport

import (
	"context"
	"fmt"
	"io"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
)

// RevealView asks the view clients watching viewID to show a line or an
// annotation and reports whether one was watching (ADR 0068). A server that
// predates the operation fails with httpapi.ErrOperationNotServed.
func (c *Client) RevealView(ctx context.Context, sessionID, viewID string, input protocol.ViewRevealInput) (protocol.ViewRevealResult, error) {
	if !protocol.ValidViewID(viewID) {
		return protocol.ViewRevealResult{}, fmt.Errorf("invalid view id %q", viewID)
	}
	if err := input.Validate(); err != nil {
		return protocol.ViewRevealResult{}, fmt.Errorf("validate view reveal: %w", err)
	}
	return httpapi.Call(ctx, c, httpapi.RevealView, httpapi.ViewPath{SessionID: sessionID, ViewID: viewID}, input)
}

// StreamViewReveals opens the authenticated live-only reveal stream of
// viewID. A server that predates the operation fails with
// httpapi.ErrOperationNotServed; other pre-stream failures are typed
// *APIError values.
func (c *Client) StreamViewReveals(ctx context.Context, sessionID, viewID string) (io.ReadCloser, error) {
	if !protocol.ValidViewID(viewID) {
		return nil, fmt.Errorf("invalid view id %q", viewID)
	}
	return httpapi.OpenStream(ctx, c, httpapi.StreamViewReveals, httpapi.ViewPath{SessionID: sessionID, ViewID: viewID})
}

// ReadViewReveals validates bounded live records and invokes receive in wire
// order.
func ReadViewReveals(body io.Reader, receive func(protocol.ViewReveal) error) error {
	return httpapi.ReadStream(body, httpapi.StreamViewReveals, func(record httpapi.StreamRecord[protocol.ViewReveal]) error {
		return receive(record.Payload)
	})
}
