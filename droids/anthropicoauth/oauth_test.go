package anthropicoauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExchangeUsesFixedProtocolPayload(t *testing.T) {
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request = %s with content type %q", r.Method, r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
	}))
	defer server.Close()
	client := NewClient(server.Client())
	client.tokenURL = server.URL

	before := time.Now().Add(54 * time.Minute)
	credentials, err := client.Exchange(context.Background(), "code", "state", "verifier", "http://localhost/callback")
	if err != nil {
		t.Fatal(err)
	}
	if payload["grant_type"] != "authorization_code" || payload["client_id"] != ClientID || payload["code_verifier"] != "verifier" || payload["state"] != "state" {
		t.Fatalf("exchange payload = %#v", payload)
	}
	if credentials.AccessToken != "access" || credentials.RefreshToken != "refresh" || credentials.ExpiresAt.Before(before) {
		t.Fatalf("credentials = %#v", credentials)
	}
}

func TestRefreshDoesNotExposeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "secret-token-body", http.StatusUnauthorized)
	}))
	defer server.Close()
	client := NewClient(server.Client())
	client.tokenURL = server.URL

	_, err := client.Refresh(context.Background(), Credentials{RefreshToken: "secret-refresh"})
	var tokenError *TokenError
	if !errors.As(err, &tokenError) || tokenError.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh error = %v", err)
	}
	if err.Error() != "Anthropic OAuth request failed with HTTP 401" {
		t.Fatalf("refresh error text = %q", err)
	}
}
