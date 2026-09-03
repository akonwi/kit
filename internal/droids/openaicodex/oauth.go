// Package openaicodex implements the OAuth protocol primitives used by
// OpenAI's ChatGPT-backed Codex service. It deliberately does not open a
// browser, render prompts, run a callback server, or persist credentials.
package openaicodex

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

const (
	clientID                  = "app_EMoamEEZ73f0CkXaXp7hrann"
	defaultAuthBaseURL        = "https://auth.openai.com"
	deviceAuthorizationExpiry = 15 * time.Minute
	minimumDevicePollInterval = time.Second
	defaultDevicePollInterval = 5 * time.Second
	maximumTokenLifetime      = 365 * 24 * time.Hour
	maximumOAuthBody          = 1 << 20
)

// DefaultRedirectURI is the loopback callback registered for Codex browser
// authorization.
const DefaultRedirectURI = "http://localhost:1455/auth/callback"

// DeviceVerificationURI is the page users visit for Codex device login.
const DeviceVerificationURI = "https://auth.openai.com/codex/device"

// Credentials are the complete application-owned OAuth credentials.
// Applications may pass them directly to droids.OpenAICodex or expose them
// through droids.OpenAICodexCredentialStore.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	AccountID    string
	ExpiresAt    time.Time
	FedRAMP      bool
}

// Authorization is one ephemeral browser authorization-code flow. It cannot
// be copied or reused safely; pass the pointer returned by BeginAuthorization
// directly to ExchangeAuthorizationCode.
type Authorization struct {
	URL string

	state       string
	verifier    string
	redirectURI string
	used        atomic.Bool
}

// State returns the unpredictable state expected on the OAuth callback.
func (a *Authorization) State() string {
	if a == nil {
		return ""
	}
	return a.state
}

// RedirectURI returns the fixed callback URI registered for the Codex client.
func (a *Authorization) RedirectURI() string {
	if a == nil {
		return ""
	}
	return a.redirectURI
}

// DeviceAuthorization describes a headless device-code flow. UserCode and
// VerificationURI are safe to display; all other state should remain local.
type DeviceAuthorization struct {
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresAt       time.Time

	deviceAuthID string
	used         atomic.Bool
}

// Error is a redacted OAuth protocol or HTTP failure.
type Error struct {
	Operation  string
	StatusCode int
	Code       string
	Message    string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	operation := e.Operation
	if operation == "" {
		operation = "request"
	}
	var details []string
	if e.StatusCode != 0 {
		details = append(details, "HTTP "+strconv.Itoa(e.StatusCode))
	}
	if e.Code != "" {
		details = append(details, e.Code)
	}
	prefix := "openai-codex oauth " + operation + " failed"
	if len(details) > 0 {
		prefix += " (" + strings.Join(details, ", ") + ")"
	}
	if e.Message != "" {
		return prefix + ": " + e.Message
	}
	return prefix
}

// Client performs OAuth protocol requests against OpenAI's fixed auth origin.
type Client struct {
	httpClient  *http.Client
	originator  string
	authBaseURL string
	now         func() time.Time
	rand        io.Reader
	sleep       func(context.Context, time.Duration) error
}

// NewClient constructs an OAuth protocol client. The originator should
// truthfully identify the embedding application, such as "kit". Redirects are
// disabled even when a custom HTTP client is supplied.
func NewClient(httpClient *http.Client, originator string) (*Client, error) {
	if originator == "" {
		originator = "droids"
	}
	if !validHeaderValue(originator) {
		return nil, fmt.Errorf("openai-codex oauth: originator is not a valid header value")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	clone := *httpClient
	if clone.Timeout == 0 {
		clone.Timeout = 30 * time.Second
	}
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("openai-codex oauth redirects are disabled")
	}
	return &Client{
		httpClient:  &clone,
		originator:  originator,
		authBaseURL: defaultAuthBaseURL,
		now:         time.Now,
		rand:        rand.Reader,
		sleep:       sleepContext,
	}, nil
}

