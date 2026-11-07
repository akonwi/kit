package server

import (
	"context"
	"reflect"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/pullrequest"
	"github.com/akonwi/kit/internal/session"
)

type pullRequestSource interface {
	Next(context.Context) (protocol.SessionPullRequestStatus, error)
	Close()
}

// runtimePullRequestSource projects the session's latest-only rich
// observation and skips states whose projection did not change, such as a
// changed review that clients do not receive.
type runtimePullRequestSource struct {
	id       string
	source   session.PullRequestStatusSubscription
	previous *protocol.SessionPullRequestStatus
}

func (s *runtimePullRequestSource) Close() { s.source.Close() }
func (s *runtimePullRequestSource) Next(ctx context.Context) (protocol.SessionPullRequestStatus, error) {
	for {
		value, err := s.source.Next(ctx)
		if err != nil {
			return protocol.SessionPullRequestStatus{}, err
		}
		result := projectPullRequestStatus(s.id, value)
		if s.previous != nil && reflect.DeepEqual(*s.previous, result) {
			continue
		}
		s.previous = &result
		return result, result.Validate()
	}
}

func (s runtimeSessionService) WatchPullRequest(ctx context.Context, id string) (pullRequestSource, error) {
	source, err := s.manager.WatchPullRequestStatus(ctx, id)
	if err != nil {
		return nil, err
	}
	return &runtimePullRequestSource{id: id, source: source}, nil
}

// projectPullRequestStatus projects one observation onto the wire. A branch
// or pull request that cannot cross the boundary safely is dropped while the
// cwd is kept.
func projectPullRequestStatus(id string, value session.PullRequestStatusUpdate) protocol.SessionPullRequestStatus {
	result := protocol.SessionPullRequestStatus{SessionID: id, CWD: value.CWD, Branch: value.Branch}
	if status := value.Status; status != nil && value.Branch != "" {
		result.PullRequest = &protocol.PullRequestStatus{
			Number: status.Number, URL: status.URL, State: protocol.PullRequestState(strings.ToLower(status.State)),
			Draft: status.Draft, Checks: projectPullRequestChecks(pullrequest.SummarizeChecks(status)),
		}
	}
	if result.Validate() != nil {
		result.PullRequest = nil
		if result.Validate() != nil {
			result.Branch = ""
		}
	}
	return result
}

func projectPullRequestChecks(summary pullrequest.ChecksSummary) protocol.PullRequestChecks {
	counts := summary.Counts
	result := protocol.PullRequestChecks{
		State: protocol.PullRequestChecksState(summary.State), Required: protocol.PullRequestRequirement(summary.Required),
		Counts: protocol.PullRequestCheckCounts{Passed: counts.Passed, Failed: counts.Failed, Pending: counts.Pending, Skipped: counts.Skipped, Canceled: counts.Canceled, Unknown: counts.Unknown},
		Items:  make([]protocol.PullRequestCheck, 0, len(summary.Checks)), Omitted: summary.Omitted,
	}
	for _, check := range summary.Checks {
		result.Items = append(result.Items, protocol.PullRequestCheck{Name: check.Name, Workflow: check.Workflow, State: protocol.PullRequestCheckState(check.State), Required: check.Required})
	}
	return result
}

// pullRequestStreamSource adapts a pull request subscription to the shared
// stream writer.
type pullRequestStreamSource struct{ source pullRequestSource }

func (s pullRequestStreamSource) Close() { s.source.Close() }
func (s pullRequestStreamSource) Next(ctx context.Context) (httpapi.StreamRecord[protocol.SessionPullRequestStatus], error) {
	value, err := s.source.Next(ctx)
	return httpapi.StreamRecord[protocol.SessionPullRequestStatus]{Name: httpapi.PullRequestStatusRecord, Payload: value}, err
}
