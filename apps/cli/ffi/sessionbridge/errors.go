package sessionbridge

import (
	"errors"

	kit "github.com/akonwi/kit/api"
)

// CursorUnavailable reports whether an earlier-transcript request failed
// because its cursor is no longer valid, as after compaction. The caller
// recovers from a fresh snapshot.
func CursorUnavailable(err error) bool {
	return errors.Is(err, kit.ErrTranscriptCursorUnavailable)
}

// Incompatible reports whether a request or stream failed because the server
// can no longer serve this client, as after the daemon was replaced by another
// release. Retrying cannot recover; the client must be restarted.
func Incompatible(err error) bool {
	return errors.Is(err, kit.ErrIncompatibleServer)
}

// Conflict reports whether the server refused a request because it conflicts
// with the session's current state, as a new prompt does while follow-ups
// are queued on an idle session.
func Conflict(err error) bool {
	var server *kit.ServerError
	return errors.As(err, &server) && server.Code == kit.ErrorConflict
}
