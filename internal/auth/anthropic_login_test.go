package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseAnthropicAuthorizationAcceptsManualForms(t *testing.T) {
	for name, input := range map[string]string{
		"code":      "authorization-code",
		"code hash": "authorization-code#expected-state",
		"query":     "code=authorization-code&state=expected-state",
		"redirect":  "http://localhost:53692/callback?code=authorization-code&state=expected-state",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := parseAnthropicAuthorization(input, "expected-state")
			if err != nil {
				t.Fatal(err)
			}
			if result.code != "authorization-code" || result.state != "expected-state" {
				t.Fatalf("authorization = %#v", result)
			}
		})
	}
}

func TestParseAnthropicAuthorizationRejectsStateMismatch(t *testing.T) {
	_, err := parseAnthropicAuthorization("code=value&state=wrong-state", "expected-state")
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("parse error = %v", err)
	}
}

func TestAnthropicCallbackValidatesState(t *testing.T) {
	results := make(chan authorizationResult, 1)
	handler := anthropicCallbackHandler("expected-state", results)
	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/callback?code=value&state=wrong", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid callback status = %d", invalid.Code)
	}
	select {
	case result := <-results:
		t.Fatalf("invalid callback published result %#v", result)
	default:
	}

	valid := httptest.NewRecorder()
	handler.ServeHTTP(valid, httptest.NewRequest(http.MethodGet, "/callback?code=value&state=expected-state", nil))
	if valid.Code != http.StatusOK {
		t.Fatalf("valid callback status = %d", valid.Code)
	}
	result := <-results
	if result.code != "value" || result.state != "expected-state" {
		t.Fatalf("callback result = %#v", result)
	}
}
