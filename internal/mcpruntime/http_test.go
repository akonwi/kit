package mcpruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/mcpconfig"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// recorder captures the headers each hop actually received.
type recorder struct {
	mu      sync.Mutex
	headers []http.Header
}

func (r *recorder) record(request *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.headers = append(r.headers, request.Header.Clone())
}

func (r *recorder) last() http.Header {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.headers) == 0 {
		return http.Header{}
	}
	return r.headers[len(r.headers)-1]
}

// clientFor builds the launcher's HTTP client for one configured server without
// going through a live MCP session.
func clientFor(t *testing.T, server mcpconfig.Server, opts ...Option) *http.Client {
	t.Helper()
	launcher, err := NewLauncher(t.Context(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: &scopedRoundTripper{
		base:      http.DefaultTransport,
		origin:    endpoint,
		headers:   server.Headers,
		auth:      server.Auth,
		lookupEnv: launcher.lookupEnv,
	}}
}

func TestHTTPRequestsCarryConfiguredHeadersAndBearerToken(t *testing.T) {
	seen := &recorder{}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.record(r)
	}))
	defer origin.Close()

	server := mcpconfig.Server{
		Name:      "remote",
		Transport: mcpconfig.TransportHTTP,
		URL:       origin.URL,
		Headers:   map[string]string{"X-Tenant": "acme"},
		Auth:      &mcpconfig.Auth{Kind: mcpconfig.AuthBearer, BearerToken: "static-secret"},
	}
	response, err := clientFor(t, server).Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	headers := seen.last()
	if got := headers.Get("X-Tenant"); got != "acme" {
		t.Errorf("X-Tenant = %q, want %q", got, "acme")
	}
	if got := headers.Get("Authorization"); got != "Bearer static-secret" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer static-secret")
	}
}

// Deferring BearerTokenEnv to request time means a rotated token is picked up
// without restarting Kit, and the token is never retained in configuration.
func TestHTTPBearerTokenEnvResolvesPerRequest(t *testing.T) {
	seen := &recorder{}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.record(r)
	}))
	defer origin.Close()

	token := "first-token"
	server := mcpconfig.Server{
		Name:      "remote",
		Transport: mcpconfig.TransportHTTP,
		URL:       origin.URL,
		Auth:      &mcpconfig.Auth{Kind: mcpconfig.AuthBearer, BearerTokenEnv: "ROTATING_TOKEN"},
	}
	client := clientFor(t, server, WithLookupEnv(func(key string) (string, bool) {
		if key == "ROTATING_TOKEN" {
			return token, true
		}
		return "", false
	}))

	for _, want := range []string{"first-token", "second-token"} {
		token = want
		response, err := client.Get(origin.URL)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if got := seen.last().Get("Authorization"); got != "Bearer "+want {
			t.Fatalf("Authorization = %q, want %q", got, "Bearer "+want)
		}
	}
}

// net/http strips sensitive headers when it follows a cross-origin redirect,
// but a round tripper runs again for the redirected request. Credentials must
// be scoped to the configured origin rather than reattached to the new host.
func TestHTTPCredentialsAreNotForwardedAcrossOrigins(t *testing.T) {
	elsewhere := &recorder{}
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.record(r)
	}))
	defer foreign.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	server := mcpconfig.Server{
		Name:      "remote",
		Transport: mcpconfig.TransportHTTP,
		URL:       origin.URL,
		Headers:   map[string]string{"X-Tenant": "acme"},
		Auth:      &mcpconfig.Auth{Kind: mcpconfig.AuthBearer, BearerToken: "static-secret"},
	}
	response, err := clientFor(t, server).Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	received := elsewhere.last()
	if got := received.Get("Authorization"); got != "" {
		t.Errorf("foreign host received Authorization = %q, want none", got)
	}
	if got := received.Get("X-Tenant"); got != "" {
		t.Errorf("foreign host received X-Tenant = %q, want none", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestHTTPResourceAuthorizationIsStrippedFromForeignOrigin(t *testing.T) {
	origin, _ := url.Parse("https://example.com/mcp")
	var received http.Header
	transport := &scopedRoundTripper{
		origin: origin,
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			received = request.Header.Clone()
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header), Request: request}, nil
		}),
		headers: map[string]string{"X-Tenant": "acme"},
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.com/redirected", nil)
	request.Header.Set("Authorization", "Bearer oauth-secret")
	request.Header.Set("X-Tenant", "copied")
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if got := received.Get("Authorization"); got != "" {
		t.Fatalf("foreign origin received Authorization = %q", got)
	}
	if got := received.Get("X-Tenant"); got != "" {
		t.Fatalf("foreign origin received configured header = %q", got)
	}
}

func TestHTTPCredentialsFollowSameOriginRedirect(t *testing.T) {
	seen := &recorder{}
	var origin *httptest.Server
	origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			http.Redirect(w, r, origin.URL+"/final", http.StatusTemporaryRedirect)
			return
		}
		seen.record(r)
	}))
	defer origin.Close()

	server := mcpconfig.Server{
		Name:      "remote",
		Transport: mcpconfig.TransportHTTP,
		URL:       origin.URL,
		Auth:      &mcpconfig.Auth{Kind: mcpconfig.AuthBearer, BearerToken: "static-secret"},
	}
	response, err := clientFor(t, server).Get(origin.URL + "/moved")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if got := seen.last().Get("Authorization"); got != "Bearer static-secret" {
		t.Fatalf("Authorization after same-origin redirect = %q, want it retained", got)
	}
}

