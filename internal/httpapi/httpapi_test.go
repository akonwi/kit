package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
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

func TestCatalogHasUniqueOperationsAndRoutes(t *testing.T) {
	ids, routes := map[string]bool{}, map[string]bool{}
	for _, operation := range Catalog() {
		route := operation.Method + " " + operation.Path
		if operation.ID == "" || ids[operation.ID] {
			t.Fatalf("duplicate or empty operation id %q", operation.ID)
		}
		if routes[route] {
			t.Fatalf("duplicate catalog route %q", route)
		}
		ids[operation.ID], routes[route] = true, true
	}
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
	var apiErr *APIError
	if !errors.As(gotErr, &apiErr) || apiErr.Code != string(ErrorInvalidRequest) || response.Code != http.StatusTeapot {
		t.Fatalf("strict decode = %v/%d", gotErr, response.Code)
	}
}

func TestDecodeOperationErrorUsesDeclaredStatusCodesAndTypedDetails(t *testing.T) {
	body := []byte(`{"error":{"code":"scratchpad_revision_conflict","message":"scratchpad revision conflict","details":{"scratchpad":{"ownerSessionId":"session_0123456789abcdef0123456789abcdef","content":"shared","revision":"2","updatedAt":"2026-03-23T12:34:56Z"}}}}`)
	err := DecodeOperationError(UpdateScratchpad, http.StatusConflict, body)
	var apiErr *APIError
	var scratchErr *protocol.ScratchpadError
	if !errors.As(err, &apiErr) || !errors.As(err, &scratchErr) || apiErr.CurrentScratchpad == nil || apiErr.CurrentScratchpad.Content != "shared" {
		t.Fatalf("typed conflict = %#v", err)
	}
	if details, ok := apiErr.TypedDetails.(protocol.ScratchpadErrorDetails); !ok || details.Scratchpad == nil {
		t.Fatalf("typed details = %#v", apiErr.TypedDetails)
	}
	for _, body := range []string{
		`{"error":{"code":"conflict","message":"wrong code"}}`,
		`{"error":{"code":"scratchpad_revision_exhausted","message":"wrong details","details":{}}}`,
	} {
		if err := DecodeOperationError(UpdateScratchpad, http.StatusConflict, []byte(body)); errors.As(err, &apiErr) {
			t.Fatalf("accepted drifted operation error: %#v", err)
		}
	}
	if err := DecodeOperationError(UpdateScratchpad, http.StatusUpgradeRequired, []byte(`{"error":{"code":"protocol_mismatch","message":"session protocol mismatch"}}`)); !errors.As(err, &apiErr) || !apiErr.IncompatibleDaemon() {
		t.Fatalf("protocol mismatch = %#v", err)
	}
}

