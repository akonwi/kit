package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/akonwi/kit/internal/protocol"
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

func serveVCS(writer http.ResponseWriter, request *http.Request, service sessionService) {
	source, err := service.SubscribeVCS(request.Context(), request.PathValue("sessionID"))
	if err != nil {
		switch {
		case errors.Is(err, vcs.ErrSubscriberLimit):
			http.Error(writer, "VCS subscriber limit exceeded", http.StatusTooManyRequests)
		case errors.Is(err, session.ErrVCSUnavailable):
			http.Error(writer, "VCS observation unavailable", http.StatusServiceUnavailable)
		default:
			writeSessionError(writer, err)
		}
		return
	}
	defer source.Close()
	writer.Header().Set("Content-Type", "application/x-ndjson")
	writer.Header().Set("Cache-Control", "no-store")
	controller := http.NewResponseController(writer)
	defer controller.SetWriteDeadline(time.Time{})
	_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if controller.Flush() != nil {
		return
	}
	for request.Context().Err() == nil {
		ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
		value, err := source.Next(ctx)
		cancel()
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if errors.Is(err, context.DeadlineExceeded) && request.Context().Err() == nil {
			if _, err := io.WriteString(writer, "\n"); err != nil {
				return
			}
		} else if err != nil {
			return
		} else {
			if value.Validate() != nil {
				return
			}
			// NDJSON is not HTML. Avoid HTML escaping multiplying valid path
			// and branch bounds beyond the stream's 64 KiB frame budget.
			var frame bytes.Buffer
			encoder := json.NewEncoder(&frame)
			encoder.SetEscapeHTML(false)
			if err := encoder.Encode(value); err != nil || frame.Len() > 64<<10 {
				return
			}
			if _, err := writer.Write(frame.Bytes()); err != nil {
				return
			}
		}
		if controller.Flush() != nil {
			return
		}
	}
}
