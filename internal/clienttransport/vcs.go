package clienttransport

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

// StreamSessionPullRequest opens the authenticated live pull request status
// stream. The server sends the current status first, then deduplicated
// latest-only `pullRequest.status` records, with comment heartbeats while
// idle. A server that predates the operation fails with
// httpapi.ErrOperationNotServed; other pre-stream failures are typed *APIError
// values.
func (c *Client) StreamSessionPullRequest(ctx context.Context, sessionID string) (io.ReadCloser, error) {
	return httpapi.OpenStream(ctx, c, httpapi.StreamSessionPullRequest, httpapi.SessionPath{SessionID: sessionID})
}

// ReadSessionPullRequest validates bounded live records and invokes receive in
// wire order, like ReadSessionVCS.
func ReadSessionPullRequest(body io.Reader, receive func(protocol.SessionPullRequestStatus) error) error {
	return httpapi.ReadStream(body, httpapi.StreamSessionPullRequest, func(record httpapi.StreamRecord[protocol.SessionPullRequestStatus]) error {
		return receive(record.Payload)
	})
}
