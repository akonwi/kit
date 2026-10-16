//go:build darwin || linux

package mcpruntime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	droidsmcp "github.com/akonwi/kit/droids/mcp"
	kitauth "github.com/akonwi/kit/internal/auth"
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

func TestOAuthAsksTheToolCallsUserAndReturnsTheRedirectsValues(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var asked droidsmcp.AuthorizationRequest
	launcher.authorize = func(_ context.Context, request droidsmcp.AuthorizationRequest) (string, error) {
		asked = request
		return oauthRedirectURL + "?code=accepted&state=state-value&iss=" + url.QueryEscape("https://issuer.example"), nil
	}
	result, err := launcher.fetchAuthorizationCode(t.Context(), "docs", "https://auth.example/authorize?state=state-value")
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "accepted" || result.State != "state-value" || result.Iss != "https://issuer.example" {
		t.Fatalf("result = %#v", result)
	}
	if asked != (droidsmcp.AuthorizationRequest{Server: "docs", URL: "https://auth.example/authorize?state=state-value", RedirectURI: "http://127.0.0.1:53173/mcp/callback"}) {
		t.Fatalf("asked = %#v", asked)
	}
}

func TestOAuthAnswerParsing(t *testing.T) {
	for _, test := range []struct {
		name, answer string
		want         *sdkauth.AuthorizationResult
		err          string
	}{
		{name: "redirect", answer: "http://127.0.0.1:53173/mcp/callback?code=c1&state=expected", want: &sdkauth.AuthorizationResult{Code: "c1", State: "expected"}},
		{name: "pasted query", answer: "  code=c2&state=expected&iss=https%3A%2F%2Fi.example  ", want: &sdkauth.AuthorizationResult{Code: "c2", State: "expected", Iss: "https://i.example"}},
		{name: "bare code", answer: "c3==", err: "OAuth authorization failed: paste the whole address the browser ended up at, not only the code"},
		{name: "other state", answer: "http://127.0.0.1:53173/mcp/callback?code=c&state=attacker", err: "OAuth authorization failed: the redirect is for a different authorization"},
		{name: "denied", answer: "http://127.0.0.1:53173/mcp/callback?error=access_denied&state=expected", err: "OAuth authorization failed: access_denied"},
		{name: "no code", answer: "http://127.0.0.1:53173/mcp/callback?state=expected", err: "OAuth authorization failed: the redirect has no code"},
		{name: "empty", answer: " ", err: "OAuth authorization failed: invalid answer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseAuthorizationAnswer(test.answer, "expected")
			if test.err != "" {
				if err == nil || err.Error() != test.err {
					t.Fatalf("error = %v, want %q", err, test.err)
				}
				return
			}
			if err != nil || *got != *test.want {
				t.Fatalf("result = %#v, %v", got, err)
			}
		})
	}
}

func TestOAuthWithoutAnAskingUserFailsWithAnExplanation(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = launcher.fetchAuthorizationCode(t.Context(), "docs", "https://auth.example/authorize?state=s")
	if err == nil || err.Error() != `MCP server "docs" needs authorization, which only the session's own conversation can ask the user for` {
		t.Fatalf("error = %v", err)
	}
}

func TestOAuthAuthorizationTimesOut(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	launcher.oauthTimeout = 25 * time.Millisecond
	launcher.authorize = func(ctx context.Context, _ droidsmcp.AuthorizationRequest) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	_, err = launcher.fetchAuthorizationCode(t.Context(), "docs", "https://auth.example/authorize?state=timeout")
	if err == nil || err.Error() != `MCP server "docs" was not authorized within 25ms` {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestOAuthRefusesAnAuthorizationPageThatIsNotHTTPS(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	launcher.authorize = func(context.Context, droidsmcp.AuthorizationRequest) (string, error) {
		t.Fatal("asked to open a page that is not https")
		return "", nil
	}
	_, err = launcher.fetchAuthorizationCode(t.Context(), "docs", "http://auth.example/authorize?state=s")
	if err == nil || err.Error() != `MCP server "docs" has an authorization page that is not https` {
		t.Fatalf("error = %v", err)
	}
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

// A subagent or print-mode call meeting a rejected credential leaves it, and
// registers no client, since no one can authorize a replacement.
func TestRejectedSavedOAuthIsKeptWhenNoOneCanAuthorize(t *testing.T) {
	key := oauthServerKey("docs", "https://example.com/mcp")
	store := &memoryMCPAuthStore{records: map[string]kitauth.MCPOAuthRecord{key: {Found: true, Revision: "saved"}}}
	delegate := &fakeOAuthHandler{token: &oauth2.Token{AccessToken: "rejected"}}
	handler := &recoveringOAuthHandler{
		delegate:     delegate,
		state:        &oauthSessionState{store: store, key: key, revision: "saved"},
		hadSaved:     true,
		server:       "docs",
		canAuthorize: droidsmcp.CanAuthorize,
	}
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/mcp", nil)
	request.Header.Set("Authorization", "Bearer rejected")
	response := &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("unauthorized"))}
	err := handler.Authorize(t.Context(), request, response)
	if err == nil || err.Error() != `MCP server "docs" needs authorization, which only the session's own conversation can ask the user for` {
		t.Fatalf("error = %v", err)
	}
	if store.deletes != 0 || delegate.authorizes != 0 {
		t.Fatalf("deletes = %d, authorizations = %d", store.deletes, delegate.authorizes)
	}
}

var _ sdkauth.OAuthHandler = (*fakeOAuthHandler)(nil)
