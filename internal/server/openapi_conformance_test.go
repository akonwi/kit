package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

func scratchpadConformanceMiddleware(t *testing.T, next http.Handler, report func(error)) http.Handler {
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
		if err := openapi3filter.ValidateResponse(context.Background(), &openapi3filter.ResponseValidationInput{
			RequestValidationInput: input, Status: recorder.Code, Header: recorder.Header(), Body: io.NopCloser(bytes.NewReader(body)), Options: options,
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
	}{
		{"get missing", http.MethodGet, "", kitsession.ErrNotFound, http.StatusNotFound},
		{"get unsupported", http.MethodGet, "", kitscratchpad.ErrUnsupported, http.StatusConflict},
		{"get unavailable", http.MethodGet, "", kitscratchpad.ErrUnavailable, http.StatusServiceUnavailable},
		{"get internal", http.MethodGet, "", errors.New("boom"), http.StatusInternalServerError},
		{"put generic invalid", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, errInvalidSessionRequest, http.StatusBadRequest},
		{"put invalid", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrInvalidContent, http.StatusBadRequest},
		{"put too large", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrContentTooLarge, http.StatusRequestEntityTooLarge},
		{"put conflict", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, conflict, http.StatusConflict},
		{"put exhausted", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrRevisionExhausted, http.StatusConflict},
		{"put migration", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrMigrationRequired, http.StatusConflict},
		{"put unsupported", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrUnsupported, http.StatusConflict},
		{"put unavailable", http.MethodPut, `{"expectedRevision":"1","content":"current"}`, kitscratchpad.ErrUnavailable, http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			registerSessionRoutes(mux, scratchpadWireTestService{record: record, err: test.err})
			var contractErr error
			handler := scratchpadConformanceMiddleware(t, mux, func(err error) { contractErr = err })
			request := httptest.NewRequest(test.method, "/v1/sessions/session_test/scratchpad", strings.NewReader(test.body))
			addContractRequestHeaders(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if contractErr != nil || response.Code != test.status {
				t.Fatalf("response = %d contract=%v body=%s", response.Code, contractErr, response.Body.String())
			}
		})
	}
}

func TestScratchpadConformanceRejectsDriftedResponse(t *testing.T) {
	var validationErr error
	handler := scratchpadConformanceMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"unexpected": true})
	}), func(err error) { validationErr = err })
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions/session_test/scratchpad", nil)
	addContractRequestHeaders(request)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if validationErr == nil {
		t.Fatal("conformance middleware accepted drifted response")
	}
}
