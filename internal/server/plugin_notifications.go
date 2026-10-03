package server

import (
	"context"
	"errors"
	"io"
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/session"
)

type pluginToastSource interface {
	Next(context.Context) (protocol.PluginToast, error)
	Close()
}

type runtimePluginToastSource struct {
	updates     <-chan session.PluginToast
	unsubscribe func()
}

func (s runtimePluginToastSource) Close() { s.unsubscribe() }
func (s runtimePluginToastSource) Next(ctx context.Context) (protocol.PluginToast, error) {
	select {
	case <-ctx.Done():
		return protocol.PluginToast{}, ctx.Err()
	case toast, ok := <-s.updates:
		if !ok {
			return protocol.PluginToast{}, io.EOF
		}
		projected := protocol.PluginToast{PluginID: toast.PluginID, Instance: toast.Instance, Title: toast.Title, Subtitle: toast.Subtitle, Variant: protocol.PluginToastVariant(toast.Variant), Persistent: toast.Persistent}
		return projected, projected.Validate()
	}
}
func (s runtimeSessionService) SubscribePluginToasts(ctx context.Context, id string) (pluginToastSource, error) {
	updates, unsubscribe, err := s.manager.SubscribePluginToasts(ctx, id)
	if err != nil {
		return nil, err
	}
	return runtimePluginToastSource{updates, unsubscribe}, nil
}

type pluginToastStreamSource struct{ source pluginToastSource }

func (s pluginToastStreamSource) Close() { s.source.Close() }
func (s pluginToastStreamSource) Next(ctx context.Context) (httpapi.StreamRecord[protocol.PluginToast], error) {
	value, err := s.source.Next(ctx)
	return httpapi.StreamRecord[protocol.PluginToast]{Name: httpapi.PluginToastRecord, Payload: value}, err
}

// StreamPluginToasts opens the authenticated live-only plugin notification stream.
func (c *Client) StreamPluginToasts(ctx context.Context, id string) (io.ReadCloser, error) {
	return httpapi.OpenStream(ctx, c, httpapi.StreamPluginToasts, httpapi.SessionPath{SessionID: id})
}

// ReadPluginToasts validates bounded SSE records and invokes receive in wire order.
func ReadPluginToasts(body io.Reader, receive func(protocol.PluginToast) error) error {
	return httpapi.ReadStream(body, httpapi.StreamPluginToasts, func(record httpapi.StreamRecord[protocol.PluginToast]) error { return receive(record.Payload) })
}

func registerPluginRoutes(mux *http.ServeMux, options httpapi.ServeOptions, service sessionService) {
	httpapi.HandleStream(mux, options, httpapi.StreamPluginToasts, func(ctx context.Context, params httpapi.SessionPath) (httpapi.StreamSource[protocol.PluginToast], error) {
		source, err := service.SubscribePluginToasts(ctx, params.SessionID)
		if err != nil {
			return nil, pluginAPIError(err)
		}
		return pluginToastStreamSource{source}, nil
	})
	httpapi.Handle(mux, options, httpapi.ExecutePluginCommand, func(ctx context.Context, params httpapi.SessionPath, input protocol.PluginCommandInput) (httpapi.NoBody, error) {
		if err := input.Validate(); err != nil {
			return httpapi.NoBody{}, pluginAPIError(errInvalidSessionRequest)
		}
		if err := service.ExecutePluginCommand(ctx, params.SessionID, input); err != nil {
			return httpapi.NoBody{}, pluginAPIError(err)
		}
		return httpapi.NoBody{}, nil
	})
}

// pluginAPIError maps session and plugin failures to declared ADR 0034 errors.
func pluginAPIError(err error) error {
	switch {
	case errors.Is(err, session.ErrPluginCommandUnavailable):
		return httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorCode(protocol.PluginCommandUnavailable), session.ErrPluginCommandUnavailable.Error(), nil)
	case errors.Is(err, session.ErrPluginCommandFailed):
		return httpapi.NewAPIError(http.StatusUnprocessableEntity, httpapi.ErrorCode(protocol.PluginCommandFailed), session.ErrPluginCommandFailed.Error(), nil)
	case errors.Is(err, session.ErrPluginNotificationCapacity):
		return httpapi.NewAPIError(http.StatusTooManyRequests, httpapi.ErrorCapacityExceeded, "plugin notification subscriber limit exceeded", nil)
	case errors.Is(err, session.ErrNotFound):
		return httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "session not found", nil)
	case errors.Is(err, session.ErrDeleteBusy):
		return httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorConflict, "session is being deleted", nil)
	case errors.Is(err, session.ErrClosed), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return httpapi.NewAPIError(http.StatusServiceUnavailable, httpapi.ErrorUnavailable, "session runtime unavailable", nil)
	case errors.Is(err, session.ErrInvalidInput), errors.Is(err, errInvalidSessionRequest):
		return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, "invalid request", nil)
	}
	return httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error", nil)
}
