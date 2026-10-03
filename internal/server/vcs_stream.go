package server

import (
	"context"
	"errors"
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/vcs"
)

type vcsSource interface {
	Next(context.Context) (protocol.SessionVCSStatus, error)
	Close()
}
type runtimeVCSSource struct {
	id     string
	source session.VCSSubscription
}

func (s runtimeVCSSource) Close() { s.source.Close() }
func (s runtimeVCSSource) Next(ctx context.Context) (protocol.SessionVCSStatus, error) {
	value, err := s.source.Next(ctx)
	if err != nil {
		return protocol.SessionVCSStatus{}, err
	}
	result := projectVCSStatus(s.id, value)
	return result, result.Validate()
}
func (s runtimeSessionService) SubscribeVCS(ctx context.Context, id string) (vcsSource, error) {
	source, err := s.manager.SubscribeVCS(ctx, id)
	if err != nil {
		return nil, err
	}
	return runtimeVCSSource{id, source}, nil
}
func projectVCSStatus(id string, value session.VCSUpdate) protocol.SessionVCSStatus {
	result := protocol.SessionVCSStatus{SessionID: id, CWD: value.CWD}
	if value.Status != nil {
		status := value.Status
		result.Status = &protocol.VCSStatus{Root: status.Root, Head: protocol.VCSHead{Kind: protocol.VCSHeadKind(status.HeadKind), Name: status.HeadName, OID: status.HeadOID}, Dirty: status.Dirty}
		if status.PullRequest != nil {
			result.Status.PullRequest = &protocol.GitHubPullRequest{Number: status.PullRequest.Number, URL: status.PullRequest.URL}
		}
	}
	// Preserve the cwd even when local metadata cannot safely cross the boundary.
	if result.Validate() != nil {
		result.Status = nil
	}
	return result
}

// vcsStreamSource adapts a VCS subscription to the shared stream writer.
type vcsStreamSource struct{ source vcsSource }

func (s vcsStreamSource) Close() { s.source.Close() }
func (s vcsStreamSource) Next(ctx context.Context) (httpapi.StreamRecord[protocol.SessionVCSStatus], error) {
	value, err := s.source.Next(ctx)
	return httpapi.StreamRecord[protocol.SessionVCSStatus]{Name: httpapi.VCSStatusRecord, Payload: value}, err
}

func registerVCSRoutes(mux *http.ServeMux, options httpapi.ServeOptions, service sessionService) {
	httpapi.Handle(mux, options, httpapi.GetSessionVCS, func(ctx context.Context, params httpapi.SessionPath, _ httpapi.NoBody) (protocol.SessionVCSStatus, error) {
		result, err := service.VCS(ctx, params.SessionID)
		if err != nil {
			return protocol.SessionVCSStatus{}, vcsAPIError(err)
		}
		if result.Validate() != nil {
			return protocol.SessionVCSStatus{}, httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error", nil)
		}
		return result, nil
	})
	httpapi.HandleStream(mux, options, httpapi.StreamSessionVCS, func(ctx context.Context, params httpapi.SessionPath) (httpapi.StreamSource[protocol.SessionVCSStatus], error) {
		source, err := service.SubscribeVCS(ctx, params.SessionID)
		if err != nil {
			return nil, vcsAPIError(err)
		}
		return vcsStreamSource{source}, nil
	})
}

// vcsAPIError maps repository-status failures to the declared ADR 0034 codes.
func vcsAPIError(err error) error {
	switch {
	case errors.Is(err, vcs.ErrSubscriberLimit):
		return httpapi.NewAPIError(http.StatusTooManyRequests, httpapi.ErrorCapacityExceeded, "VCS subscriber limit exceeded", nil)
	case errors.Is(err, session.ErrNotFound):
		return httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "session not found", nil)
	case errors.Is(err, session.ErrDeleteBusy):
		return httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorConflict, "session is being deleted", nil)
	case errors.Is(err, session.ErrVCSUnavailable), errors.Is(err, session.ErrClosed),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The observer or runtime closed underneath the request.
		return httpapi.NewAPIError(http.StatusServiceUnavailable, httpapi.ErrorUnavailable, "VCS observation unavailable", nil)
	case errors.Is(err, session.ErrInvalidInput), errors.Is(err, errInvalidSessionRequest):
		return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, "invalid request", nil)
	}
	return httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error", nil)
}
