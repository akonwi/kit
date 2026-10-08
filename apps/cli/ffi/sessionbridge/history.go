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
