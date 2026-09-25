package mcpruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	kitauth "github.com/akonwi/kit/internal/auth"
	droidsmcp "github.com/akonwi/kit/internal/droids/mcp"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

const (
	oauthCallbackAddress = "127.0.0.1:53173"
	oauthCallbackPath    = "/mcp/callback"
	oauthRedirectURL     = "http://" + oauthCallbackAddress + oauthCallbackPath
)

var oauthFlowSlot = make(chan struct{}, 1)

func oauthServerKey(name, endpoint string) string {
	digest := sha256.Sum256([]byte(endpoint))
	return name + "@" + hex.EncodeToString(digest[:])
}

func (l *Launcher) oauthHandler(ctx context.Context, serverName, endpoint string, client *http.Client) (sdkauth.OAuthHandler, error) {
	if l.oauthStore == nil {
		return nil, fmt.Errorf("MCP server %q requires OAuth but no credential store is configured", serverName)
	}
	key := oauthServerKey(serverName, endpoint)
	record, err := l.oauthStore.LoadMCPOAuth(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("load MCP OAuth credentials for %q: %w", serverName, err)
	}
	state := &oauthSessionState{ctx: l.lifetime, store: l.oauthStore, key: key, revision: record.Revision, issuer: record.Session.Issuer}

	config := &sdkauth.AuthorizationCodeHandlerConfig{
		RedirectURL: oauthRedirectURL,
		DynamicClientRegistrationConfig: &sdkauth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			RedirectURIs: []string{oauthRedirectURL}, ClientName: "Kit MCP Client (" + serverName + ")",
			GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
			TokenEndpointAuthMethod: "none",
		}},
		RequestRefreshToken: true,
		Client:              client,
		AuthorizationCodeFetcher: func(fetchCtx context.Context, args *sdkauth.AuthorizationArgs) (*sdkauth.AuthorizationResult, error) {
			result, err := l.fetchAuthorizationCode(fetchCtx, serverName, args.URL)
			if err == nil {
				state.setIssuer(result.Iss)
			}
			return result, err
		},
	}
	if record.Found {
		savedConfig, savedToken := record.Session.Config, record.Session.Token
		// Only reuse a registered client when the authorization server supplied an
		// issuer to bind it to. Without that binding, retain the token but perform
		// fresh dynamic registration if reauthorization becomes necessary.
		if record.Session.Issuer != "" {
			config.PreregisteredClient = savedClientCredentials(record.Session)
			config.DynamicClientRegistrationConfig = nil
		}
		refreshContext := context.WithValue(l.lifetime, oauth2.HTTPClient, client)
		config.InitialTokenSource = newSavingTokenSource(
			savedConfig.TokenSource(refreshContext, &savedToken), &savedConfig, &savedToken, state.save,
		)
	}
	config.NewTokenSource = func(tokenCtx context.Context, oauthConfig *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
		if err := state.save(oauthConfig, token); err != nil {
			return nil, err
		}
		return newSavingTokenSource(oauthConfig.TokenSource(tokenCtx, token), oauthConfig, token, state.save), nil
	}
	delegate, err := sdkauth.NewAuthorizationCodeHandler(config)
	if err != nil {
		return nil, fmt.Errorf("configure MCP OAuth for %q: %w", serverName, err)
	}
	return &recoveringOAuthHandler{
		delegate: delegate, state: state, hadSaved: record.Found,
	}, nil
}

type savingTokenSource struct {
	mu      sync.Mutex
	wrapped oauth2.TokenSource
	config  *oauth2.Config
	current oauth2.Token
	save    func(*oauth2.Config, *oauth2.Token) error
}

