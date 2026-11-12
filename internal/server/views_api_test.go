package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	kitsession "github.com/akonwi/kit/internal/session"
)

var viewTestID = "view_" + strings.Repeat("0a", 16)

type viewWireTestService struct {
	sessionService
	source    viewRevealSource
	delivered bool
	err       error
	revealed  *protocol.ViewRevealInput
	watched   *string
}

func (s viewWireTestService) WatchViewReveals(_ context.Context, _ string, viewID string) (viewRevealSource, error) {
	if s.watched != nil {
		*s.watched = viewID
	}
	return s.source, s.err
}

func (s viewWireTestService) RevealView(_ context.Context, _ string, _ string, input protocol.ViewRevealInput) (bool, error) {
	if s.revealed != nil {
		*s.revealed = input
	}
	return s.delivered, s.err
}

type viewRevealSequence struct {
	values []protocol.ViewReveal
	closed bool
}

func (s *viewRevealSequence) Next(context.Context) (protocol.ViewReveal, error) {
	if len(s.values) == 0 {
		return protocol.ViewReveal{}, io.EOF
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, value.Validate()
}
func (s *viewRevealSequence) Close() { s.closed = true }

func serveViewContract(t *testing.T, service sessionService, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	registerSessionRoutes(mux, service)
	var contractErr error
	handler := contractConformanceMiddleware(t, mux, func(err error) { contractErr = err })
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	request := httptest.NewRequest(method, "/v1/sessions/"+vcsTestSessionID+path, reader)
	addContractRequestHeaders(request)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if contractErr != nil {
		t.Fatalf("contract: %v body=%s", contractErr, response.Body.String())
	}
	return response
}

func TestViewRevealStreamSendsOnlyRecordsOfTheView(t *testing.T) {
	values := []protocol.ViewReveal{
		{SessionID: vcsTestSessionID, ViewID: viewTestID, Line: 40},
		{SessionID: vcsTestSessionID, ViewID: viewTestID, Annotation: 7},
	}
	source := &viewRevealSequence{values: append([]protocol.ViewReveal(nil), values...)}
	var watched string
	response := serveViewContract(t, viewWireTestService{source: source, watched: &watched}, http.MethodGet, "/views/"+viewTestID+"/events", "")
	if response.Code != http.StatusOK || !source.closed || watched != viewTestID {
		t.Fatalf("response = %d closed=%t watched=%q", response.Code, source.closed, watched)
	}
	want := ": connected\n\n" +
		"event: view.reveal\ndata: {\"sessionId\":\"" + vcsTestSessionID + "\",\"viewId\":\"" + viewTestID + "\",\"line\":40}\n\n" +
		"event: view.reveal\ndata: {\"sessionId\":\"" + vcsTestSessionID + "\",\"viewId\":\"" + viewTestID + "\",\"annotation\":7}\n\n"
	if response.Body.String() != want {
		t.Fatalf("body =\n%q\nwant\n%q", response.Body.String(), want)
	}
}

func TestRevealViewReportsWhetherAViewWatched(t *testing.T) {
	for _, delivered := range []bool{true, false} {
		var revealed protocol.ViewRevealInput
		response := serveViewContract(t, viewWireTestService{delivered: delivered, revealed: &revealed}, http.MethodPost, "/views/"+viewTestID+"/reveal", `{"line":12}`)
		var result protocol.ViewRevealResult
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Delivered != delivered || revealed != (protocol.ViewRevealInput{Line: 12}) {
			t.Fatalf("delivered %t: response = %d %s, revealed %+v", delivered, response.Code, response.Body.String(), revealed)
		}
	}
}

func TestViewRoutesRejectInvalidRequestsAndMapSessionErrors(t *testing.T) {
	for _, test := range []struct {
		name, method, path, body string
		err                      error
		status                   int
		code                     string
	}{
		{"uppercase view id", http.MethodGet, "/views/" + strings.ToUpper(viewTestID) + "/events", "", nil, http.StatusBadRequest, "invalid_request"},
		{"short view id", http.MethodPost, "/views/view_1/reveal", `{"line":1}`, nil, http.StatusBadRequest, "invalid_request"},
		{"no target", http.MethodPost, "/views/" + viewTestID + "/reveal", `{}`, nil, http.StatusBadRequest, "invalid_request"},
		{"two targets", http.MethodPost, "/views/" + viewTestID + "/reveal", `{"line":1,"annotation":2}`, nil, http.StatusBadRequest, "invalid_request"},
		{"missing session stream", http.MethodGet, "/views/" + viewTestID + "/events", "", kitsession.ErrNotFound, http.StatusNotFound, "not_found"},
		{"missing session reveal", http.MethodPost, "/views/" + viewTestID + "/reveal", `{"line":1}`, kitsession.ErrNotFound, http.StatusNotFound, "not_found"},
		{"deleting", http.MethodPost, "/views/" + viewTestID + "/reveal", `{"line":1}`, kitsession.ErrDeleteBusy, http.StatusConflict, "conflict"},
		{"too many watchers", http.MethodGet, "/views/" + viewTestID + "/events", "", kitsession.ErrViewRevealCapacity, http.StatusTooManyRequests, "capacity_exceeded"},
		{"closed runtime", http.MethodGet, "/views/" + viewTestID + "/events", "", kitsession.ErrClosed, http.StatusServiceUnavailable, "unavailable"},
		{"internal", http.MethodPost, "/views/" + viewTestID + "/reveal", `{"line":1}`, errors.New("boom"), http.StatusInternalServerError, "internal"},
	} {
		response := serveViewContract(t, viewWireTestService{err: test.err}, test.method, test.path, test.body)
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if response.Code != test.status || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Error.Code != test.code {
			t.Fatalf("%s: response = %d %s", test.name, response.Code, response.Body.String())
		}
	}
}

func TestRuntimeViewRevealSourceProjectsItsIdentityAndEndsWithTheWatch(t *testing.T) {
	reveals := make(chan kitsession.ViewReveal, 1)
	stopped := false
	source := runtimeViewRevealSource{sessionID: vcsTestSessionID, viewID: viewTestID, reveals: reveals, stop: func() { stopped = true }}
	reveals <- kitsession.ViewReveal{Line: 3}
	got, err := source.Next(t.Context())
	if err != nil || got != (protocol.ViewReveal{SessionID: vcsTestSessionID, ViewID: viewTestID, Line: 3}) {
		t.Fatalf("Next = %+v, %v", got, err)
	}
	close(reveals)
	if _, err := source.Next(t.Context()); !errors.Is(err, io.EOF) {
		t.Fatalf("Next after the watch ended = %v, want EOF", err)
	}
	source.Close()
	if !stopped {
		t.Fatal("closing the source left the watch open")
	}
}
