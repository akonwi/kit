// Package toastbridge exposes the typed plugin-toast stream to Ard without
// requiring the renderer to instantiate Go generic stream operations.
package toastbridge

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/protocol"
)

// Watch reads fresh plugin toast notifications until ctx is canceled, the
// stream closes, or the server returns an error. Each validated notification is
// delivered synchronously to receive.
func Watch(ctx context.Context, transport httpapi.Transport, sessionID string, receive func(protocol.PluginToast)) error {
	body, err := httpapi.OpenStream(ctx, transport, httpapi.StreamPluginToasts, httpapi.SessionPath{SessionID: sessionID})
	if err != nil {
		return err
	}
	defer body.Close()
	return httpapi.ReadStream(body, httpapi.StreamPluginToasts, func(record httpapi.StreamRecord[protocol.PluginToast]) error {
		receive(record.Payload)
		return nil
	})
}

// IsTerminal reports a non-transient HTTP rejection of the subscription.
// Clean EOFs and transport/server failures are retried by the Ard owner.
func IsTerminal(err error) bool {
	if err == nil || errors.Is(err, io.EOF) {
		return false
	}
	var api *httpapi.APIError
	return errors.As(err, &api) && api.StatusCode != http.StatusServiceUnavailable && api.StatusCode != http.StatusInternalServerError
}
