package kit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
)

const pullRequestTestSession = "session_0123456789abcdef0123456789abcdef"

const pullRequestTestRecord = `{"sessionId":"` + pullRequestTestSession + `","cwd":"/repo","branch":"main","pullRequest":{"number":42,"url":"https://github.com/a/b/pull/42","state":"open","draft":false,"checks":{"state":"failing","required":"unsatisfied","counts":{"passed":1,"failed":1,"pending":0,"skipped":0,"canceled":0,"unknown":0},"items":[{"name":"test","workflow":"CI","state":"failed","required":true},{"name":"lint","state":"passed","required":false}],"omitted":0}}}`

type pullRequestTransport struct {
	sessionTransport
	opens  int
	frames []string
	err    error
}

func (t *pullRequestTransport) StreamSessionPullRequest(context.Context, string) (io.ReadCloser, error) {
	t.opens++
	if t.err != nil {
		return nil, t.err
	}
	frame := ""
	if len(t.frames) > 0 {
		frame, t.frames = t.frames[0], t.frames[1:]
	}
	return io.NopCloser(strings.NewReader(frame)), nil
}

func TestWatchPullRequestDeliversStatusAndReconnects(t *testing.T) {
	transport := &pullRequestTransport{frames: []string{"", "event: pullRequest.status\ndata: " + pullRequestTestRecord + "\n\n"}}
	session := newSession(nil, transport, pullRequestTestSession, SessionSnapshot{})
	ctx, cancel := context.WithCancel(t.Context())
	var got protocol.SessionPullRequestStatus
	err := session.WatchPullRequest(ctx, func(status SessionPullRequestStatus) {
		got = status
		cancel()
	})
	if !errors.Is(err, context.Canceled) || transport.opens != 2 {
		t.Fatalf("WatchPullRequest() opens=%d err=%v", transport.opens, err)
	}
	pr := got.PullRequest
	want := protocol.PullRequestChecks{
		State: protocol.PullRequestChecksFailing, Required: protocol.PullRequestRequirementUnsatisfied,
		Counts: protocol.PullRequestCheckCounts{Passed: 1, Failed: 1},
		Items:  []protocol.PullRequestCheck{{Name: "test", Workflow: "CI", State: protocol.PullRequestCheckFailed, Required: true}, {Name: "lint", State: protocol.PullRequestCheckPassed}},
	}
	if got.Branch != "main" || pr == nil || pr.Number != 42 || pr.State != protocol.PullRequestOpen || pr.Checks.State != want.State || pr.Checks.Required != want.Required || pr.Checks.Counts != want.Counts || len(pr.Checks.Items) != 2 || pr.Checks.Items[0] != want.Items[0] || pr.Checks.Items[1] != want.Items[1] {
		t.Fatalf("status = %+v", got)
	}
}

func TestWatchPullRequestReportsAServerThatPredatesIt(t *testing.T) {
	transport := &pullRequestTransport{err: &httpapi.StreamError{Err: httpapi.ErrOperationNotServed}}
	session := newSession(nil, transport, pullRequestTestSession, SessionSnapshot{})
	err := session.WatchPullRequest(t.Context(), func(SessionPullRequestStatus) { t.Fatal("unexpected delivery") })
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Capability != "watchPullRequest" || transport.opens != 1 {
		t.Fatalf("WatchPullRequest() opens=%d err=%v, want one unsupported attempt", transport.opens, err)
	}
}

func TestWatchPullRequestStopsOnAnotherSessionsStatus(t *testing.T) {
	other := strings.Replace(pullRequestTestRecord, pullRequestTestSession, "session_ffffffffffffffffffffffffffffffff", 1)
	transport := &pullRequestTransport{frames: []string{"event: pullRequest.status\ndata: " + other + "\n\n"}}
	session := newSession(nil, transport, pullRequestTestSession, SessionSnapshot{})
	err := session.WatchPullRequest(t.Context(), func(SessionPullRequestStatus) { t.Fatal("wrong-session update delivered") })
	var terminal *StreamWatchTerminalError
	if !errors.As(err, &terminal) || transport.opens != 1 {
		t.Fatalf("WatchPullRequest() opens=%d err=%v, want one terminal attempt", transport.opens, err)
	}
}

func TestWatchPullRequestStopsOnADeclaredNotFound(t *testing.T) {
	transport := &pullRequestTransport{err: &httpapi.APIError{StatusCode: http.StatusNotFound, Code: "not_found", Message: "session not found"}}
	session := newSession(nil, transport, pullRequestTestSession, SessionSnapshot{})
	err := session.WatchPullRequest(t.Context(), func(SessionPullRequestStatus) {})
	var terminal *StreamWatchTerminalError
	if !errors.As(err, &terminal) || errors.Is(err, ErrUnsupported) {
		t.Fatalf("WatchPullRequest() err=%v, want a terminal not-found failure", err)
	}
}

func TestAnUnroutedPullRequestStreamIsNotServed(t *testing.T) {
	transport := testSessionTransport(func(context.Context, string, string, io.Reader, bool) (*http.Response, error) {
		header := http.Header{}
		header.Set("Content-Type", "text/plain; charset=utf-8")
		return &http.Response{StatusCode: http.StatusNotFound, Header: header, Body: io.NopCloser(strings.NewReader("404 page not found\n"))}, nil
	})
	_, err := httpapi.OpenStream(t.Context(), transport, httpapi.StreamSessionPullRequest, httpapi.SessionPath{SessionID: pullRequestTestSession})
	if !errors.Is(err, httpapi.ErrOperationNotServed) {
		t.Fatalf("unrouted open = %v, want ErrOperationNotServed", err)
	}
}
