package client

import (
	"context"

	"github.com/akonwi/kit/internal/protocol"
	kitserver "github.com/akonwi/kit/internal/server"
	"github.com/akonwi/kit/internal/sessionclient"
)

var _ sessionclient.PluginToastSession = (*localSession)(nil)

type pluginToastStream struct {
	updates chan protocol.PluginToast
	err     error
}

func (s *pluginToastStream) Updates() <-chan protocol.PluginToast { return s.updates }
func (s *pluginToastStream) Err() error                           { return s.err }

func (c *localSession) WatchPluginToasts(ctx context.Context) (sessionclient.PluginToastStream, error) {
	body, err := c.transport.StreamPluginToasts(ctx, c.id)
	if err != nil {
		return nil, err
	}
	stream := &pluginToastStream{updates: make(chan protocol.PluginToast, 16)}
	go func() {
		defer close(stream.updates)
		defer body.Close()
		stream.err = kitserver.ReadPluginToasts(body, func(toast protocol.PluginToast) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case stream.updates <- toast:
				return nil
			}
		})
	}()
	return stream, nil
}
