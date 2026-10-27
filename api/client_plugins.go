package kit

import (
	"context"
	"io"

	protocol "github.com/akonwi/kit/api/contract"
)

// clientPluginHosting is the attachment a hosting stream renews on every
// connection.
type clientPluginHosting struct {
	instanceID string
	input      protocol.AttachClientPluginInput
}

// HostPlugin attaches a client-hosted plugin instance to the session and
// watches the session on an event stream bound to it (ADR 0051). Updates are
// those of Watch; client_plugin.call.requested events carry their call only
// on this stream, and the caller answers each with CompleteClientPluginCall.
// The stream attaches the instance again before every reconnect, so the
// instance survives a server restart or runtime reload. Closing the stream
// starts the instance's grace period; DetachPlugin stops it at once.
func (c *Session) HostPlugin(ctx context.Context, instanceID string, input protocol.AttachClientPluginInput) (*SessionUpdateStream, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	input.Tools = append([]protocol.ClientPluginToolDeclaration(nil), input.Tools...)
	if err := c.transport.AttachClientPlugin(operation, c.id, instanceID, input); err != nil {
		cancel()
		return nil, projectError(err)
	}
	snapshot, err := c.Snapshot(operation)
	if err != nil {
		cancel()
		return nil, projectError(err)
	}
	stream := &SessionUpdateStream{
		updates: make(chan SessionUpdate, 16), done: make(chan struct{}), cancel: cancel,
		hosting: &clientPluginHosting{instanceID: instanceID, input: input},
	}
	go stream.run(operation, c, snapshot)
	return stream, nil
}

// DetachPlugin stops a client-hosted plugin instance; its pending calls end
// as unavailable.
func (c *Session) DetachPlugin(ctx context.Context, instanceID string) error {
	return sessionError(c, ctx, func(operation context.Context) error {
		return c.transport.DetachClientPlugin(operation, c.id, instanceID)
	})
}

// CompleteClientPluginCall answers a pending call to a client-hosted plugin
// instance. A call that already ended fails with a conflict error.
func (c *Session) CompleteClientPluginCall(ctx context.Context, instanceID, callID string, input protocol.CompleteClientPluginCallInput) error {
	return sessionError(c, ctx, func(operation context.Context) error {
		return c.transport.CompleteClientPluginCall(operation, c.id, instanceID, callID, input)
	})
}

// open opens one connection of the stream, renewing its client plugin
// attachment first when it hosts one.
func (s *SessionUpdateStream) open(ctx context.Context, session *Session, streamID string, cursor int64) (io.ReadCloser, error) {
	if s.hosting == nil {
		return session.transport.StreamSessionEvents(ctx, session.id, streamID, cursor)
	}
	if err := session.transport.AttachClientPlugin(ctx, session.id, s.hosting.instanceID, s.hosting.input); err != nil {
		return nil, err
	}
	return session.transport.StreamClientPluginSessionEvents(ctx, session.id, streamID, cursor, s.hosting.instanceID)
}
