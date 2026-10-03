package clienttransport

import (
	"context"
	"io"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
)

// StreamPluginToasts opens the authenticated live-only plugin notification stream.
func (c *Client) StreamPluginToasts(ctx context.Context, id string) (io.ReadCloser, error) {
	return httpapi.OpenStream(ctx, c, httpapi.StreamPluginToasts, httpapi.SessionPath{SessionID: id})
}

// ReadPluginToasts validates bounded SSE records and invokes receive in wire order.
func ReadPluginToasts(body io.Reader, receive func(protocol.PluginToast) error) error {
	return httpapi.ReadStream(body, httpapi.StreamPluginToasts, func(record httpapi.StreamRecord[protocol.PluginToast]) error {
		return receive(record.Payload)
	})
}
