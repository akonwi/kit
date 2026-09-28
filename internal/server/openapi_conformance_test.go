package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
	request.Header.Set(protocolHeader, "41")
	if request.Method == http.MethodPut {
		request.Header.Set("Content-Type", "application/json")
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
