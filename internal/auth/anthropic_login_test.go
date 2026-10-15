package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

// The authorization URL and redirect carry only the state and the
// verifier's challenge; the verifier itself goes only to the token exchange.
func TestAnthropicLoginKeepsTheVerifierOutOfTheAuthorizationURL(t *testing.T) {
	var exchange map[string]string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&exchange); err != nil {
			t.Error(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(
			`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))}, nil
	})}
	store := NewStore(filepath.Join(t.TempDir(), "auth.json"))
	login, err := NewAnthropicOAuthLogin(AnthropicOAuthLoginOptions{Store: store, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	authorization := make(chan string, 1)
	var query url.Values
	err = login.Login(context.Background(), authorization, func(instructions AnthropicLoginInstructions) error {
		parsed, err := url.Parse(instructions.AuthorizationURL)
		if err != nil {
			return err
		}
		query = parsed.Query()
		authorization <- instructions.RedirectURI + "?code=the-code&state=" + url.QueryEscape(query.Get("state"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier := exchange["code_verifier"]
	digest := sha256.Sum256([]byte(verifier))
	if verifier == "" || query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(digest[:]) {
		t.Fatalf("exchange verifier %q does not match the URL's challenge %q", verifier, query.Get("code_challenge"))
	}
	if state := query.Get("state"); len(state) != 43 || state == verifier || strings.Contains(query.Encode(), verifier) {
		t.Fatalf("authorization URL state %q must be 256 random bits apart from the verifier", state)
	}
	if exchange["code"] != "the-code" || exchange["state"] != query.Get("state") {
		t.Fatalf("exchange = %v", exchange)
	}
	record, err := store.LoadAnthropicCredentials(context.Background())
	if err != nil || record.Credentials.AccessToken != "access" {
		t.Fatalf("stored credentials = %+v, %v", record, err)
	}
}

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

func TestParseAnthropicAuthorizationRejectsADeniedRedirect(t *testing.T) {
	_, err := parseAnthropicAuthorization("http://localhost:53692/callback?error=access_denied&state=expected-state", "expected-state")
	if err == nil || err.Error() != "Anthropic authorization was not granted (access_denied)" {
		t.Fatalf("parse error = %v", err)
	}
}
