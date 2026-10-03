package client

import (
	"context"
	"io"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	kitserver "github.com/akonwi/kit/internal/server"
	"github.com/akonwi/kit/internal/sessionclient"
)

var _ sessionclient.PluginToastSession = (*localSession)(nil)

// pluginToastStreamIdleLimit ends a stream with no record or heartbeat for
// three server heartbeat intervals (ADR 0035).
const pluginToastStreamIdleLimit = vcsStreamIdleLimit

type pluginToastStream struct {
	updates chan protocol.PluginToast
	err     error
}

func (s *pluginToastStream) Updates() <-chan protocol.PluginToast { return s.updates }
func (s *pluginToastStream) Err() error                           { return s.err }

func (c *localSession) WatchPluginToasts(ctx context.Context) (sessionclient.PluginToastStream, error) {
	body, err := c.transport.StreamPluginToasts(ctx, c.id)
	if err != nil {
		return nil, classifyStreamWatchError(err)
	}
	stream := &pluginToastStream{updates: make(chan protocol.PluginToast, 16)}
	go func() {
		defer close(stream.updates)
		defer body.Close()
		stream.err = readPluginToastStream(ctx, body, stream.updates)
	}()
	return stream, nil
}

// readPluginToastStream delivers validated toasts in wire order until the
// stream ends, returning nil for a clean end or cancellation and wrapping
// terminal failures in *sessionclient.StreamWatchTerminalError.
func readPluginToastStream(ctx context.Context, body io.ReadCloser, updates chan<- protocol.PluginToast) error {
	watched, stop := httpapi.WatchStreamIdle(ctx, body, pluginToastStreamIdleLimit)
	defer stop()
	err := kitserver.ReadPluginToasts(watched, func(toast protocol.PluginToast) error {
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
