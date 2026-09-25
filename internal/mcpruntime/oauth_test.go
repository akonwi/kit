//go:build darwin || linux

package mcpruntime

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	kitauth "github.com/akonwi/kit/internal/auth"
	droidsmcp "github.com/akonwi/kit/internal/droids/mcp"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

type memoryMCPAuthStore struct {
	mu      sync.Mutex
	records map[string]kitauth.MCPOAuthRecord
	deletes int
	saves   int
}

func (s *memoryMCPAuthStore) LoadMCPOAuth(_ context.Context, key string) (kitauth.MCPOAuthRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.records[key], nil
}

func (s *memoryMCPAuthStore) SaveMCPOAuth(_ context.Context, key, expected string, session kitauth.MCPOAuthSession) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.records[key]
	if current.Revision != expected {
		return "", kitauth.ErrCredentialsChanged
	}
	s.saves++
	revision := "next-revision"
	if s.records == nil {
		s.records = map[string]kitauth.MCPOAuthRecord{}
	}
	s.records[key] = kitauth.MCPOAuthRecord{Session: session, Revision: revision, Found: true}
	return revision, nil
}

func (s *memoryMCPAuthStore) DeleteMCPOAuth(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes++
	s.records[key] = kitauth.MCPOAuthRecord{Revision: "logout"}
	return nil
}

func (s *memoryMCPAuthStore) DeleteMCPOAuthRevision(_ context.Context, key, expected string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records[key].Revision != expected {
		return "", kitauth.ErrCredentialsChanged
	}
	s.deletes++
	s.records[key] = kitauth.MCPOAuthRecord{Revision: "rejected"}
	return "rejected", nil
}

