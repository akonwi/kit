package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	kitscratchpad "github.com/akonwi/kit/internal/scratchpad"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/version"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"
)

func contractConformanceMiddleware(t *testing.T, next http.Handler, report func(error)) http.Handler {
	t.Helper()
	document, err := openapi3.NewLoader().LoadFromFile("../../api/kit-session.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	router, err := legacyrouter.NewRouter(document)
	if err != nil {
		t.Fatal(err)
	}
	options := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, IncludeResponseStatus: true, RejectWhenRequestBodyNotSpecified: false}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, params, err := router.FindRoute(r)
		if err != nil {
			report(err)
			http.Error(w, "contract route", http.StatusInternalServerError)
			return
		}
		input := &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route, Options: options}
		if err := openapi3filter.ValidateRequest(context.Background(), input); err != nil {
			report(fmt.Errorf("request: %w", err))
			http.Error(w, "contract request", http.StatusInternalServerError)
			return
		}
		recorder := httptest.NewRecorder()
		next.ServeHTTP(recorder, r)
		body := append([]byte(nil), recorder.Body.Bytes()...)
		responseOptions := options
		if mediaType, _, _ := mime.ParseMediaType(recorder.Header().Get("Content-Type")); mediaType == "text/event-stream" {
			// kin-openapi cannot decode event streams; records are checked
			// against the published payload schema by validateStreamRecords.
			copied := *options
			copied.ExcludeResponseBody = true
			responseOptions = &copied
		}
		if err := openapi3filter.ValidateResponse(context.Background(), &openapi3filter.ResponseValidationInput{
			RequestValidationInput: input, Status: recorder.Code, Header: recorder.Header(), Body: io.NopCloser(bytes.NewReader(body)), Options: responseOptions,
		}); err != nil {
			report(fmt.Errorf("response: %w", err))
			http.Error(w, "contract response", http.StatusInternalServerError)
			return
		}
		for key, values := range recorder.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(body)
	})
}

func addContractRequestHeaders(request *http.Request) {
	request.Header.Set("Authorization", "Bearer test")
	request.Header.Set(instanceHeader, "instance_test")
	request.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion))
	if request.Method == http.MethodPut {
		request.Header.Set("Content-Type", "application/json")
	}
}

