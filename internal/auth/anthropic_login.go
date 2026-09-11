package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/anthropicoauth"
)

const anthropicCallbackAddress = "127.0.0.1:53692"
const anthropicRedirectURI = "http://localhost:53692/callback"

// AnthropicLoginInstructions are safe values displayed while browser OAuth is pending.
type AnthropicLoginInstructions struct {
	AuthorizationURL string
	RedirectURI      string
}

// AnthropicOAuthLogin performs one Claude Pro/Max browser OAuth flow.
type AnthropicOAuthLogin struct {
	oauth       *anthropicoauth.Client
	store       *Store
	acquireSave func(context.Context) (func() error, error)
}

// AnthropicOAuthLoginOptions configures Claude subscription login.
type AnthropicOAuthLoginOptions struct {
	Store       *Store
	HTTPClient  *http.Client
	AcquireSave func(context.Context) (func() error, error)
}

// NewAnthropicOAuthLogin constructs a login service using Anthropic's fixed OAuth origins.
func NewAnthropicOAuthLogin(options AnthropicOAuthLoginOptions) (*AnthropicOAuthLogin, error) {
	if options.Store == nil {
		return nil, fmt.Errorf("auth: Anthropic credential store is required")
	}
	return &AnthropicOAuthLogin{oauth: anthropicoauth.NewClient(options.HTTPClient), store: options.Store, acquireSave: options.AcquireSave}, nil
}

// Login starts a loopback callback server, reports the browser URL, accepts an
// optional manually pasted code or redirect URL, and installs the credential.
func (l *AnthropicOAuthLogin) Login(ctx context.Context, manualCode <-chan string, notify func(AnthropicLoginInstructions) error) error {
	if l == nil || l.oauth == nil || l.store == nil {
		return fmt.Errorf("auth: Anthropic login is not configured")
	}
	if notify == nil {
		return fmt.Errorf("auth: Anthropic login notifier is required")
	}
	verifier, challenge, err := newAnthropicPKCE()
	if err != nil {
		return err
	}
	listener, _ := net.Listen("tcp", anthropicCallbackAddress)
	result := make(chan authorizationResult, 1)
	if listener != nil {
		defer listener.Close()
		server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: anthropicCallbackHandler(verifier, result)}
		go func() { _ = server.Serve(listener) }()
		defer func() {
			shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		}()
	}

	parameters := url.Values{
		"code": {"true"}, "client_id": {anthropicoauth.ClientID}, "response_type": {"code"},
		"redirect_uri": {anthropicRedirectURI}, "scope": {anthropicoauth.Scopes},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {verifier},
	}
	if err := notify(AnthropicLoginInstructions{AuthorizationURL: anthropicoauth.AuthorizeURL + "?" + parameters.Encode(), RedirectURI: anthropicRedirectURI}); err != nil {
		return err
	}

	var authorization authorizationResult
	select {
	case <-ctx.Done():
		return ctx.Err()
	case authorization = <-result:
	case input := <-manualCode:
		authorization, err = parseAnthropicAuthorization(input, verifier)
		if err != nil {
			return err
		}
	}
	if authorization.err != nil {
		return authorization.err
	}
	credentials, err := l.oauth.Exchange(ctx, authorization.code, authorization.state, verifier, anthropicRedirectURI)
	if err != nil {
		return fmt.Errorf("exchange Anthropic authorization code: %w", err)
	}

	release := func() error { return nil }
	if l.acquireSave != nil {
		release, err = l.acquireSave(ctx)
		if err != nil {
			return fmt.Errorf("authorize Anthropic credential save: %w", err)
		}
		if release == nil {
			return fmt.Errorf("auth: Anthropic login save guard returned no release function")
		}
	}
	saveErr := l.store.ReplaceAnthropicOAuthCredentials(ctx, droids.AnthropicCredentials{
		AccessToken: credentials.AccessToken, RefreshToken: credentials.RefreshToken, ExpiresAt: credentials.ExpiresAt,
	})
	if saveErr != nil {
		saveErr = fmt.Errorf("save Anthropic credentials: %w", saveErr)
	}
	return errors.Join(saveErr, release())
}

type authorizationResult struct {
	code, state string
	err         error
}

func anthropicCallbackHandler(expectedState string, result chan<- authorizationResult) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/callback" {
			http.NotFound(writer, request)
			return
		}
		code, state := request.URL.Query().Get("code"), request.URL.Query().Get("state")
		if oauthError := request.URL.Query().Get("error"); oauthError != "" {
			if state != expectedState {
				http.Error(writer, "Invalid OAuth callback.", http.StatusBadRequest)
				return
			}
			http.Error(writer, "Anthropic authentication did not complete.", http.StatusBadRequest)
			select {
			case result <- authorizationResult{err: fmt.Errorf("Anthropic authentication failed")}:
			default:
			}
			return
		}
		if code == "" || state == "" || state != expectedState {
			http.Error(writer, "Invalid OAuth callback.", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte("<!doctype html><title>Kit connected</title><p>Anthropic authentication completed. You can close this window.</p>"))
		select {
		case result <- authorizationResult{code: code, state: state}:
		default:
		}
	})
}

func newAnthropicPKCE() (string, string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", "", fmt.Errorf("generate Anthropic OAuth verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(value)
	digest := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func parseAnthropicAuthorization(input, expectedState string) (authorizationResult, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return authorizationResult{}, fmt.Errorf("Anthropic authorization code is missing")
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		return validateAnthropicAuthorization(parsed.Query().Get("code"), parsed.Query().Get("state"), expectedState)
	}
	if strings.Contains(value, "#") {
		parts := strings.SplitN(value, "#", 2)
		return validateAnthropicAuthorization(parts[0], parts[1], expectedState)
	}
	if strings.Contains(value, "code=") {
		parameters, err := url.ParseQuery(value)
		if err != nil {
			return authorizationResult{}, fmt.Errorf("parse Anthropic authorization response: %w", err)
		}
		return validateAnthropicAuthorization(parameters.Get("code"), parameters.Get("state"), expectedState)
	}
	return authorizationResult{code: value, state: expectedState}, nil
}

func validateAnthropicAuthorization(code, state, expectedState string) (authorizationResult, error) {
	if code == "" {
		return authorizationResult{}, fmt.Errorf("Anthropic authorization code is missing")
	}
	if state == "" {
		state = expectedState
	}
	if state != expectedState {
		return authorizationResult{}, fmt.Errorf("Anthropic OAuth state mismatch")
	}
	return authorizationResult{code: code, state: state}, nil
}
