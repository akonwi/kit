package openaicodex

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserAuthorizationUsesPKCEAndExchangesOnce(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/oauth/token" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		tokenCalls.Add(1)
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.Form.Get("grant_type") != "authorization_code" || request.Form.Get("client_id") != clientID || request.Form.Get("code") != "auth-code" {
			t.Fatalf("token form = %#v", request.Form)
		}
		if request.Form.Get("redirect_uri") != DefaultRedirectURI || len(request.Form.Get("code_verifier")) < 43 {
			t.Fatalf("token form = %#v", request.Form)
		}
		writeToken(t, w, "account-1", "refresh-1", "", 3600)
	}))
	defer server.Close()

	client := testClient(t, server)
	client.rand = strings.NewReader(strings.Repeat("a", 128))
	authorization, err := client.BeginAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	authorizeURL, err := url.Parse(authorization.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := authorizeURL.Query()
	if authorizeURL.Path != "/oauth/authorize" || query.Get("client_id") != clientID || query.Get("originator") != "kit" {
		t.Fatalf("authorization URL = %s", authorization.URL)
	}
	if query.Get("state") == "" || query.Get("state") != authorization.State() || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization query = %#v", query)
	}
	verifier := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	challenge := sha256.Sum256([]byte(verifier))
	if query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(challenge[:]) {
		t.Fatalf("PKCE challenge = %q", query.Get("code_challenge"))
	}

	credentials, err := client.ExchangeAuthorizationCode(context.Background(), authorization, "auth-code", authorization.State())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccountID != "account-1" || credentials.RefreshToken != "refresh-1" || credentials.IDToken != "" {
		t.Fatalf("credentials = %#v", credentials)
	}
	if credentials.ExpiresAt.Sub(client.now()) != time.Hour {
		t.Fatalf("expires at = %s", credentials.ExpiresAt)
	}
	if _, err := client.ExchangeAuthorizationCode(context.Background(), authorization, "auth-code", authorization.State()); err == nil {
		t.Fatal("authorization was reused")
	}
	if tokenCalls.Load() != 1 {
		t.Fatalf("token calls = %d", tokenCalls.Load())
	}
}

func TestBrowserAuthorizationRequiresState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		writeToken(t, w, "account-1", "refresh", "", 3600)
	}))
	defer server.Close()
	client := testClient(t, server)
	authorization, err := client.BeginAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExchangeAuthorizationCode(context.Background(), authorization, "code", ""); err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("error = %v", err)
	}
	if _, err := client.ExchangeAuthorizationCode(context.Background(), authorization, "code", authorization.State()); err != nil {
		t.Fatalf("valid callback after unrelated state mismatch: %v", err)
	}
}

func TestDeviceAuthorizationPollsAndExchanges(t *testing.T) {
	var polls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			assertJSONField(t, request, "client_id", clientID)
			_, _ = io.WriteString(w, `{"device_auth_id":"device-1","user_code":"ABCD-EFGH","interval":"0"}`)
		case "/api/accounts/deviceauth/token":
			assertJSONField(t, request, "device_auth_id", "device-1")
			if polls.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"error":{"code":"deviceauth_authorization_pending","message":"pending"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"authorization_code":"device-code","code_verifier":"device-verifier"}`)
		case "/oauth/token":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("code") != "device-code" || request.Form.Get("code_verifier") != "device-verifier" || request.Form.Get("redirect_uri") != server.URL+"/deviceauth/callback" {
				t.Fatalf("exchange form = %#v", request.Form)
			}
			writeToken(t, w, "account-device", "refresh-device", "", 900)
		default:
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	device, err := client.BeginDeviceAuthorization(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if device.UserCode != "ABCD-EFGH" || device.VerificationURI != server.URL+"/codex/device" || device.Interval != minimumDevicePollInterval {
		t.Fatalf("device = %#v", device)
	}
	credentials, err := client.PollDeviceAuthorization(context.Background(), device)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccountID != "account-device" || credentials.RefreshToken != "refresh-device" || polls.Load() != 2 {
		t.Fatalf("credentials = %#v, polls=%d", credentials, polls.Load())
	}
	if _, err := client.PollDeviceAuthorization(context.Background(), device); err == nil {
		t.Fatal("device authorization was reused")
	}
}

func TestDeviceAuthorizationAcceptsAlternateCodeAndDefaultInterval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"device_auth_id":"device-1","usercode":"ALT-CODE"}`)
	}))
	defer server.Close()
	client := testClient(t, server)
	device, err := client.BeginDeviceAuthorization(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if device.UserCode != "ALT-CODE" || device.Interval != defaultDevicePollInterval {
		t.Fatalf("device = %#v", device)
	}
}

func TestDeviceAuthorizationRejectsUnsafeDisplayCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{\"device_auth_id\":\"device-1\",\"user_code\":\"CODE\\u001b[31m\",\"interval\":5}")
	}))
	defer server.Close()
	client := testClient(t, server)
	if _, err := client.BeginDeviceAuthorization(context.Background()); err == nil {
		t.Fatal("unsafe device code was accepted")
	}
}

