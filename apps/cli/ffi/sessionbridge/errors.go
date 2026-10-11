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