func TestHTTPConnectionForceCancelsHangingGracefulClose(t *testing.T) {
	remote := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "remote", Version: "0.1.0"}, nil)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return remote }, nil)
	deleteStarted := make(chan struct{})
	deleteCancelled := make(chan struct{})
	var deleteOnce sync.Once
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteOnce.Do(func() { close(deleteStarted) })
			<-r.Context().Done()
			close(deleteCancelled)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer endpoint.Close()

	launcher, err := NewLauncher(t.Context(), func(l *Launcher) {
		l.httpCloseGrace = 25 * time.Millisecond
	})
	if err != nil {
		t.Fatal(err)
	}
	servers, err := launcher.Servers(t.TempDir(), []mcpconfig.Server{{
		Name: "remote", Transport: mcpconfig.TransportHTTP, URL: endpoint.URL,
	}})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := servers[0].Transport(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	connectCtx, cancelConnect := context.WithTimeout(t.Context(), 5*time.Second)
	session, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.1.0"}, nil).Connect(connectCtx, transport, nil)
	cancelConnect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	before := time.Now()
	if err := session.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	elapsed := time.Since(before)
	if elapsed < 25*time.Millisecond {
		t.Fatalf("close returned after %v, before graceful-close allowance", elapsed)
	}
	if elapsed > time.Second {
		t.Fatalf("close returned after %v, want forced cancellation", elapsed)
	}
	select {
	case <-deleteStarted:
	default:
		t.Fatal("SDK did not attempt graceful session deletion")
	}
	select {
	case <-deleteCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("hanging HTTP shutdown request was not cancelled")
	}
}

func TestHTTPServerReusesPersistedOAuthToken(t *testing.T) {
	remote := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "protected", Version: "0.1.0"}, nil)
	sdkmcp.AddTool(remote, &sdkmcp.Tool{Name: "probe"},
		func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{}, nil, nil
		})
	mcpHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return remote }, nil)
	var authorized atomic.Bool
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer saved-access" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+"http://"+r.Host+`/.well-known/oauth-protected-resource"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		authorized.Store(true)
		mcpHandler.ServeHTTP(w, r)
	}))
	defer endpoint.Close()

	key := oauthServerKey("protected", endpoint.URL)
	store := &memoryMCPAuthStore{records: map[string]auth.MCPOAuthRecord{
		key: {
			Found: true, Revision: "saved",
			Session: auth.MCPOAuthSession{
				Config: oauth2.Config{ClientID: "client", RedirectURL: oauthRedirectURL, Endpoint: oauth2.Endpoint{AuthURL: endpoint.URL + "/authorize", TokenURL: endpoint.URL + "/token"}},
				Token:  oauth2.Token{AccessToken: "saved-access", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)},
			},
		},
	}}
	launcher, err := NewLauncher(t.Context(), WithMCPAuthStore(store))
	if err != nil {
		t.Fatal(err)
	}
	servers, err := launcher.Servers(t.TempDir(), []mcpconfig.Server{{
		Name: "protected", Transport: mcpconfig.TransportHTTP, URL: endpoint.URL,
		Auth: &mcpconfig.Auth{Kind: mcpconfig.AuthOAuth},
	}})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := servers[0].Transport(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	session, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.1.0"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if !authorized.Load() {
		t.Fatal("server never received the persisted bearer token")
	}
}

func TestHTTPFactoryAttachesOAuthHandler(t *testing.T) {
	store := &memoryMCPAuthStore{records: map[string]auth.MCPOAuthRecord{}}
	launcher, err := NewLauncher(t.Context(), WithMCPAuthStore(store))
	if err != nil {
		t.Fatal(err)
	}
	factory, err := launcher.httpFactory(mcpconfig.Server{
		Name: "protected", Transport: mcpconfig.TransportHTTP, URL: "https://example.com/mcp",
		Auth: &mcpconfig.Auth{Kind: mcpconfig.AuthOAuth},
	})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := factory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	streamable := transport.(*sdkmcp.StreamableClientTransport)
	if streamable.OAuthHandler == nil {
		t.Fatal("OAuth transport has no handler")
	}
}

func TestHTTPFactoryRejectsOAuthWithoutCredentialStore(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	factory, err := launcher.httpFactory(mcpconfig.Server{
		Name: "protected", Transport: mcpconfig.TransportHTTP, URL: "https://example.com/mcp",
		Auth: &mcpconfig.Auth{Kind: mcpconfig.AuthOAuth},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory(t.Context()); err == nil {
		t.Fatal("OAuth transport accepted no credential store")
	}
}

func TestHTTPServerConnectsAndCallsTools(t *testing.T) {
	remote := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "remote", Version: "0.1.0"}, nil)
	var tenant string
	sdkmcp.AddTool(remote, &sdkmcp.Tool{Name: "probe", Description: "Report the observed tenant header."},
		func(ctx context.Context, request *sdkmcp.CallToolRequest, input struct{}) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: tenant}}}, nil, nil
		})
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return remote }, nil)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value := r.Header.Get("X-Tenant"); value != "" {
			tenant = value
		}
		handler.ServeHTTP(w, r)
	}))
	defer endpoint.Close()

	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	servers, err := launcher.Servers(t.TempDir(), []mcpconfig.Server{{
		Name:      "remote",
		Transport: mcpconfig.TransportHTTP,
		URL:       endpoint.URL,
		Headers:   map[string]string{"X-Tenant": "acme"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	transport, err := servers[0].Transport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.1.0"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "probe"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	text, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok || text.Text != "acme" {
		t.Fatalf("probe = %#v, want the configured tenant header", result.Content[0])
	}
}
