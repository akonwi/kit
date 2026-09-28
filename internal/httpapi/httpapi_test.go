package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testInput struct {
	Name string `json:"name"`
}
type testOutput struct {
	Value string `json:"value"`
}

type testTransport func(context.Context, string, string, io.Reader, bool) (*http.Response, error)

func (fn testTransport) DoSessionRequest(ctx context.Context, method, path string, body io.Reader, jsonBody bool) (*http.Response, error) {
	return fn(ctx, method, path, body, jsonBody)
}

func TestHandleBindsPathAndStrictlyDecodes(t *testing.T) {
	op := Operation[SessionPath, testInput, testOutput]{ID: "test", Method: http.MethodPut, Path: "/items/{sessionID}", Success: http.StatusCreated}
	mux := http.NewServeMux()
	sentinel := errors.New("passthrough")
	var gotErr error
	Handle(mux, ServeOptions{MaxRequestBytes: 1024, WriteError: func(w http.ResponseWriter, err error) { gotErr = err; http.Error(w, "error", http.StatusTeapot) }}, op,
		func(_ context.Context, params SessionPath, input testInput) (testOutput, error) {
			if params.SessionID != "a/b" || input.Name != "kit" {
				t.Fatalf("params/input = %+v %+v", params, input)
			}
			return testOutput{}, sentinel
		})
	request := httptest.NewRequest(http.MethodPut, "/items/a%2Fb", strings.NewReader(`{"name":"kit"}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if !errors.Is(gotErr, sentinel) || response.Code != http.StatusTeapot {
		t.Fatalf("error/status = %v/%d", gotErr, response.Code)
	}

	request = httptest.NewRequest(http.MethodPut, "/items/id", strings.NewReader(`{"name":"kit","extra":true}`))
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	var requestErr *RequestError
	if !errors.As(gotErr, &requestErr) || response.Code != http.StatusTeapot {
		t.Fatalf("strict decode = %v/%d", gotErr, response.Code)
	}
}

func TestCallEscapesPathStrictlyDecodesAndPassesErrors(t *testing.T) {
	op := Operation[SessionPath, testInput, testOutput]{ID: "test", Method: http.MethodPut, Path: "/items/{sessionID}", Success: http.StatusOK}
	transport := testTransport(func(_ context.Context, method, path string, body io.Reader, jsonBody bool) (*http.Response, error) {
		encoded, _ := io.ReadAll(body)
		if method != http.MethodPut || path != "/items/a%2Fb" || !jsonBody || string(encoded) != `{"name":"kit"}` {
			t.Fatalf("request = %s %s %t %s", method, path, jsonBody, encoded)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"value":"ok"}`))}, nil
	})
	output, err := Call(t.Context(), transport, op, SessionPath{SessionID: "a/b"}, testInput{Name: "kit"})
	if err != nil || output.Value != "ok" {
		t.Fatalf("Call = %+v, %v", output, err)
	}

	transport = func(context.Context, string, string, io.Reader, bool) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"error":"missing"}`))}, nil
	}
	_, err = Call(t.Context(), transport, op, SessionPath{}, testInput{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || apiErr.Message != "missing" {
		t.Fatalf("error = %#v", err)
	}

	transport = func(context.Context, string, string, io.Reader, bool) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"value":"ok","extra":1}`))}, nil
	}
	if _, err := Call(t.Context(), transport, op, SessionPath{}, testInput{}); err == nil {
		t.Fatal("Call accepted unknown response field")
	}
}
