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

type viewRevealSource interface {
	Next(context.Context) (protocol.ViewReveal, error)
	Close()
}

type runtimeViewRevealSource struct {
	sessionID, viewID string
	reveals           <-chan session.ViewReveal
	stop              func()
}

func (s runtimeViewRevealSource) Close() { s.stop() }
func (s runtimeViewRevealSource) Next(ctx context.Context) (protocol.ViewReveal, error) {
	select {
	case <-ctx.Done():
		return protocol.ViewReveal{}, ctx.Err()
	case reveal, ok := <-s.reveals:
		if !ok {
			return protocol.ViewReveal{}, io.EOF
		}
		projected := protocol.ViewReveal{SessionID: s.sessionID, ViewID: s.viewID, Line: reveal.Line, Annotation: reveal.Annotation}
		return projected, projected.Validate()
	}
}

func (s runtimeSessionService) WatchViewReveals(ctx context.Context, sessionID, viewID string) (viewRevealSource, error) {
	reveals, stop, err := s.manager.WatchViewReveals(ctx, sessionID, viewID)
	if err != nil {
		return nil, err
	}
	return runtimeViewRevealSource{sessionID: sessionID, viewID: viewID, reveals: reveals, stop: stop}, nil
}

func (s runtimeSessionService) RevealView(ctx context.Context, sessionID, viewID string, input protocol.ViewRevealInput) (bool, error) {
	return s.manager.RevealView(ctx, sessionID, viewID, session.ViewReveal{Line: input.Line, Annotation: input.Annotation})
}

type viewRevealStreamSource struct{ source viewRevealSource }

func (s viewRevealStreamSource) Close() { s.source.Close() }
func (s viewRevealStreamSource) Next(ctx context.Context) (httpapi.StreamRecord[protocol.ViewReveal], error) {
	value, err := s.source.Next(ctx)
	return httpapi.StreamRecord[protocol.ViewReveal]{Name: httpapi.ViewRevealRecord, Payload: value}, err
}

// registerViewRoutes serves the live view reveal relay (ADR 0068).
func registerViewRoutes(mux *http.ServeMux, options httpapi.ServeOptions, service sessionService) {
	httpapi.HandleStream(mux, options, httpapi.StreamViewReveals, func(ctx context.Context, params httpapi.ViewPath) (httpapi.StreamSource[protocol.ViewReveal], error) {
		if !protocol.ValidViewID(params.ViewID) {
			return nil, invalidViewID()
		}
		source, err := service.WatchViewReveals(ctx, params.SessionID, params.ViewID)
		if err != nil {
			return nil, viewAPIError(err)
		}
		return viewRevealStreamSource{source}, nil
	})
	httpapi.Handle(mux, options, httpapi.RevealView, func(ctx context.Context, params httpapi.ViewPath, input protocol.ViewRevealInput) (protocol.ViewRevealResult, error) {
		if !protocol.ValidViewID(params.ViewID) {
			return protocol.ViewRevealResult{}, invalidViewID()
		}
		if err := input.Validate(); err != nil {
			return protocol.ViewRevealResult{}, httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, "a reveal names exactly one of a line and an annotation", nil)
		}
		delivered, err := service.RevealView(ctx, params.SessionID, params.ViewID, input)
		if err != nil {
			return protocol.ViewRevealResult{}, viewAPIError(err)
		}
		return protocol.ViewRevealResult{Delivered: delivered}, nil
	})
}

func invalidViewID() error {
	return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, "view id must be view_ and 32 lowercase hex digits", nil)
}

// viewAPIError maps session failures to the declared view route errors.
func viewAPIError(err error) error {
	switch {
	case errors.Is(err, session.ErrViewRevealCapacity):
		return httpapi.NewAPIError(http.StatusTooManyRequests, httpapi.ErrorCapacityExceeded, "view reveal watcher limit exceeded", nil)
	case errors.Is(err, session.ErrNotFound):
		return httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "session not found", nil)
	case errors.Is(err, session.ErrDeleteBusy):
		return httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorConflict, "session is being deleted", nil)
	case errors.Is(err, session.ErrClosed), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return httpapi.NewAPIError(http.StatusServiceUnavailable, httpapi.ErrorUnavailable, "session runtime unavailable", nil)
	case errors.Is(err, session.ErrInvalidInput):
		return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, "invalid request", nil)
	}
	return httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error", nil)
}
