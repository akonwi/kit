package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

// listRouteTestService answers listSessions from fixed sessions and records
// which list was asked for.
type listRouteTestService struct {
	sessionService
	calls *[]string
}

func (service listRouteTestService) List(_ context.Context, cwd string) ([]protocol.SessionInfo, error) {
	*service.calls = append(*service.calls, "List "+cwd)
	return []protocol.SessionInfo{
		{ID: "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CWD: "/work", Model: "test/echo", ThinkingLevel: "medium", ConfigurationRevision: 1, CreatedAt: "2026-03-23T12:00:00Z", UpdatedAt: "2026-03-23T12:30:00Z"},
	}, nil
}

func (service listRouteTestService) ListByAttention(_ context.Context, cwd string) ([]protocol.SessionInfo, error) {
	*service.calls = append(*service.calls, "ListByAttention "+cwd)
	return []protocol.SessionInfo{
		{ID: "session_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CWD: "/work", Model: "test/echo", ThinkingLevel: "medium", ConfigurationRevision: 1, CreatedAt: "2026-03-23T12:00:00Z", UpdatedAt: "2026-03-23T12:20:00Z",
			Activity: &protocol.SessionInfoActivity{Status: protocol.SessionActivityAwaitingResponse, Since: "2026-03-23T12:10:00Z"}},
		{ID: "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CWD: "/work", Model: "test/echo", ThinkingLevel: "medium", ConfigurationRevision: 1, CreatedAt: "2026-03-23T12:00:00Z", UpdatedAt: "2026-03-23T12:30:00Z"},
	}, nil
}

func serveListRoute(t *testing.T, target string) (*httptest.ResponseRecorder, []string) {
	t.Helper()
	var calls []string
	mux := http.NewServeMux()
	registerSessionRoutes(mux, listRouteTestService{calls: &calls})
	var contractErr error
	handler := contractConformanceMiddleware(t, mux, func(err error) { contractErr = err })
	request := httptest.NewRequest(http.MethodGet, target, nil)
	addContractRequestHeaders(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if contractErr != nil {
		t.Fatalf("contract validation: %v", contractErr)
	}
	return response, calls
}

// Released clients decode the list strictly, so a list asked for without an
// order is byte for byte what it was before orders existed.
func TestListSessionsWithoutAnOrderIsUnchanged(t *testing.T) {
	t.Parallel()
	response, calls := serveListRoute(t, "/v1/sessions?cwd=%2Fwork")
	if response.Code != http.StatusOK {
		t.Fatalf("GET sessions = %d %q", response.Code, response.Body.String())
	}
	want := `{"sessions":[{"id":"session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","cwd":"/work","model":"test/echo","thinkingLevel":"medium","configurationRevision":1,"createdAt":"2026-03-23T12:00:00Z","updatedAt":"2026-03-23T12:30:00Z"}]}`
	if got := strings.TrimSpace(response.Body.String()); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
	if len(calls) != 1 || calls[0] != "List /work" {
		t.Fatalf("calls = %q, want the plain list", calls)
	}
}

func TestListSessionsInAttentionOrderSaysSo(t *testing.T) {
	t.Parallel()
	response, calls := serveListRoute(t, "/v1/sessions?order=attention")
	if response.Code != http.StatusOK {
		t.Fatalf("GET sessions = %d %q", response.Code, response.Body.String())
	}
	want := `{"sessions":[` +
		`{"id":"session_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","cwd":"/work","model":"test/echo","thinkingLevel":"medium","configurationRevision":1,"createdAt":"2026-03-23T12:00:00Z","updatedAt":"2026-03-23T12:20:00Z","activity":{"status":"awaitingResponse","since":"2026-03-23T12:10:00Z"}},` +
		`{"id":"session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","cwd":"/work","model":"test/echo","thinkingLevel":"medium","configurationRevision":1,"createdAt":"2026-03-23T12:00:00Z","updatedAt":"2026-03-23T12:30:00Z"}` +
		`],"order":"attention"}`
	if got := strings.TrimSpace(response.Body.String()); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
	if len(calls) != 1 || calls[0] != "ListByAttention " {
		t.Fatalf("calls = %q, want the attention list", calls)
	}
}

// The contract already refuses an unknown order, so this request goes to the
// routes without the conformance check.
func TestListSessionsRejectsAnUnknownOrder(t *testing.T) {
	t.Parallel()
	var calls []string
	mux := http.NewServeMux()
	registerSessionRoutes(mux, listRouteTestService{calls: &calls})
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions?order=newest", nil)
	addContractRequestHeaders(request)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"invalid_request"`) {
		t.Fatalf("GET sessions?order=newest = %d %q, want 400 invalid_request", response.Code, response.Body.String())
	}
	if len(calls) != 0 {
		t.Fatalf("calls = %q, want none", calls)
	}
}