func TestOAuthResponseErrorSanitizesTerminalControls(t *testing.T) {
	err := oauthResponseError("test", http.StatusBadRequest, []byte(`{"error":{"code":"bad\ncode","message":"unsafe\u001b[31m\u009b\u202emessage"}}`))
	if strings.ContainsAny(err.Code+err.Message, "\n\r\x1b\u009b\u202e") {
		t.Fatalf("error was not sanitized: %#v", err)
	}
}

func TestDevicePollingAcceptsBareForbiddenAsPending(t *testing.T) {
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = io.WriteString(w, `{"device_auth_id":"device-1","user_code":"CODE","interval":5}`)
		case "/api/accounts/deviceauth/token":
			if polls.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = io.WriteString(w, `{"authorization_code":"code","code_verifier":"verifier"}`)
		case "/oauth/token":
			writeToken(t, w, "account", "refresh", "", 3600)
		}
	}))
	defer server.Close()
	client := testClient(t, server)
	device, err := client.BeginDeviceAuthorization(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.PollDeviceAuthorization(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if polls.Load() != 2 {
		t.Fatalf("polls = %d", polls.Load())
	}
}

func TestDevicePollingRejectsUnclassifiedForbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = io.WriteString(w, `{"device_auth_id":"device-1","user_code":"CODE","interval":0}`)
		case "/api/accounts/deviceauth/token":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"code":"access_denied","message":"denied"}}`)
		}
	}))
	defer server.Close()
	client := testClient(t, server)
	device, err := client.BeginDeviceAuthorization(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.PollDeviceAuthorization(context.Background(), device)
	var protocolErr *Error
	if !errorsAs(err, &protocolErr) || protocolErr.Code != "access_denied" {
		t.Fatalf("error = %#v", err)
	}
}

func TestDevicePollingHonorsCancellationDuringSlowDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/accounts/deviceauth/usercode" {
			_, _ = io.WriteString(w, `{"device_auth_id":"device-1","user_code":"CODE","interval":0}`)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":"slow_down","message":"wait"}}`)
	}))
	defer server.Close()
	client := testClient(t, server)
	client.sleep = sleepContext
	device, err := client.BeginDeviceAuthorization(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err = client.PollDeviceAuthorization(ctx, device)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("error = %v, context = %v", err, ctx.Err())
	}
}

func TestRefreshPreservesRotatedFieldsAndAccount(t *testing.T) {
	t.Run("preserves omitted refresh fields", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("grant_type") != "refresh_token" || request.Form.Get("refresh_token") != "old-refresh" {
				t.Fatalf("form = %#v", request.Form)
			}
			writeToken(t, w, "account-1", "", "", 3600)
		}))
		defer server.Close()
		client := testClient(t, server)
		got, err := client.Refresh(context.Background(), Credentials{
			RefreshToken: "old-refresh", IDToken: "old-id", AccountID: "account-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.RefreshToken != "old-refresh" || got.IDToken != "old-id" || got.AccountID != "account-1" {
			t.Fatalf("credentials = %#v", got)
		}
	})

	t.Run("accepts refresh-only rotation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"refresh_token": "rotated-refresh"})
		}))
		defer server.Close()
		client := testClient(t, server)
		previous := Credentials{
			AccessToken: testAccessToken(t, "account-1"), RefreshToken: "old-refresh",
			AccountID: "account-1", ExpiresAt: client.now().Add(30 * time.Minute),
		}
		got, err := client.Refresh(context.Background(), previous)
		if err != nil {
			t.Fatal(err)
		}
		if got.AccessToken != previous.AccessToken || got.RefreshToken != "rotated-refresh" || !got.ExpiresAt.Equal(previous.ExpiresAt) {
			t.Fatalf("credentials = %#v", got)
		}
	})

	t.Run("carries FedRAMP routing", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fedRAMP := true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  testAccessTokenClaims(t, "account-1", &fedRAMP, nil),
				"refresh_token": "new-refresh", "expires_in": 3600,
			})
		}))
		defer server.Close()
		client := testClient(t, server)
		got, err := client.Refresh(context.Background(), Credentials{RefreshToken: "old", AccountID: "account-1"})
		if err != nil {
			t.Fatal(err)
		}
		if !got.FedRAMP {
			t.Fatalf("credentials = %#v", got)
		}
	})

	t.Run("rejects mismatched returned id token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeToken(t, w, "account-1", "new-refresh", testAccessToken(t, "account-2"), 3600)
		}))
		defer server.Close()
		client := testClient(t, server)
		_, err := client.Refresh(context.Background(), Credentials{RefreshToken: "old", AccountID: "account-1"})
		var protocolErr *Error
		if !errorsAs(err, &protocolErr) || protocolErr.Code != "account_changed" {
			t.Fatalf("error = %#v", err)
		}
	})

	t.Run("rejects opaque replacement access token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "opaque-replacement", "refresh_token": "new", "expires_in": 3600,
			})
		}))
		defer server.Close()
		client := testClient(t, server)
		_, err := client.Refresh(context.Background(), Credentials{RefreshToken: "old", AccountID: "account-1"})
		if err == nil || !strings.Contains(err.Error(), "access token") {
			t.Fatalf("error = %#v", err)
		}
	})

	t.Run("rejects account change", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeToken(t, w, "account-2", "new-refresh", "", 3600)
		}))
		defer server.Close()
		client := testClient(t, server)
		_, err := client.Refresh(context.Background(), Credentials{RefreshToken: "old", AccountID: "account-1"})
		var protocolErr *Error
		if !errorsAs(err, &protocolErr) || protocolErr.Code != "account_changed" {
			t.Fatalf("error = %#v", err)
		}
	})
}

