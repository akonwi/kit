// Package anthropicoauth implements Anthropic's fixed Claude Pro/Max OAuth protocol.
package anthropicoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	ClientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	AuthorizeURL = "https://claude.ai/oauth/authorize"
	TokenURL     = "https://platform.claude.com/v1/oauth/token"
	Scopes       = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
)

// Credentials contains a Claude subscription access-token generation.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// TokenError reports a non-success response without exposing token values.
type TokenError struct {
	StatusCode int
}

func (e *TokenError) Error() string {
	return fmt.Sprintf("Anthropic OAuth request failed with HTTP %d", e.StatusCode)
}

// Client exchanges and refreshes credentials against Anthropic's fixed token endpoint.
type Client struct {
	httpClient *http.Client
	tokenURL   string
}

// NewClient constructs an OAuth client. Redirects are disabled so secrets are
// never forwarded away from Anthropic's fixed token origin.
func NewClient(client *http.Client) *Client {
	clone := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if client != nil {
		*clone = *client
		clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		if clone.Timeout == 0 {
			clone.Timeout = 30 * time.Second
		}
	}
	return &Client{httpClient: clone, tokenURL: TokenURL}
}

// Exchange trades an authorization code and PKCE verifier for credentials.
func (c *Client) Exchange(ctx context.Context, code, state, verifier, redirectURI string) (Credentials, error) {
	return c.request(ctx, map[string]string{
		"grant_type": "authorization_code", "client_id": ClientID, "code": code,
		"state": state, "redirect_uri": redirectURI, "code_verifier": verifier,
	})
}

// Refresh rotates an expired credential.
func (c *Client) Refresh(ctx context.Context, credentials Credentials) (Credentials, error) {
	return c.request(ctx, map[string]string{
		"grant_type": "refresh_token", "client_id": ClientID, "refresh_token": credentials.RefreshToken,
	})
}

func (c *Client) request(ctx context.Context, payload map[string]string) (Credentials, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Credentials{}, fmt.Errorf("encode Anthropic OAuth request: %w", err)
	}
	tokenURL := c.tokenURL
	if tokenURL == "" {
		tokenURL = TokenURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return Credentials{}, fmt.Errorf("create Anthropic OAuth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("send Anthropic OAuth request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Credentials{}, fmt.Errorf("read Anthropic OAuth response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Credentials{}, &TokenError{StatusCode: response.StatusCode}
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(responseBody, &token); err != nil {
		return Credentials{}, fmt.Errorf("decode Anthropic OAuth response: %w", err)
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.ExpiresIn <= 0 {
		return Credentials{}, errors.New("Anthropic OAuth response is incomplete")
	}
	return Credentials{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(token.ExpiresIn)*time.Second - 5*time.Minute)}, nil
}