// BeginAuthorization creates a PKCE browser flow. The application should open
// Authorization.URL, listen only on loopback for DefaultRedirectURI, validate
// method/path, and pass the callback code and state to ExchangeAuthorizationCode.
func (c *Client) BeginAuthorization() (*Authorization, error) {
	if c == nil {
		return nil, fmt.Errorf("openai-codex oauth: nil client")
	}
	verifier, err := randomBase64URL(c.rand, 32)
	if err != nil {
		return nil, fmt.Errorf("openai-codex oauth: generate PKCE verifier: %w", err)
	}
	state, err := randomBase64URL(c.rand, 24)
	if err != nil {
		return nil, fmt.Errorf("openai-codex oauth: generate state: %w", err)
	}
	challenge := sha256.Sum256([]byte(verifier))
	values := url.Values{
		"response_type":              {"code"},
		"client_id":                  {clientID},
		"redirect_uri":               {DefaultRedirectURI},
		"scope":                      {"openid profile email offline_access"},
		"code_challenge":             {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method":      {"S256"},
		"state":                      {state},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {c.originator},
	}
	return &Authorization{
		URL:         c.endpoint("/oauth/authorize") + "?" + values.Encode(),
		state:       state,
		verifier:    verifier,
		redirectURI: DefaultRedirectURI,
	}, nil
}

// ExchangeAuthorizationCode validates and consumes a browser flow, then
// exchanges its code for credentials. State is mandatory even for a manually
// pasted callback URL.
func (c *Client) ExchangeAuthorizationCode(ctx context.Context, authorization *Authorization, code, state string) (Credentials, error) {
	if authorization == nil {
		return Credentials{}, fmt.Errorf("openai-codex oauth: authorization is required")
	}
	if code == "" {
		return Credentials{}, fmt.Errorf("openai-codex oauth: authorization code is required")
	}
	if state == "" || state != authorization.state {
		return Credentials{}, fmt.Errorf("openai-codex oauth: state mismatch")
	}
	if !authorization.used.CompareAndSwap(false, true) {
		return Credentials{}, fmt.Errorf("openai-codex oauth: authorization was already consumed")
	}
	return c.exchange(ctx, code, authorization.verifier, authorization.redirectURI)
}

// BeginDeviceAuthorization starts a device-code flow for a headless client.
func (c *Client) BeginDeviceAuthorization(ctx context.Context) (*DeviceAuthorization, error) {
	requestBody, err := json.Marshal(map[string]string{"client_id": clientID})
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, http.MethodPost, c.endpoint("/api/accounts/deviceauth/usercode"), "application/json", requestBody)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := readBoundedBody(response.Body)
	if err != nil {
		return nil, &Error{Operation: "device authorization", StatusCode: response.StatusCode, Message: err.Error()}
	}
	if response.StatusCode != http.StatusOK {
		return nil, oauthResponseError("device authorization", response.StatusCode, body)
	}
	var payload struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		UserCodeAlt  string          `json:"usercode"`
		Interval     json.RawMessage `json:"interval"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &Error{Operation: "device authorization", StatusCode: response.StatusCode, Message: "invalid JSON response"}
	}
	if payload.UserCode == "" {
		payload.UserCode = payload.UserCodeAlt
	}
	interval := defaultDevicePollInterval
	var intervalErr error
	if len(payload.Interval) > 0 {
		interval, intervalErr = parseInterval(payload.Interval)
	}
	if intervalErr != nil || !validOpaqueOAuthValue(payload.DeviceAuthID, 4096) || !validDeviceUserCode(payload.UserCode) {
		return nil, &Error{Operation: "device authorization", StatusCode: response.StatusCode, Message: "response is missing or has malformed required fields"}
	}
	interval = clampDevicePollInterval(interval)
	return &DeviceAuthorization{
		UserCode:        payload.UserCode,
		VerificationURI: c.endpoint("/codex/device"),
		Interval:        interval,
		ExpiresAt:       c.now().Add(deviceAuthorizationExpiry),
		deviceAuthID:    payload.DeviceAuthID,
	}, nil
}

// PollDeviceAuthorization consumes a device-code flow and waits until the user
// authorizes it, it expires, or ctx is canceled.
func (c *Client) PollDeviceAuthorization(ctx context.Context, device *DeviceAuthorization) (Credentials, error) {
	if device == nil {
		return Credentials{}, fmt.Errorf("openai-codex oauth: device authorization is required")
	}
	if !device.used.CompareAndSwap(false, true) {
		return Credentials{}, fmt.Errorf("openai-codex oauth: device authorization was already consumed")
	}
	interval := clampDevicePollInterval(device.Interval)
	for {
		remaining := device.ExpiresAt.Sub(c.now())
		if remaining <= 0 {
			return Credentials{}, &Error{Operation: "device polling", Code: "expired", Message: "device authorization expired"}
		}
		wait := interval
		if wait > remaining {
			wait = remaining
		}
		if err := c.sleep(ctx, wait); err != nil {
			return Credentials{}, err
		}
		if !device.ExpiresAt.After(c.now()) {
			return Credentials{}, &Error{Operation: "device polling", Code: "expired", Message: "device authorization expired"}
		}
		requestBody, err := json.Marshal(map[string]string{
			"device_auth_id": device.deviceAuthID,
			"user_code":      device.UserCode,
		})
		if err != nil {
			return Credentials{}, err
		}
		response, err := c.do(ctx, http.MethodPost, c.endpoint("/api/accounts/deviceauth/token"), "application/json", requestBody)
		if err != nil {
			return Credentials{}, err
		}
		body, readErr := readBoundedBody(response.Body)
		response.Body.Close()
		if readErr != nil {
			return Credentials{}, &Error{Operation: "device polling", StatusCode: response.StatusCode, Message: readErr.Error()}
		}
		if response.StatusCode == http.StatusOK {
			var payload struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.Unmarshal(body, &payload); err != nil || payload.AuthorizationCode == "" || payload.CodeVerifier == "" {
				return Credentials{}, &Error{Operation: "device polling", StatusCode: response.StatusCode, Message: "response is missing required fields"}
			}
			return c.exchange(ctx, payload.AuthorizationCode, payload.CodeVerifier, c.endpoint("/deviceauth/callback"))
		}

		protocolErr := oauthResponseError("device polling", response.StatusCode, body)
		switch protocolErr.Code {
		case "deviceauth_authorization_pending", "authorization_pending":
			continue
		case "slow_down":
			if interval <= time.Duration(math.MaxInt64)-5*time.Second {
				interval += 5 * time.Second
			}
			continue
		case "":
			// The Codex device endpoint also uses an empty 403/404 response to
			// indicate that authorization is still pending.
			if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound {
				continue
			}
			fallthrough
		default:
			return Credentials{}, protocolErr
		}
	}
}

// Refresh exchanges the previous refresh token. Rotated refresh and ID tokens
// are preserved, and an account change is rejected.
func (c *Client) Refresh(ctx context.Context, previous Credentials) (Credentials, error) {
	if previous.RefreshToken == "" {
		return Credentials{}, fmt.Errorf("openai-codex oauth: refresh token is required")
	}
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {previous.RefreshToken},
		"client_id":     {clientID},
	}
	return c.tokenRequest(ctx, "refresh", values, previous)
}

func (c *Client) exchange(ctx context.Context, code, verifier, redirectURI string) (Credentials, error) {
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	return c.tokenRequest(ctx, "exchange", values, Credentials{})
}

func (c *Client) tokenRequest(ctx context.Context, operation string, values url.Values, previous Credentials) (Credentials, error) {
	response, err := c.do(ctx, http.MethodPost, c.endpoint("/oauth/token"), "application/x-www-form-urlencoded", []byte(values.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	defer response.Body.Close()
	body, err := readBoundedBody(response.Body)
	if err != nil {
		return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Message: err.Error()}
	}
	if response.StatusCode != http.StatusOK {
		return Credentials{}, oauthResponseError(operation, response.StatusCode, body)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Message: "invalid JSON response"}
	}
	returnedAccess := payload.AccessToken != ""
	returnedID := payload.IDToken != ""
	if !returnedAccess {
		payload.AccessToken = previous.AccessToken
	}
	if payload.RefreshToken == "" {
		payload.RefreshToken = previous.RefreshToken
	}
	if !returnedID {
		payload.IDToken = previous.IDToken
	}
	if payload.AccessToken == "" || payload.RefreshToken == "" {
		return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Message: "response is missing required fields"}
	}

	accessMetadata, accessMetadataErr := ParseAccountMetadata(payload.AccessToken)
	if accessMetadataErr != nil {
		// A newly returned access token must carry its own identity. Never bind
		// an opaque replacement token to stale ID-token or credential metadata.
		if returnedAccess || previous.AccountID == "" {
			return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Message: "access token is missing ChatGPT account metadata"}
		}
		accessMetadata = AccountMetadata{ID: previous.AccountID, FedRAMP: previous.FedRAMP}
	}
	if previous.AccountID != "" && previous.AccountID != accessMetadata.ID {
		return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Code: "account_changed", Message: "refreshed credentials belong to another ChatGPT account"}
	}

	fedRAMP := previous.FedRAMP
	if accessMetadata.HasFedRAMP {
		fedRAMP = accessMetadata.FedRAMP
	}
	if returnedID {
		idMetadata, err := parseOptionalAccountMetadata(payload.IDToken)
		if err != nil {
			return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Message: "id token has malformed ChatGPT account metadata"}
		}
		if idMetadata.ID != "" && idMetadata.ID != accessMetadata.ID {
			return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Code: "account_changed", Message: "access and id tokens belong to different ChatGPT accounts"}
		}
		if idMetadata.HasFedRAMP {
			if accessMetadata.HasFedRAMP && accessMetadata.FedRAMP != idMetadata.FedRAMP {
				return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Code: "routing_changed", Message: "access and id tokens disagree on FedRAMP routing"}
			}
			fedRAMP = idMetadata.FedRAMP
		}
	}

	now := c.now()
	jwtExpiry, hasJWTExpiry, expiryErr := tokenExpiry(payload.AccessToken, now)
	if expiryErr != nil {
		return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Message: expiryErr.Error()}
	}
	expiresAt := time.Time{}
	if payload.ExpiresIn < 0 || payload.ExpiresIn > int64(maximumTokenLifetime/time.Second) {
		return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Message: "expires_in is outside the supported range"}
	}
	if payload.ExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(payload.ExpiresIn) * time.Second)
		if hasJWTExpiry && jwtExpiry.Before(expiresAt) {
			expiresAt = jwtExpiry
		}
	} else if hasJWTExpiry {
		expiresAt = jwtExpiry
	} else if !returnedAccess && previous.ExpiresAt.After(now) {
		expiresAt = previous.ExpiresAt
	} else {
		return Credentials{}, &Error{Operation: operation, StatusCode: response.StatusCode, Message: "response is missing token expiry"}
	}
	return Credentials{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		IDToken:      payload.IDToken,
		AccountID:    accessMetadata.ID,
		ExpiresAt:    expiresAt,
		FedRAMP:      fedRAMP,
	}, nil
}

// AccountMetadata is non-secret routing metadata extracted from a trusted
// OpenAI OAuth token.
type AccountMetadata struct {
	ID         string
	FedRAMP    bool
	HasFedRAMP bool
}

// ParseAccountMetadata extracts the ChatGPT account and regulated-routing
// claims from a token. It does not verify the JWT signature and is intended
// only for tokens returned by the fixed OpenAI token endpoint over verified
// TLS.
func ParseAccountMetadata(token string) (AccountMetadata, error) {
	metadata, err := parseOptionalAccountMetadata(token)
	if err != nil {
		return AccountMetadata{}, err
	}
	if metadata.ID == "" {
		return AccountMetadata{}, fmt.Errorf("openai-codex oauth: ChatGPT account id claim is missing")
	}
	return metadata, nil
}

func parseOptionalAccountMetadata(token string) (AccountMetadata, error) {
	claims, err := jwtClaims(token)
	if err != nil {
		return AccountMetadata{}, err
	}
	value, exists := claims["https://api.openai.com/auth"]
	if !exists {
		return AccountMetadata{}, nil
	}
	var auth struct {
		AccountID string `json:"chatgpt_account_id"`
		FedRAMP   *bool  `json:"chatgpt_account_is_fedramp"`
	}
	if err := json.Unmarshal(value, &auth); err != nil {
		return AccountMetadata{}, fmt.Errorf("openai-codex oauth: decode ChatGPT account metadata")
	}
	metadata := AccountMetadata{ID: auth.AccountID}
	if auth.FedRAMP != nil {
		metadata.FedRAMP = *auth.FedRAMP
		metadata.HasFedRAMP = true
	}
	return metadata, nil
}

// AccountID extracts ChatGPT's account id claim from an access token. It does
// not verify the JWT signature and is intended only for tokens returned by the
// fixed OpenAI token endpoint over a verified TLS connection.
func AccountID(accessToken string) (string, error) {
	metadata, err := ParseAccountMetadata(accessToken)
	if err != nil {
		return "", err
	}
	return metadata.ID, nil
}

// AccessTokenExpiry extracts the optional expiry claim from an OAuth access
// token. It does not verify the JWT signature; callers should use it only for
// tokens obtained from OpenAI's fixed OAuth endpoint.
func AccessTokenExpiry(token string) (time.Time, bool, error) {
	claims, err := jwtClaims(token)
	if err != nil {
		return time.Time{}, false, err
	}
	raw, exists := claims["exp"]
	if !exists {
		return time.Time{}, false, nil
	}
	var expires json.Number
	if err := json.Unmarshal(raw, &expires); err != nil {
		return time.Time{}, false, fmt.Errorf("token expiry claim is malformed")
	}
	seconds, err := expires.Int64()
	if err != nil || seconds <= 0 {
		return time.Time{}, false, fmt.Errorf("token expiry claim is malformed")
	}
	return time.Unix(seconds, 0), true, nil
}

func tokenExpiry(token string, now time.Time) (time.Time, bool, error) {
	expiry, ok, err := AccessTokenExpiry(token)
	if err != nil || !ok {
		return expiry, ok, err
	}
	if !expiry.After(now) {
		return time.Time{}, false, fmt.Errorf("token is expired")
	}
	if expiry.After(now.Add(maximumTokenLifetime)) {
		return time.Time{}, false, fmt.Errorf("token expiry is outside the supported range")
	}
	return expiry, true, nil
}

func jwtClaims(token string) (map[string]json.RawMessage, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("openai-codex oauth: token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("openai-codex oauth: decode token claims")
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("openai-codex oauth: decode token claims")
	}
	return claims, nil
}

func (c *Client) do(ctx context.Context, method, endpoint, contentType string, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai-codex oauth: create request: %w", err)
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "droids/openai-codex-oauth")
	request.Header.Set("originator", c.originator)
	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Operation: "request", Message: err.Error()}
	}
	return response, nil
}

func (c *Client) endpoint(path string) string {
	return strings.TrimRight(c.authBaseURL, "/") + path
}

func randomBase64URL(source io.Reader, size int) (string, error) {
	data := make([]byte, size)
	if _, err := io.ReadFull(source, data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func readBoundedBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maximumOAuthBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maximumOAuthBody {
		return nil, fmt.Errorf("response exceeds 1 MiB")
	}
	return body, nil
}

func oauthResponseError(operation string, status int, body []byte) *Error {
	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Code    string          `json:"code"`
		Message string          `json:"message"`
	}
	_ = json.Unmarshal(body, &envelope)
	code, message := envelope.Code, envelope.Message
	if len(envelope.Error) > 0 {
		var object struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(envelope.Error, &object) == nil {
			if object.Code != "" {
				code = object.Code
			} else if object.Type != "" {
				code = object.Type
			}
			if object.Message != "" {
				message = object.Message
			}
		} else {
			var text string
			if json.Unmarshal(envelope.Error, &text) == nil && text != "" {
				code = text
			}
		}
	}
	return &Error{
		Operation:  operation,
		StatusCode: status,
		Code:       truncate(code, 128),
		Message:    truncate(message, 512),
	}
}

func parseInterval(raw json.RawMessage) (time.Duration, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("missing interval")
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return 0, fmt.Errorf("invalid interval")
		}
		number = json.Number(strings.TrimSpace(text))
	}
	seconds, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64)/float64(time.Second) {
		return 0, fmt.Errorf("invalid interval")
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func clampDevicePollInterval(interval time.Duration) time.Duration {
	if interval < minimumDevicePollInterval {
		return minimumDevicePollInterval
	}
	return interval
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func truncate(value string, maximum int) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) ||
			unicode.Is(unicode.Zl, character) || unicode.Is(unicode.Zp, character) {
			return ' '
		}
		return character
	}, value)
	characters := []rune(value)
	if len(characters) <= maximum {
		return value
	}
	return string(characters[:maximum]) + "…"
}

func validDeviceUserCode(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func validOpaqueOAuthValue(value string, maximum int) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func validHeaderValue(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
