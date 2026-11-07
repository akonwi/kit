package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/pullrequest"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/vcs"
	"github.com/getkin/kin-openapi/openapi3"
)

type pullRequestWireTestService struct {
	sessionService
	source pullRequestSource
	err    error
}

func (s pullRequestWireTestService) WatchPullRequest(context.Context, string) (pullRequestSource, error) {
	return s.source, s.err
}

type pullRequestSequenceSource struct {
	values []protocol.SessionPullRequestStatus
	closed bool
}

func (s *pullRequestSequenceSource) Next(context.Context) (protocol.SessionPullRequestStatus, error) {
	if len(s.values) == 0 {
		return protocol.SessionPullRequestStatus{}, io.EOF
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, value.Validate()
}
func (s *pullRequestSequenceSource) Close() { s.closed = true }

func failingPullRequestStatus() *pullrequest.Status {
	return &pullrequest.Status{
		Number: 42, URL: "https://github.com/akonwi/kit/pull/42", HeadRefName: "main", State: "OPEN", Draft: true,
		RequiredChecks: pullrequest.RequirementUnsatisfied,
		Checks: []pullrequest.Check{
			{Name: "build", Workflow: "CI", State: pullrequest.CheckPassed, Required: true},
			{Name: "test", Workflow: "CI", State: pullrequest.CheckFailed, Required: true},
			{Name: "lint", Workflow: "CI", State: pullrequest.CheckPending},
		},
		Reviews: []pullrequest.Review{{Author: "reviewer", State: "APPROVED"}},
	}
}

func pullRequestContractStatuses() []protocol.SessionPullRequestStatus {
	return []protocol.SessionPullRequestStatus{
		{SessionID: vcsTestSessionID, CWD: "/repo"},
		{SessionID: vcsTestSessionID, CWD: "/repo", Branch: "main"},
		projectPullRequestStatus(vcsTestSessionID, kitsession.PullRequestStatusUpdate{CWD: "/repo", Root: "/repo", Branch: "main", Status: failingPullRequestStatus()}),
		projectPullRequestStatus(vcsTestSessionID, kitsession.PullRequestStatusUpdate{CWD: "/repo", Root: "/repo", Branch: "main", Status: &pullrequest.Status{Number: 7, URL: "https://github.com/akonwi/kit/pull/7", State: "MERGED"}}),
	}
}

func TestProjectPullRequestStatusSummarizesChecks(t *testing.T) {
	got := projectPullRequestStatus(vcsTestSessionID, kitsession.PullRequestStatusUpdate{CWD: "/repo", Root: "/repo", Branch: "main", Status: failingPullRequestStatus()})
	want := protocol.SessionPullRequestStatus{
		SessionID: vcsTestSessionID, CWD: "/repo", Branch: "main",
		PullRequest: &protocol.PullRequestStatus{Number: 42, URL: "https://github.com/akonwi/kit/pull/42", State: protocol.PullRequestOpen, Draft: true, Checks: protocol.PullRequestChecks{
			State: protocol.PullRequestChecksFailing, Required: protocol.PullRequestRequirementUnsatisfied,
			Counts: protocol.PullRequestCheckCounts{Passed: 1, Failed: 1, Pending: 1},
			Items: []protocol.PullRequestCheck{
				{Name: "test", Workflow: "CI", State: protocol.PullRequestCheckFailed, Required: true},
				{Name: "lint", Workflow: "CI", State: protocol.PullRequestCheckPending},
				{Name: "build", Workflow: "CI", State: protocol.PullRequestCheckPassed, Required: true},
			},
		}},
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("projection =\n%s\nwant\n%s", gotJSON, wantJSON)
	}
	none := projectPullRequestStatus(vcsTestSessionID, kitsession.PullRequestStatusUpdate{CWD: "/repo", Root: "/repo", Branch: "main", Status: &pullrequest.Status{Number: 1, URL: "https://github.com/a/b/pull/1", State: "OPEN"}})
	if encoded, _ := json.Marshal(none.PullRequest.Checks); string(encoded) != `{"state":"none","required":"unknown","counts":{"passed":0,"failed":0,"pending":0,"skipped":0,"canceled":0,"unknown":0},"items":[],"omitted":0}` {
		t.Fatalf("no-check projection = %s", encoded)
	}
}

func TestProjectPullRequestStatusBoundsTheCheckList(t *testing.T) {
	status := &pullrequest.Status{Number: 42, URL: "https://github.com/akonwi/kit/pull/42", State: "OPEN", RequiredChecks: pullrequest.RequirementSatisfied}
	for range 96 {
		status.Checks = append(status.Checks, pullrequest.Check{Name: strings.Repeat("n", 300), Workflow: "w\x00f", State: pullrequest.CheckPassed})
	}
	got := projectPullRequestStatus(vcsTestSessionID, kitsession.PullRequestStatusUpdate{CWD: "/repo", Branch: "main", Status: status})
	if err := got.Validate(); err != nil || got.PullRequest == nil {
		t.Fatalf("projection invalid: %v %+v", err, got)
	}
	checks := got.PullRequest.Checks
	if len(checks.Items) != protocol.MaxPullRequestChecks || checks.Omitted != 46 || checks.Counts.Passed != 96 || len(checks.Items[0].Name) != protocol.MaxPullRequestCheckTextBytes || checks.Items[0].Workflow != "wf" {
		t.Fatalf("listed %d omitted %d counts %+v first %q/%q", len(checks.Items), checks.Omitted, checks.Counts, checks.Items[0].Name[:8], checks.Items[0].Workflow)
	}
	if encoded, _ := json.Marshal(got); len(encoded) > 32<<10 {
		t.Fatalf("a bounded record encodes to %d bytes", len(encoded))
	}
}

func TestProjectPullRequestStatusDropsWhatCannotCrossTheBoundary(t *testing.T) {
	unsafe := failingPullRequestStatus()
	unsafe.URL = "javascript:alert(1)"
	got := projectPullRequestStatus(vcsTestSessionID, kitsession.PullRequestStatusUpdate{CWD: "/repo", Branch: "main", Status: unsafe})
	if got.Branch != "main" || got.PullRequest != nil || got.Validate() != nil {
		t.Fatalf("unsafe URL projection = %+v", got)
	}
	got = projectPullRequestStatus(vcsTestSessionID, kitsession.PullRequestStatusUpdate{CWD: "/repo", Branch: "main\n", Status: failingPullRequestStatus()})
	if got.CWD != "/repo" || got.Branch != "" || got.PullRequest != nil || got.Validate() != nil {
		t.Fatalf("unsafe branch projection = %+v", got)
	}
	got = projectPullRequestStatus(vcsTestSessionID, kitsession.PullRequestStatusUpdate{CWD: "/repo", Status: failingPullRequestStatus()})
	if got.PullRequest != nil || got.Validate() != nil {
		t.Fatalf("detached projection = %+v", got)
	}
}

type fakeLatestPullRequestStatus struct {
	values []kitsession.PullRequestStatusUpdate
	closed bool
}

func (s *fakeLatestPullRequestStatus) Next(context.Context) (kitsession.PullRequestStatusUpdate, error) {
	if len(s.values) == 0 {
		return kitsession.PullRequestStatusUpdate{}, io.EOF
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, nil
}
func (s *fakeLatestPullRequestStatus) Close() { s.closed = true }

func TestPullRequestSourceSkipsUnchangedProjections(t *testing.T) {
	failing := failingPullRequestStatus()
	reviewed := pullrequest.Clone(failing)
	reviewed.Reviews = nil
	passed := pullrequest.Clone(failing)
	passed.Checks[1].State = pullrequest.CheckPassed
	update := func(status *pullrequest.Status) kitsession.PullRequestStatusUpdate {
		return kitsession.PullRequestStatusUpdate{CWD: "/repo", Root: "/repo", Branch: "main", Status: status}
	}
	fake := &fakeLatestPullRequestStatus{values: []kitsession.PullRequestStatusUpdate{update(failing), update(reviewed), update(passed), update(passed)}}
	source := &runtimePullRequestSource{id: vcsTestSessionID, source: fake}
	var states []protocol.PullRequestChecksState
	for {
		value, err := source.Next(t.Context())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, value.PullRequest.Checks.State)
	}
	if len(states) != 2 || states[0] != protocol.PullRequestChecksFailing || states[1] != protocol.PullRequestChecksPending {
		t.Fatalf("delivered states = %v, want failing then pending", states)
	}
	source.Close()
	if !fake.closed {
		t.Fatal("closing the source left the subscription open")
	}
}

func TestSessionPullRequestStreamRecordsConformToPublishedPayloadSchema(t *testing.T) {
	document, err := openapi3.NewLoader().LoadFromFile("../../api/kit-session.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	schema := document.Paths.Find("/v1/sessions/{sessionID}/pull-request/events").Get.Responses.Status(http.StatusOK).Value.Content.Get("text/event-stream").Schema.Value
	source := &pullRequestSequenceSource{values: pullRequestContractStatuses()}
	response := serveVCSContract(t, pullRequestWireTestService{source: source}, "/pull-request/events")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" || !source.closed {
		t.Fatalf("response = %d %v closed=%t", response.Code, response.Header(), source.closed)
	}
	records := strings.Split(strings.TrimSuffix(response.Body.String(), "\n\n"), "\n\n")
	if len(records) != 1+len(pullRequestContractStatuses()) || records[0] != ": connected" {
		t.Fatalf("records = %q", records)
	}
	for index, record := range records[1:] {
		name, data, ok := strings.Cut(record, "\n")
		if !ok || name != "event: pullRequest.status" || !strings.HasPrefix(data, "data: ") {
			t.Fatalf("record %d = %q", index, record)
		}
		var value any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data: ")), &value); err != nil {
			t.Fatal(err)
		}
		if err := schema.VisitJSON(value); err != nil {
			t.Fatalf("record %d does not match SessionPullRequestStatus: %v", index, err)
		}
	}
	var decoded []protocol.SessionPullRequestStatus
	if err := ReadSessionPullRequest(response.Body, func(value protocol.SessionPullRequestStatus) error { decoded = append(decoded, value); return nil }); err != nil || len(decoded) != len(pullRequestContractStatuses()) {
		t.Fatalf("decoded %d records: %v", len(decoded), err)
	}
}

func TestSessionPullRequestStreamErrorsConformToContract(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{kitsession.ErrNotFound, http.StatusNotFound, "not_found"},
		{kitsession.ErrDeleteBusy, http.StatusConflict, "conflict"},
		{vcs.ErrSubscriberLimit, http.StatusTooManyRequests, "capacity_exceeded"},
		{kitsession.ErrVCSUnavailable, http.StatusServiceUnavailable, "unavailable"},
		{errors.New("boom"), http.StatusInternalServerError, "internal"},
	} {
		response := serveVCSContract(t, pullRequestWireTestService{err: test.err}, "/pull-request/events")
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if response.Code != test.status || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Error.Code != test.code {
			t.Fatalf("%v: response = %d %s", test.err, response.Code, response.Body.String())
		}
	}
}
