package kit

import (
	"context"
	"errors"
	"io"
	"sync"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
)

// PluginToastStreamIdleLimit ends a stream with no record or heartbeat for
// three server heartbeat intervals (ADR 0035).
const PluginToastStreamIdleLimit = vcsStreamIdleLimit

// PluginToastStream delivers plugin notifications across transient reconnects.
type PluginToastStream struct {
	updates chan protocol.PluginToast
	done    chan struct{}
	cancel  context.CancelFunc
	once    sync.Once
	err     error
}

// Updates returns the ordered notification channel.
func (s *PluginToastStream) Updates() <-chan protocol.PluginToast { return s.updates }

// Err waits for the stream to finish and returns its terminal error.
func (s *PluginToastStream) Err() error {
	<-s.done
	return s.err
}

// Close detaches the stream without affecting any server-side operation.
func (s *PluginToastStream) Close() error {
	if s != nil {
		s.once.Do(s.cancel)
	}
	return nil
}

// WatchPluginToasts starts a bounded notification stream that reconnects after
// transient opens, idle timeouts, and disconnects.
func (c *Session) WatchPluginToasts(ctx context.Context) (*PluginToastStream, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	body, openErr := c.transport.StreamPluginToasts(operation, c.id)
	if openErr != nil {
		classified := classifyStreamWatchError(openErr)
		var terminal *StreamWatchTerminalError
		if errors.As(classified, &terminal) {
			cancel()
			return nil, classified
		}
	}
	stream := &PluginToastStream{
		updates: make(chan protocol.PluginToast, 16), done: make(chan struct{}), cancel: cancel,
	}
	go stream.run(operation, c, body, openErr)
	return stream, nil
}

func (s *PluginToastStream) run(ctx context.Context, session *Session, body io.ReadCloser, streamErr error) {
	defer close(s.done)
	defer close(s.updates)
	defer s.once.Do(s.cancel)
	failure := 0
	for {
		if body != nil {
			streamErr = readPluginToastStream(ctx, body, s.updates)
			body = nil
		}
		if ctx.Err() != nil {
			return
		}
		classified := classifyStreamWatchError(streamErr)
		var terminal *StreamWatchTerminalError
		if errors.As(classified, &terminal) {
			s.err = classified
			return
		}
		failure++
		if err := waitForRetry(ctx, failure); err != nil {
			return
		}
		body, streamErr = session.transport.StreamPluginToasts(ctx, session.id)
	}
}

// readPluginToastStream delivers validated toasts in wire order until the
// stream ends, returning nil for a clean end or cancellation and wrapping
// terminal failures in *StreamWatchTerminalError.
func readPluginToastStream(ctx context.Context, body io.ReadCloser, updates chan<- protocol.PluginToast) error {
	watched, stop := httpapi.WatchStreamIdle(ctx, body, PluginToastStreamIdleLimit)
	defer stop()
	defer body.Close()
	err := clienttransport.ReadPluginToasts(watched, func(toast protocol.PluginToast) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case updates <- toast:
			return nil
		}
	})
	if ctx.Err() != nil {
		return nil
	}
	return classifyStreamWatchError(err)
}