func TestOAuthCallbackOpensBrowserAndReturnsValidatedValues(t *testing.T) {
	var prompted string
	launcher, err := NewLauncher(t.Context(),
		WithOAuthBrowser(func(ctx context.Context, raw string) error {
			parsed, err := url.Parse(raw)
			if err != nil {
				return err
			}
			callback := oauthRedirectURL + "?code=accepted&state=" + url.QueryEscape(parsed.Query().Get("state")) + "&iss=" + url.QueryEscape("https://issuer.example")
			go func() {
				response, requestErr := http.Get(callback)
				if requestErr == nil {
					response.Body.Close()
				}
			}()
			return nil
		}),
		WithOAuthPrompt(func(_ context.Context, _ string, raw string, openErr error) error {
			if openErr != nil {
				t.Fatalf("browser open = %v", openErr)
			}
			prompted = raw
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	launcher.oauthTimeout = 5 * time.Second

	result, err := launcher.fetchAuthorizationCode(t.Context(), "docs", "https://auth.example/authorize?state=state-value")
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "accepted" || result.State != "state-value" || result.Iss != "https://issuer.example" {
		t.Fatalf("result = %#v", result)
	}
	if prompted == "" {
		t.Fatal("authorization URL was not surfaced")
	}
}

func TestOAuthBrowserFailureSurfacesFallbackWhileCallbackRemainsActive(t *testing.T) {
	openFailure := errors.New("no browser")
	launcher, err := NewLauncher(t.Context(),
		WithOAuthBrowser(func(context.Context, string) error { return openFailure }),
		WithOAuthPrompt(func(_ context.Context, _ string, raw string, got error) error {
			if !errors.Is(got, openFailure) {
				t.Fatalf("open error = %v", got)
			}
			parsed, _ := url.Parse(raw)
			callback := oauthRedirectURL + "?code=manual&state=" + url.QueryEscape(parsed.Query().Get("state"))
			go func() {
				response, requestErr := http.Get(callback)
				if requestErr == nil {
					response.Body.Close()
				}
			}()
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := launcher.fetchAuthorizationCode(t.Context(), "docs", "https://auth.example/authorize?state=fallback")
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "manual" || result.State != "fallback" {
		t.Fatalf("result = %#v", result)
	}
}

func TestOAuthCallbackIgnoresMismatchedErrorState(t *testing.T) {
	result := make(chan oauthCallbackResult, 1)
	handler := oauthCallbackHandler("expected", result)
	bad := httptest.NewRequest(http.MethodGet, oauthCallbackPath+"?error=denied&state=attacker", nil)
	badResponse := httptest.NewRecorder()
	handler.ServeHTTP(badResponse, bad)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("bad state status = %d", badResponse.Code)
	}
	select {
	case value := <-result:
		t.Fatalf("mismatched callback terminated flow: %#v", value)
	default:
	}
	valid := httptest.NewRequest(http.MethodGet, oauthCallbackPath+"?error=denied&state=expected", nil)
	handler.ServeHTTP(httptest.NewRecorder(), valid)
	select {
	case value := <-result:
		if value.err == nil || !strings.Contains(value.err.Error(), "denied") {
			t.Fatalf("callback result = %#v", value)
		}
	case <-time.After(time.Second):
		t.Fatal("valid OAuth error callback was not delivered")
	}
}

func TestOAuthAuthorizationTimesOutAndClosesCallback(t *testing.T) {
	launcher, err := NewLauncher(t.Context(),
		WithOAuthBrowser(func(context.Context, string) error { return nil }),
		WithOAuthPrompt(func(context.Context, string, string, error) error { return nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	launcher.oauthTimeout = 25 * time.Millisecond
	_, err = launcher.fetchAuthorizationCode(t.Context(), "docs", "https://auth.example/authorize?state=timeout")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	listener, listenErr := netListenOAuthCallback()
	if listenErr != nil {
		t.Fatalf("callback listener survived timeout: %v", listenErr)
	}
	listener.Close()
}

func netListenOAuthCallback() (io.Closer, error) {
	return net.Listen("tcp", oauthCallbackAddress)
}

type fakeOAuthHandler struct {
	token      *oauth2.Token
	nextToken  *oauth2.Token
	authorizes int
}

func (h *fakeOAuthHandler) TokenSource(context.Context) (oauth2.TokenSource, error) {
	return oauth2.StaticTokenSource(h.token), nil
}
func (h *fakeOAuthHandler) Authorize(_ context.Context, _ *http.Request, response *http.Response) error {
	h.authorizes++
	if h.nextToken != nil {
		h.token = h.nextToken
	}
	response.Body.Close()
	return nil
}

func TestLogoutOAuthDeletesOnlyTheConfiguredServerIdentity(t *testing.T) {
	endpoint := "https://example.com/mcp"
	key := oauthServerKey("docs", endpoint)
	other := oauthServerKey("docs", "https://other.example/mcp")
	store := &memoryMCPAuthStore{records: map[string]kitauth.MCPOAuthRecord{
		key: {Found: true, Revision: "one"}, other: {Found: true, Revision: "two"},
	}}
	launcher, err := NewLauncher(t.Context(), WithMCPAuthStore(store))
	if err != nil {
		t.Fatal(err)
	}
	if err := launcher.LogoutOAuth(t.Context(), "docs", endpoint); err != nil {
		t.Fatal(err)
	}
	if record := store.records[key]; record.Found || record.Revision == "" {
		t.Fatalf("configured server credentials survived logout: %#v", record)
	}
	if _, exists := store.records[other]; !exists {
		t.Fatal("logout removed credentials for a different endpoint")
	}
}

func TestConcurrentUnauthenticatedResponsesShareOneBrowserAuthorization(t *testing.T) {
	delegate := &fakeOAuthHandler{nextToken: &oauth2.Token{AccessToken: "fresh"}}
	handler := &recoveringOAuthHandler{delegate: delegate, state: &oauthSessionState{}}
	for range 2 {
		request, _ := http.NewRequest(http.MethodPost, "https://example.com/mcp", nil)
		response := &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("unauthorized"))}
		if err := handler.Authorize(t.Context(), request, response); err != nil {
			t.Fatal(err)
		}
	}
	if delegate.authorizes != 1 {
		t.Fatalf("browser authorizations = %d, want one", delegate.authorizes)
	}
}

func TestOAuthSaveGenerationConflictRequestsSessionRebuild(t *testing.T) {
	key := oauthServerKey("docs", "https://example.com/mcp")
	store := &memoryMCPAuthStore{records: map[string]kitauth.MCPOAuthRecord{key: {Found: true, Revision: "newer"}}}
	state := &oauthSessionState{ctx: t.Context(), store: store, key: key, revision: "stale"}
	config := &oauth2.Config{ClientID: "client", RedirectURL: oauthRedirectURL, Endpoint: oauth2.Endpoint{AuthURL: "https://auth.example/authorize", TokenURL: "https://auth.example/token"}}
	if err := state.save(config, &oauth2.Token{AccessToken: "access"}); !errors.Is(err, droidsmcp.ErrAuthenticationChanged) {
		t.Fatalf("save error = %v, want session rebuild", err)
	}
}

func TestRejectedStaleOAuthGenerationRequestsSessionRebuild(t *testing.T) {
	key := oauthServerKey("docs", "https://example.com/mcp")
	store := &memoryMCPAuthStore{records: map[string]kitauth.MCPOAuthRecord{key: {Found: true, Revision: "newer"}}}
	delegate := &fakeOAuthHandler{token: &oauth2.Token{AccessToken: "rejected"}}
	handler := &recoveringOAuthHandler{
		delegate: delegate,
		state:    &oauthSessionState{store: store, key: key, revision: "stale"},
		hadSaved: true,
	}
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/mcp", nil)
	request.Header.Set("Authorization", "Bearer rejected")
	response := &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("unauthorized"))}
	if err := handler.Authorize(t.Context(), request, response); !errors.Is(err, droidsmcp.ErrAuthenticationChanged) {
		t.Fatalf("authorization error = %v, want session rebuild", err)
	}
	if delegate.authorizes != 0 {
		t.Fatal("stale handler started a browser flow")
	}
}

func TestRejectedSavedOAuthIsClearedOnceBeforeReauthorization(t *testing.T) {
	key := oauthServerKey("docs", "https://example.com/mcp")
	store := &memoryMCPAuthStore{records: map[string]kitauth.MCPOAuthRecord{key: {Found: true, Revision: "saved"}}}
	delegate := &fakeOAuthHandler{token: &oauth2.Token{AccessToken: "rejected"}}
	handler := &recoveringOAuthHandler{
		delegate: delegate,
		state:    &oauthSessionState{store: store, key: key, revision: "saved"},
		hadSaved: true,
	}
	for range 2 {
		request, _ := http.NewRequest(http.MethodPost, "https://example.com/mcp", nil)
		request.Header.Set("Authorization", "Bearer rejected")
		response := &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("unauthorized"))}
		if err := handler.Authorize(t.Context(), request, response); err != nil {
			t.Fatal(err)
		}
	}
	if store.deletes != 1 {
		t.Fatalf("credential deletes = %d, want one", store.deletes)
	}
	if delegate.authorizes != 2 {
		t.Fatalf("authorization calls = %d", delegate.authorizes)
	}
}

var _ sdkauth.OAuthHandler = (*fakeOAuthHandler)(nil)