func TestDeviceIntervalValidation(t *testing.T) {
	for _, raw := range []string{`"NaN"`, `"Inf"`, `-1`, `1e30`, `{}`} {
		if _, err := parseInterval(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted interval %s", raw)
		}
	}
	if got, err := parseInterval(json.RawMessage(`0`)); err != nil || clampDevicePollInterval(got) != minimumDevicePollInterval {
		t.Fatalf("zero interval = %s, %v", got, err)
	}
	if got, err := parseInterval(json.RawMessage(`9999`)); err != nil || clampDevicePollInterval(got) != 9999*time.Second {
		t.Fatalf("large interval = %s, %v", got, err)
	}
}

func TestAccountIDRejectsMalformedTokens(t *testing.T) {
	fedRAMP := true
	token := testAccessTokenClaims(t, "account-1", &fedRAMP, nil)
	metadata, err := ParseAccountMetadata(token)
	if err != nil || metadata.ID != "account-1" || !metadata.HasFedRAMP || !metadata.FedRAMP {
		t.Fatalf("metadata = %#v, %v", metadata, err)
	}
	if id, err := AccountID(token); err != nil || id != "account-1" {
		t.Fatalf("account id = %q, %v", id, err)
	}
	for _, token := range []string{"", "not-a-jwt", "a.%%%25.c", testAccessToken(t, "")} {
		if _, err := AccountID(token); err == nil {
			t.Fatalf("accepted token %q", token)
		}
	}
}

func TestTokenExpiryMustBeCurrentAndBounded(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	expired := now.Add(-time.Second).Unix()
	if _, _, err := tokenExpiry(testAccessTokenClaims(t, "account", nil, &expired), now); err == nil {
		t.Fatal("expired JWT was accepted")
	}
	far := now.Add(maximumTokenLifetime + time.Second).Unix()
	if _, _, err := tokenExpiry(testAccessTokenClaims(t, "account", nil, &far), now); err == nil {
		t.Fatal("unreasonably distant JWT expiry was accepted")
	}
	valid := now.Add(time.Hour).Unix()
	if expiry, ok, err := tokenExpiry(testAccessTokenClaims(t, "account", nil, &valid), now); err != nil || !ok || expiry.Unix() != valid {
		t.Fatalf("valid expiry = %s, %v, %v", expiry, ok, err)
	}
}

func TestOAuthDoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Bool
	attacker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		leaked.Store(true)
	}))
	defer attacker.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, attacker.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()
	client := testClient(t, redirector)
	_, err := client.Refresh(context.Background(), Credentials{RefreshToken: "secret"})
	if err == nil {
		t.Fatal("redirect unexpectedly succeeded")
	}
	if leaked.Load() {
		t.Fatal("refresh credential followed a redirect")
	}
}

func testClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(server.Client(), "kit")
	if err != nil {
		t.Fatal(err)
	}
	client.authBaseURL = server.URL
	fixed := time.Unix(1_800_000_000, 0)
	client.now = func() time.Time { return fixed }
	client.sleep = func(context.Context, time.Duration) error { return nil }
	return client
}

func writeToken(t *testing.T, w http.ResponseWriter, accountID, refreshToken, idToken string, expiresIn int64) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"access_token":  testAccessToken(t, accountID),
		"refresh_token": refreshToken,
		"id_token":      idToken,
		"expires_in":    expiresIn,
	}); err != nil {
		t.Fatal(err)
	}
}

func testAccessToken(t *testing.T, accountID string) string {
	t.Helper()
	return testAccessTokenClaims(t, accountID, nil, nil)
}

func testAccessTokenClaims(t *testing.T, accountID string, fedRAMP *bool, expiresAt *int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	auth := map[string]any{"chatgpt_account_id": accountID}
	if fedRAMP != nil {
		auth["chatgpt_account_is_fedramp"] = *fedRAMP
	}
	claims := map[string]any{"https://api.openai.com/auth": auth}
	if expiresAt != nil {
		claims["exp"] = *expiresAt
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func assertJSONField(t *testing.T, request *http.Request, key, want string) {
	t.Helper()
	defer request.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body[key] != want {
		t.Fatalf("%s = %q, want %q", key, body[key], want)
	}
}

// Keep this helper local so tests remain readable without coupling assertions
// to the exact concrete error wrapping implementation.
func errorsAs(err error, target any) bool {
	return errors.As(err, target)
}