func newSavingTokenSource(wrapped oauth2.TokenSource, config *oauth2.Config, initial *oauth2.Token, save func(*oauth2.Config, *oauth2.Token) error) oauth2.TokenSource {
	if wrapped == nil {
		return nil
	}
	source := &savingTokenSource{wrapped: wrapped, config: config, save: save}
	if initial != nil {
		source.current = *initial
	}
	return source
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, err := s.wrapped.Token()
	if err != nil {
		return nil, err
	}
	if token != nil && (token.AccessToken != s.current.AccessToken || token.RefreshToken != s.current.RefreshToken || !token.Expiry.Equal(s.current.Expiry)) {
		if err := s.save(s.config, token); err != nil {
			return nil, err
		}
		s.current = *token
	}
	return token, nil
}

func savedClientCredentials(session kitauth.MCPOAuthSession) *oauthex.ClientCredentials {
	credentials := &oauthex.ClientCredentials{ClientID: session.Config.ClientID, Issuer: session.Issuer}
	if session.Config.ClientSecret != "" {
		credentials.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: session.Config.ClientSecret}
	}
	return credentials
}

type oauthSessionState struct {
	mu       sync.Mutex
	ctx      context.Context
	store    MCPAuthStore
	key      string
	revision string
	issuer   string
}

func (s *oauthSessionState) save(config *oauth2.Config, token *oauth2.Token) error {
	if config == nil || token == nil {
		return errors.New("save MCP OAuth credentials: missing OAuth session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	next, err := s.store.SaveMCPOAuth(ctx, s.key, s.revision, kitauth.MCPOAuthSession{Config: *config, Token: *token, Issuer: s.issuer})
	if err != nil {
		if errors.Is(err, kitauth.ErrCredentialsChanged) {
			return fmt.Errorf("%w: OAuth credentials were replaced while saving", droidsmcp.ErrAuthenticationChanged)
		}
		return fmt.Errorf("save MCP OAuth credentials: %w", err)
	}
	s.revision = next
	return nil
}

func (s *oauthSessionState) setIssuer(issuer string) {
	s.mu.Lock()
	s.issuer = issuer
	s.mu.Unlock()
}

func (s *oauthSessionState) clearRejected(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := s.store.DeleteMCPOAuthRevision(ctx, s.key, s.revision)
	if err != nil {
		if errors.Is(err, kitauth.ErrCredentialsChanged) {
			return fmt.Errorf("%w: rejected credential was already replaced", droidsmcp.ErrAuthenticationChanged)
		}
		return err
	}
	s.revision = next
	return nil
}

type recoveringOAuthHandler struct {
	delegate sdkauth.OAuthHandler
	state    *oauthSessionState

	mu       sync.Mutex
	hadSaved bool
}

func (h *recoveringOAuthHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return h.delegate.TokenSource(ctx)
}

func (h *recoveringOAuthHandler) Authorize(ctx context.Context, request *http.Request, response *http.Response) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// A concurrent request may have completed authorization while this response
	// waited for the single-flight lock. Retry it with the newer token instead of
	// opening a second browser flow.
	if source, err := h.delegate.TokenSource(ctx); err == nil && source != nil {
		if token, tokenErr := source.Token(); tokenErr == nil && token != nil {
			requestToken := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
			if token.AccessToken != "" && requestToken != token.AccessToken {
				response.Body.Close()
				return nil
			}
		}
	}
	if h.hadSaved {
		if err := h.state.clearRejected(ctx); err != nil {
			response.Body.Close()
			return fmt.Errorf("discard rejected MCP OAuth credentials: %w", err)
		}
		h.hadSaved = false
	}
	return h.delegate.Authorize(ctx, request, response)
}

func (l *Launcher) fetchAuthorizationCode(ctx context.Context, serverName, authorizationURL string) (*sdkauth.AuthorizationResult, error) {
	if l.oauthTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.oauthTimeout)
		defer cancel()
	}
	select {
	case oauthFlowSlot <- struct{}{}:
		defer func() { <-oauthFlowSlot }()
	case <-ctx.Done():
		return nil, fmt.Errorf("wait to authorize MCP server %q: %w", serverName, ctx.Err())
	}

	listener, err := net.Listen("tcp", oauthCallbackAddress)
	if err != nil {
		return nil, fmt.Errorf("start MCP OAuth callback for %q: %w", serverName, err)
	}
	defer listener.Close()
	parsedAuthorizationURL, err := url.Parse(authorizationURL)
	if err != nil {
		return nil, fmt.Errorf("parse MCP OAuth authorization URL: %w", err)
	}
	expectedState := parsedAuthorizationURL.Query().Get("state")
	if expectedState == "" {
		return nil, errors.New("MCP OAuth authorization URL is missing state")
	}
	result := make(chan oauthCallbackResult, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: oauthCallbackHandler(expectedState, result)}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = server.Shutdown(shutdownCtx)
		cancel()
		<-serveDone
	}()

	openErr := l.openBrowser(ctx, authorizationURL)
	if err := l.oauthPrompt(ctx, serverName, authorizationURL, openErr); err != nil {
		return nil, fmt.Errorf("present MCP OAuth authorization for %q: %w", serverName, err)
	}
	select {
	case callback := <-result:
		if callback.err != nil {
			return nil, callback.err
		}
		return &sdkauth.AuthorizationResult{Code: callback.code, State: callback.state, Iss: callback.issuer}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("authorize MCP server %q: %w", serverName, ctx.Err())
	}
}