func TestDecodeOperationErrorRejectsMalformedScratchpadErrors(t *testing.T) {
	record := `{"ownerSessionId":"session_0123456789abcdef0123456789abcdef","content":"shared","revision":"2","updatedAt":"2026-03-23T12:34:56Z"`
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"missing conflict record", http.StatusConflict, `{"error":{"code":"scratchpad_revision_conflict","message":"scratchpad revision conflict","details":{}}}`},
		{"omitted conflict details", http.StatusConflict, `{"error":{"code":"scratchpad_revision_conflict","message":"scratchpad revision conflict"}}`},
		{"undeclared status for code", http.StatusBadRequest, `{"error":{"code":"scratchpad_revision_conflict","message":"scratchpad revision conflict","details":{"scratchpad":` + record + `}}}}`},
		{"unknown record field", http.StatusConflict, `{"error":{"code":"scratchpad_revision_conflict","message":"scratchpad revision conflict","details":{"scratchpad":` + record + `,"unexpected":true}}}}`},
		{"details on code without details", http.StatusServiceUnavailable, `{"error":{"code":"scratchpad_unavailable","message":"scratchpad unavailable","details":{"unexpected":"value"}}}`},
		{"null details", http.StatusServiceUnavailable, `{"error":{"code":"scratchpad_unavailable","message":"scratchpad unavailable","details":null}}`},
		{"legacy string body", http.StatusServiceUnavailable, `{"error":"scratchpad unavailable"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := DecodeOperationError(UpdateScratchpad, test.status, []byte(test.body))
			var apiError *APIError
			if err == nil || errors.As(err, &apiError) {
				t.Fatalf("malformed error decoded as %#v", err)
			}
		})
	}
}

func TestLegacyDecoderAcceptsCommonPreRoutingErrors(t *testing.T) {
	for _, test := range []struct {
		status int
		code   ErrorCode
	}{
		{http.StatusMisdirectedRequest, ErrorInvalidHost},
		{http.StatusForbidden, ErrorForbidden},
		{http.StatusUnauthorized, ErrorUnauthorized},
		{http.StatusConflict, ErrorInstanceMismatch},
		{http.StatusUpgradeRequired, ErrorProtocolMismatch},
	} {
		err := DecodeError(test.status, []byte(fmt.Sprintf(`{"error":{"code":%q,"message":"rejected"}}`, test.code)))
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != string(test.code) || apiErr.StatusCode != test.status {
			t.Fatalf("DecodeError(%s) = %#v", test.code, err)
		}
	}
}

func TestCallEscapesPathStrictlyDecodesAndPassesErrors(t *testing.T) {
	op := Operation[SessionPath, testInput, testOutput]{ID: "test", Method: http.MethodPut, Path: "/items/{sessionID}", Success: http.StatusOK,
		Errors: []ErrorResponse{{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}}}}
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
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"not_found","message":"missing"}}`))}, nil
	}
	_, err = Call(t.Context(), transport, op, SessionPath{}, testInput{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || apiErr.Code != string(ErrorNotFound) || apiErr.Message != "missing" {
		t.Fatalf("error = %#v", err)
	}

	transport = func(context.Context, string, string, io.Reader, bool) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"value":"ok","extra":1}`))}, nil
	}
	if _, err := Call(t.Context(), transport, op, SessionPath{}, testInput{}); err == nil {
		t.Fatal("Call accepted unknown response field")
	}
}

func TestCallBoundsRequestAndResponseBodies(t *testing.T) {
	op := Operation[SessionPath, testInput, testOutput]{ID: "test", Method: http.MethodPut, Path: "/items/{sessionID}", Success: http.StatusOK,
		Errors: []ErrorResponse{{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}}}}
	respond := func(status int, body string) testTransport {
		return func(context.Context, string, string, io.Reader, bool) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
	}
	valid := `{"value":"ok"}`
	atLimit := valid + strings.Repeat(" ", maxResponseBytes-len(valid))
	if output, err := Call(t.Context(), respond(http.StatusOK, atLimit), op, SessionPath{SessionID: "a"}, testInput{Name: "kit"}); err != nil || output.Value != "ok" {
		t.Fatalf("response at limit = %+v, %v", output, err)
	}
	if _, err := Call(t.Context(), respond(http.StatusOK, atLimit+" "), op, SessionPath{SessionID: "a"}, testInput{Name: "kit"}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("response beyond limit error = %v", err)
	}
	notFound := `{"error":{"code":"not_found","message":"missing"}}`
	oversizedError := notFound + strings.Repeat(" ", maxResponseBytes-len(notFound)+1)
	if _, err := Call(t.Context(), respond(http.StatusNotFound, oversizedError), op, SessionPath{SessionID: "a"}, testInput{Name: "kit"}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized error body error = %v", err)
	}

	called := false
	transport := testTransport(func(context.Context, string, string, io.Reader, bool) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected request")
	})
	_, err := Call(t.Context(), transport, op, SessionPath{SessionID: "a"}, testInput{Name: strings.Repeat("x", MaxRequestBytes)})
	if err == nil || !strings.Contains(err.Error(), "exceeds") || called {
		t.Fatalf("oversized request error = %v, transport called = %t", err, called)
	}
}