func TestScratchpadErrorResponsesConformToContract(t *testing.T) {
	record := protocol.Scratchpad{OwnerSessionID: "session_0123456789abcdef0123456789abcdef", Content: "current", Revision: 2, UpdatedAt: "2026-03-23T12:34:56Z"}
	conflict := &kitscratchpad.ConflictError{Expected: 1, Current: kitscratchpad.Record{OwnerSessionID: record.OwnerSessionID, Content: record.Content, Revision: 2, UpdatedAt: time.Date(2026, 3, 23, 12, 34, 56, 0, time.UTC)}}
	tests := []struct {
		name, method, body string
		err                error
		status             int
		code               string
	}{
		{"get invalid", http.MethodGet, "", kitsession.ErrInvalidInput, http.StatusBadRequest, "invalid_request"},
		{"get missing", http.MethodGet, "", kitsession.ErrNotFound, http.StatusNotFound, "not_found"},
		{"get migration", http.MethodGet, "", kitscratchpad.ErrMigrationRequired, http.StatusConflict, "scratchpad_migration_required"},
		{"get unsupported", http.MethodGet, "", kitscratchpad.ErrUnsupported, http.StatusConflict, "scratchpad_unsupported"},
		{"get unavailable", http.MethodGet, "", kitscratchpad.ErrUnavailable, http.StatusServiceUnavailable, "scratchpad_unavailable"},
		{"get internal", http.MethodGet, "", errors.New("boom"), http.StatusInternalServerError, "internal"},
		{"put generic invalid", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, errInvalidSessionRequest, http.StatusBadRequest, "invalid_request"},
		{"put invalid", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrInvalidContent, http.StatusBadRequest, "scratchpad_invalid_content"},
		{"put missing", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitsession.ErrNotFound, http.StatusNotFound, "not_found"},
		{"put too large", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrContentTooLarge, http.StatusRequestEntityTooLarge, "scratchpad_too_large"},
		{"put request limit", http.MethodPut, `{"expectedRevision":"1","content":"` + strings.Repeat("x", maxSessionRequestBytes) + `"}`, nil, http.StatusRequestEntityTooLarge, "limit_exceeded"},
		{"put conflict", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, conflict, http.StatusConflict, "scratchpad_revision_conflict"},
		{"put exhausted", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrRevisionExhausted, http.StatusConflict, "scratchpad_revision_exhausted"},
		{"put migration", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrMigrationRequired, http.StatusConflict, "scratchpad_migration_required"},
		{"put unsupported", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrUnsupported, http.StatusConflict, "scratchpad_unsupported"},
		{"put unavailable", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrUnavailable, http.StatusServiceUnavailable, "scratchpad_unavailable"},
		{"put internal", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, errors.New("boom"), http.StatusInternalServerError, "internal"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			registerSessionRoutes(mux, scratchpadWireTestService{record: record, err: test.err})
			var contractErr error
			handler := contractConformanceMiddleware(t, mux, func(err error) { contractErr = err })
			request := httptest.NewRequest(test.method, "/v1/sessions/session_test/scratchpad", strings.NewReader(test.body))
			addContractRequestHeaders(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if contractErr != nil || response.Code != test.status {
				t.Fatalf("response = %d contract=%v body=%s", response.Code, contractErr, response.Body.String())
			}
			var envelope struct {
				Error struct {
					Code    string          `json:"code"`
					Message string          `json:"message"`
					Details json.RawMessage `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != test.code || envelope.Error.Message == "" {
				t.Fatalf("error body = %s, want code %s", response.Body.String(), test.code)
			}
			if (test.code == "scratchpad_revision_conflict") != (len(envelope.Error.Details) > 0) {
				t.Fatalf("details presence for %s = %s", test.code, envelope.Error.Details)
			}
			if test.status == http.StatusInternalServerError && envelope.Error.Message != "internal server error" {
				t.Fatalf("internal message = %q", envelope.Error.Message)
			}
		})
	}
}

func TestPreRoutingErrorResponsesConformToContract(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   string
		mutate func(*http.Request)
	}{
		{"host", http.StatusMisdirectedRequest, "invalid_host", func(r *http.Request) { r.Host = "wrong.example" }},
		{"origin", http.StatusForbidden, "forbidden", func(r *http.Request) { r.Header.Set("Origin", "http://wrong.example") }},
		{"bearer", http.StatusUnauthorized, "unauthorized", func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }},
		{"instance", http.StatusConflict, "instance_mismatch", func(r *http.Request) { r.Header.Set(instanceHeader, "wrong") }},
		{"protocol", http.StatusUpgradeRequired, "protocol_mismatch", func(r *http.Request) { r.Header.Set(protocolHeader, "+43") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next := newHandler(localHandlerOptions{
				baseURL: "http://example.com", expectedHost: "example.com", token: "test",
				registry: Registry{InstanceID: "instance_test"}, sessions: scratchpadWireTestService{},
			})
			var contractErr error
			handler := contractConformanceMiddleware(t, next, func(err error) { contractErr = err })
			request := httptest.NewRequest(http.MethodGet, "http://example.com/v1/sessions/session_test/scratchpad", nil)
			addContractRequestHeaders(request)
			test.mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if contractErr != nil || response.Code != test.status {
				t.Fatalf("response = %d contract=%v body=%s", response.Code, contractErr, response.Body.String())
			}
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Error.Code != test.code {
				t.Fatalf("body = %s, want code %s", response.Body.String(), test.code)
			}
			if test.status == http.StatusUnauthorized && response.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q", response.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestScratchpadConformanceRejectsDriftedErrorBody(t *testing.T) {
	var validationErr error
	handler := contractConformanceMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{
			"code": "scratchpad_revision_exhausted", "message": "scratchpad revision is exhausted", "details": map[string]any{},
		}})
	}), func(err error) { validationErr = err })
	request := httptest.NewRequest(http.MethodPut, "/v1/sessions/session_test/scratchpad", strings.NewReader(`{"expectedRevision":"1","content":"current"}`))
	addContractRequestHeaders(request)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if validationErr == nil {
		t.Fatal("conformance middleware accepted drifted error response")
	}
}
