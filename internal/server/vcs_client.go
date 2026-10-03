package server

import (
	"context"
	"io"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
)

// StreamError marks a terminal session-stream protocol violation: malformed
// framing, an oversized record, an undeclared record, or a payload that fails
// strict decoding or validation.
type StreamError = httpapi.StreamError

// StreamSessionVCS opens the authenticated live repository-status stream. The
// server sends the latest snapshot first, then deduplicated latest-only
// `vcs.status` records, with comment heartbeats while idle. Pre-stream
// failures are typed *APIError values.
func (c *Client) StreamSessionVCS(ctx context.Context, sessionID string) (io.ReadCloser, error) {
	return httpapi.OpenStream(ctx, c, httpapi.StreamSessionVCS, httpapi.SessionPath{SessionID: sessionID})
}

// ReadSessionVCS validates bounded live records and invokes receive in wire
// order. Comments are heartbeats. Violations are *StreamError; a truncated
// record is a violation rather than an update. A clean end returns nil.
func ReadSessionVCS(body io.Reader, receive func(protocol.SessionVCSStatus) error) error {
	return httpapi.ReadStream(body, httpapi.StreamSessionVCS, func(record httpapi.StreamRecord[protocol.SessionVCSStatus]) error {
		return receive(record.Payload)
	})
}
