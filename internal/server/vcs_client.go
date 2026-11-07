package server

import (
	"io"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
)

// StreamError marks a terminal session-stream protocol violation.
type StreamError = httpapi.StreamError

// ReadSessionVCS validates bounded live records and invokes receive in wire order.
func ReadSessionVCS(body io.Reader, receive func(protocol.SessionVCSStatus) error) error {
	return clienttransport.ReadSessionVCS(body, receive)
}

// ReadSessionPullRequest validates bounded live pull request records and
// invokes receive in wire order.
func ReadSessionPullRequest(body io.Reader, receive func(protocol.SessionPullRequestStatus) error) error {
	return clienttransport.ReadSessionPullRequest(body, receive)
}
