package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/droids/anthropicoauth"
)

const anthropicRedirectURI = "http://localhost:53692/callback"

// AnthropicLoginInstructions are safe values displayed while browser OAuth is
// pending. The browser redirects to RedirectURI on the machine running it,
// whose client relays the redirect, or the code the page shows, to Login.
type AnthropicLoginInstructions struct {
	AuthorizationURL string
	RedirectURI      string
}

// AnthropicOAuthLogin performs one Claude Pro/Max browser OAuth flow.
type AnthropicOAuthLogin struct {
	oauth *anthropicoauth.Client
	store *Store
}

// AnthropicOAuthLoginOptions configures Claude subscription login.
type AnthropicOAuthLoginOptions struct {
	Store      *Store
	HTTPClient *http.Client
}

// NewAnthropicOAuthLogin constructs a login service using Anthropic's fixed OAuth origins.
func NewAnthropicOAuthLogin(options AnthropicOAuthLoginOptions) (*AnthropicOAuthLogin, error) {
	if options.Store == nil {
		return nil, fmt.Errorf("auth: Anthropic credential store is required")
	}
	return &AnthropicOAuthLogin{oauth: anthropicoauth.NewClient(options.HTTPClient), store: options.Store}, nil
}

// Login reports the browser URL, waits for an authorization (the relayed
// redirect URL or a pasted code) on authorization, and installs the
// credential.
func (l *AnthropicOAuthLogin) Login(ctx context.Context, authorization <-chan string, notify func(AnthropicLoginInstructions) error) error {
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
	// The state is separate from the verifier, which never leaves this
	// process: whoever sees the authorization URL or the redirect cannot
	// exchange the code without it.
	state, err := randomURLToken()
	if err != nil {
		return fmt.Errorf("generate Anthropic OAuth state: %w", err)
	}
	parameters := url.Values{
		"code": {"true"}, "client_id": {anthropicoauth.ClientID}, "response_type": {"code"},
		"redirect_uri": {anthropicRedirectURI}, "scope": {anthropicoauth.Scopes},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {state},
	}
	if err := notify(AnthropicLoginInstructions{AuthorizationURL: anthropicoauth.AuthorizeURL + "?" + parameters.Encode(), RedirectURI: anthropicRedirectURI}); err != nil {
		return err
	}

	var input string
	select {
	case <-ctx.Done():
		return ctx.Err()
	case input = <-authorization:
	}
	result, err := parseAnthropicAuthorization(input, state)
	if err != nil {
		return err
	}
	credentials, err := l.oauth.Exchange(ctx, result.code, result.state, verifier, anthropicRedirectURI)
	if err != nil {
		return fmt.Errorf("exchange Anthropic authorization code: %w", err)
	}
	if err := l.store.ReplaceAnthropicOAuthCredentials(ctx, droids.AnthropicCredentials{
		AccessToken: credentials.AccessToken, RefreshToken: credentials.RefreshToken, ExpiresAt: credentials.ExpiresAt,
	}); err != nil {
		return fmt.Errorf("save Anthropic credentials: %w", err)
	}
	return nil
}

type authorizationResult struct {
	code, state string
}

func newAnthropicPKCE() (string, string, error) {
	verifier, err := randomURLToken()
	if err != nil {
		return "", "", fmt.Errorf("generate Anthropic OAuth verifier: %w", err)
	}
	digest := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

// randomURLToken returns 256 random bits, URL-safe.
func randomURLToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func parseAnthropicAuthorization(input, expectedState string) (authorizationResult, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return authorizationResult{}, fmt.Errorf("Anthropic authorization code is missing")
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		if oauthError := parsed.Query().Get("error"); oauthError != "" {
			return authorizationResult{}, fmt.Errorf("Anthropic authorization was not granted (%s)", oauthError)
		}
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
