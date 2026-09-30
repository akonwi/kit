// Package sessionbridge adapts session-client calls whose Go shapes Ard cannot
// import directly.
package sessionbridge

import (
	"context"
	"errors"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

// ErrWatchUnsupported reports a bound session without attachment-scoped
// events.
var ErrWatchUnsupported = errors.New("session does not support event watching")

// Watch packages SessionEventWatcher.Watch's baseline snapshot and stream.
type Watch struct {
	Snapshot protocol.SessionSnapshot
	Stream   sessionclient.EventStream
}

// WatchSession opens the attachment-scoped event stream of session.
func WatchSession(ctx context.Context, session sessionclient.Session) (Watch, error) {
	watcher, ok := session.(sessionclient.SessionEventWatcher)
	if !ok {
		return Watch{}, ErrWatchUnsupported
	}
	snapshot, stream, err := watcher.Watch(ctx)
	if err != nil {
		return Watch{}, err
	}
	return Watch{Snapshot: snapshot, Stream: stream}, nil
}