type oauthCallbackResult struct {
	code, state, issuer string
	err                 error
}

func oauthCallbackHandler(expectedState string, result chan<- oauthCallbackResult) http.Handler {
	var once sync.Once
	complete := func(value oauthCallbackResult) { once.Do(func() { result <- value }) }
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		if request.Method != http.MethodGet || request.URL.Path != oauthCallbackPath {
			http.Error(writer, "Not found.", http.StatusNotFound)
			return
		}
		query := request.URL.Query()
		if state := query.Get("state"); state == "" || state != expectedState || len(state) > maximumCallbackValueBytes {
			http.Error(writer, "OAuth authorization failed: invalid state.", http.StatusBadRequest)
			return
		}
		if oauthError := query.Get("error"); oauthError != "" {
			if len(oauthError) > maximumCallbackValueBytes {
				http.Error(writer, "OAuth authorization failed: invalid error.", http.StatusBadRequest)
				complete(oauthCallbackResult{err: errors.New("OAuth authorization failed: oversized error")})
				return
			}
			message := "OAuth authorization failed: " + oauthError
			http.Error(writer, template.HTMLEscapeString(message), http.StatusBadRequest)
			complete(oauthCallbackResult{err: errors.New(message)})
			return
		}
		code, state := query.Get("code"), query.Get("state")
		if code == "" || len(code) > maximumCallbackValueBytes {
			http.Error(writer, "OAuth authorization failed: invalid callback.", http.StatusBadRequest)
			complete(oauthCallbackResult{err: errors.New("OAuth authorization failed: invalid callback")})
			return
		}
		issuer := query.Get("iss")
		if len(issuer) > maximumCallbackValueBytes {
			http.Error(writer, "OAuth authorization failed: invalid issuer.", http.StatusBadRequest)
			complete(oauthCallbackResult{err: errors.New("OAuth authorization failed: oversized issuer")})
			return
		}
		_, _ = writer.Write([]byte("<!doctype html><title>Kit MCP login complete</title><h1>Authorization complete</h1><p>You can close this window and return to Kit.</p>"))
		complete(oauthCallbackResult{code: code, state: state, issuer: issuer})
	})
}

const maximumCallbackValueBytes = 64 << 10

// LogoutOAuth removes saved credentials for one configured server.
func (l *Launcher) LogoutOAuth(ctx context.Context, serverName, endpoint string) error {
	if l == nil || l.oauthStore == nil {
		return errors.New("MCP OAuth credential store is unavailable")
	}
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return fmt.Errorf("MCP OAuth endpoint is invalid: %w", err)
	}
	return l.oauthStore.DeleteMCPOAuth(ctx, oauthServerKey(serverName, endpoint))
}
