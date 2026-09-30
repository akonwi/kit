package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/vcs"
	"github.com/getkin/kin-openapi/openapi3"
)

type vcsWireTestService struct {
	sessionService
	status protocol.SessionVCSStatus
	source vcsSource
	err    error
}

func (s vcsWireTestService) VCS(context.Context, string) (protocol.SessionVCSStatus, error) {
	return s.status, s.err
}
func (s vcsWireTestService) SubscribeVCS(context.Context, string) (vcsSource, error) {
	return s.source, s.err
}

type vcsSequenceSource struct {
	values []protocol.SessionVCSStatus
	closed bool
}

func (s *vcsSequenceSource) Next(context.Context) (protocol.SessionVCSStatus, error) {
	if len(s.values) == 0 {
		return protocol.SessionVCSStatus{}, io.EOF
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, nil
}
func (s *vcsSequenceSource) Close() { s.closed = true }

const vcsTestSessionID = "session_0123456789abcdef0123456789abcdef"

func vcsContractStatuses() []protocol.SessionVCSStatus {
	return []protocol.SessionVCSStatus{
		{SessionID: vcsTestSessionID, CWD: "/repo"},
		{SessionID: vcsTestSessionID, CWD: "/repo/sub", Status: &protocol.VCSStatus{Root: "/repo", Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"}, Dirty: false,
			PullRequest: &protocol.GitHubPullRequest{Number: 42, URL: "https://github.com/akonwi/kit/pull/42"}}},
		{SessionID: vcsTestSessionID, CWD: "/repo", Status: &protocol.VCSStatus{Root: "/repo", Head: protocol.VCSHead{Kind: protocol.VCSHeadDetached, OID: strings.Repeat("a", 40)}, Dirty: true}},
		{SessionID: vcsTestSessionID, CWD: "/repo", Status: &protocol.VCSStatus{Root: "/repo", Head: protocol.VCSHead{Kind: protocol.VCSHeadUnborn, Name: "trunk"}}},
	}
}

func serveVCSContract(t *testing.T, service sessionService, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	registerSessionRoutes(mux, service)
	var contractErr error
	handler := contractConformanceMiddleware(t, mux, func(err error) { contractErr = err })
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+vcsTestSessionID+path, nil)
	addContractRequestHeaders(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if contractErr != nil {
		t.Fatalf("contract: %v body=%s", contractErr, response.Body.String())
	}
	return response
}

func TestGetSessionVCSConformsToContract(t *testing.T) {
	for _, status := range vcsContractStatuses() {
		response := serveVCSContract(t, vcsWireTestService{status: status}, "/vcs")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
		}
	}
}

func TestSessionVCSErrorResponsesConformToContract(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		stream bool
	}{
		{"missing", kitsession.ErrNotFound, http.StatusNotFound, "not_found", false},
		{"deleting", kitsession.ErrDeleteBusy, http.StatusConflict, "conflict", false},
		{"unavailable", kitsession.ErrVCSUnavailable, http.StatusServiceUnavailable, "unavailable", false},
		{"closed", kitsession.ErrClosed, http.StatusServiceUnavailable, "unavailable", false},
		{"observer closed", context.Canceled, http.StatusServiceUnavailable, "unavailable", false},
		{"internal", errors.New("boom"), http.StatusInternalServerError, "internal", false},
		{"stream missing", kitsession.ErrNotFound, http.StatusNotFound, "not_found", true},
		{"stream deleting", kitsession.ErrDeleteBusy, http.StatusConflict, "conflict", true},
		{"stream subscriber limit", vcs.ErrSubscriberLimit, http.StatusTooManyRequests, "capacity_exceeded", true},
		{"stream unavailable", kitsession.ErrVCSUnavailable, http.StatusServiceUnavailable, "unavailable", true},
		{"stream closed", kitsession.ErrClosed, http.StatusServiceUnavailable, "unavailable", true},
		{"stream internal", errors.New("boom"), http.StatusInternalServerError, "internal", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := "/vcs"
			if test.stream {
				path = "/vcs/events"
			}
			response := serveVCSContract(t, vcsWireTestService{err: test.err}, path)
			if response.Code != test.status || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("response = %d %v body=%s", response.Code, response.Header(), response.Body.String())
			}
			var envelope struct {
				Error struct {
					Code    string          `json:"code"`
					Message string          `json:"message"`
					Details json.RawMessage `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != test.code || envelope.Error.Message == "" || envelope.Error.Details != nil {
				t.Fatalf("error body = %s, want code %s", response.Body.String(), test.code)
			}
			if test.status == http.StatusInternalServerError && envelope.Error.Message != "internal server error" {
				t.Fatalf("internal message = %q", envelope.Error.Message)
			}
		})
	}
}

func TestSessionVCSStreamRecordsConformToPublishedPayloadSchema(t *testing.T) {
	document, err := openapi3.NewLoader().LoadFromFile("../../api/kit-session.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	operation := document.Paths.Find("/v1/sessions/{sessionID}/vcs/events").Get
	schema := operation.Responses.Status(http.StatusOK).Value.Content.Get("text/event-stream").Schema.Value
	source := &vcsSequenceSource{values: vcsContractStatuses()}
	response := serveVCSContract(t, vcsWireTestService{source: source}, "/vcs/events")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" || !source.closed {
		t.Fatalf("response = %d %v closed=%t", response.Code, response.Header(), source.closed)
	}
	records := strings.Split(strings.TrimSuffix(response.Body.String(), "\n\n"), "\n\n")
	if len(records) != 1+len(vcsContractStatuses()) || records[0] != ": connected" {
		t.Fatalf("records = %q", records)
	}
	for index, record := range records[1:] {
		name, data, ok := strings.Cut(record, "\n")
		if !ok || name != "event: vcs.status" || !strings.HasPrefix(data, "data: ") || strings.Contains(data, "\n") {
			t.Fatalf("record %d = %q", index, record)
		}
		var value any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data: ")), &value); err != nil {
			t.Fatal(err)
		}
		if err := schema.VisitJSON(value); err != nil {
			t.Fatalf("record %d does not match SessionVCSStatus: %v", index, err)
		}
	}
	var decoded []protocol.SessionVCSStatus
	if err := ReadSessionVCS(response.Body, func(value protocol.SessionVCSStatus) error { decoded = append(decoded, value); return nil }); err != nil || len(decoded) != len(vcsContractStatuses()) {
		t.Fatalf("decoded %d records: %v", len(decoded), err)
	}
}
