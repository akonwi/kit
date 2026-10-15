package contract

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/providerlogin"
)

// LoginMethod is a way to log in to a provider.
type LoginMethod string

// Login methods.
const (
	// LoginMethodDevice shows a verification URL and a code to enter there.
	LoginMethodDevice LoginMethod = "device"
	// LoginMethodBrowser opens an authorization page whose redirect, or the
	// code it shows, is submitted to the login.
	LoginMethodBrowser LoginMethod = "browser"
	// LoginMethodAPIKey saves an API key with SetProviderAPIKey.
	LoginMethodAPIKey LoginMethod = "apiKey"
)

// EnumValues lists the login methods.
func (LoginMethod) EnumValues() []string { return []string{"device", "browser", "apiKey"} }

// CredentialSource is where the server's credential for a provider comes
// from.
type CredentialSource string

// Credential sources.
const (
	// CredentialSourceStore is the server's credential store, which logins
	// and API keys write.
	CredentialSourceStore CredentialSource = "store"
	// CredentialSourceEnvironment is the server's environment, which takes
	// precedence and cannot be changed through the API.
	CredentialSourceEnvironment CredentialSource = "environment"
)

// EnumValues lists the credential sources.
func (CredentialSource) EnumValues() []string { return []string{"store", "environment"} }

// ProviderInfo describes a model provider and the server's credential for it.
type ProviderInfo struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Methods []LoginMethod `json:"methods"`
	// Configured reports whether the server has a credential for the
	// provider.
	Configured bool `json:"configured"`
	// Source is where the credential comes from, or would be stored.
	Source CredentialSource `json:"source"`
}

// ProviderList lists the server's providers in display order.
type ProviderList struct {
	Providers []ProviderInfo `json:"providers"`
}

// StartLoginInput starts a login to Provider with Method, device or browser.
type StartLoginInput struct {
	Provider string      `json:"provider"`
	Method   LoginMethod `json:"method"`
}

// LoginState is a login's progress. Every state but pending is final.
type LoginState string

// Login states.
const (
	LoginStatePending    LoginState = "pending"
	LoginStateSucceeded  LoginState = "succeeded"
	LoginStateFailed     LoginState = "failed"
	LoginStateExpired    LoginState = "expired"
	LoginStateCancelled  LoginState = "cancelled"
	LoginStateSuperseded LoginState = "superseded"
)

// EnumValues lists the login states.
func (LoginState) EnumValues() []string {
	return []string{"pending", "succeeded", "failed", "expired", "cancelled", "superseded"}
}

// DeviceLoginInstructions tell the user where to enter a device code.
type DeviceLoginInstructions struct {
	VerificationURI string `json:"verificationUri"`
	UserCode        string `json:"userCode"`
	ExpiresAt       string `json:"expiresAt"`
}

// BrowserLoginInstructions tell the user which page authorizes the login. The
// page redirects to RedirectURI on the machine running the browser, whose
// client relays the redirect to the server.
type BrowserLoginInstructions struct {
	AuthorizationURL string `json:"authorizationUrl"`
	RedirectURI      string `json:"redirectUri"`
}

// ProviderLogin is a server-owned login in progress or recently finished.
type ProviderLogin struct {
	ID       string      `json:"id"`
	Provider string      `json:"provider"`
	Method   LoginMethod `json:"method"`
	State    LoginState  `json:"state"`
	// Device or Browser holds the instructions for the login's method while
	// it is pending. A finished login carries neither.
	Device  *DeviceLoginInstructions  `json:"device,omitempty"`
	Browser *BrowserLoginInstructions `json:"browser,omitempty"`
	// Failure explains a failed login.
	Failure string `json:"failure,omitempty"`
	// ExpiresAt is when a pending login expires.
	ExpiresAt string `json:"expiresAt"`
}

// LoginAuthorizationInput completes a browser login with the redirect URL the
// browser was sent to, or the code the authorization page showed.
type LoginAuthorizationInput struct {
	Authorization string `json:"authorization"`
}

// ProviderAPIKeyInput is an API key for a provider. The server never returns
// it.
type ProviderAPIKeyInput struct {
	APIKey string `json:"apiKey"`
}

// MaxAPIKeyBytes bounds an API key.
const MaxAPIKeyBytes = 4 << 10

func validProviderID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, character := range id {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func validLoginMethod(method LoginMethod) bool {
	return method == LoginMethodDevice || method == LoginMethodBrowser || method == LoginMethodAPIKey
}

func validHTTPSURL(value string, allowLoopbackHTTP bool) bool {
	if !validRendererText(value, 4<<10) {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return parsed.Scheme == "https" || (allowLoopbackHTTP && parsed.Scheme == "http" &&
		(parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1"))
}

// Validate checks a provider crossing a transport boundary.
func (provider ProviderInfo) Validate() error {
	if !validProviderID(provider.ID) {
		return fmt.Errorf("provider id %q is invalid", provider.ID)
	}
	if !validRendererText(provider.Name, 128) || strings.TrimSpace(provider.Name) != provider.Name || provider.Name == "" {
		return fmt.Errorf("provider %s name is invalid", provider.ID)
	}
	if len(provider.Methods) == 0 || len(provider.Methods) > 3 {
		return fmt.Errorf("provider %s must list one to three login methods", provider.ID)
	}
	seen := map[LoginMethod]bool{}
	for _, method := range provider.Methods {
		if !validLoginMethod(method) || seen[method] {
			return fmt.Errorf("provider %s lists an invalid or repeated login method", provider.ID)
		}
		seen[method] = true
	}
	if provider.Source != CredentialSourceStore && provider.Source != CredentialSourceEnvironment {
		return fmt.Errorf("provider %s credential source is invalid", provider.ID)
	}
	return nil
}

// Validate checks a provider list crossing a transport boundary.
func (list ProviderList) Validate() error {
	if len(list.Providers) > 64 {
		return fmt.Errorf("provider list exceeds 64 providers")
	}
	seen := map[string]bool{}
	for _, provider := range list.Providers {
		if err := provider.Validate(); err != nil {
			return err
		}
		if seen[provider.ID] {
			return fmt.Errorf("provider %s is listed twice", provider.ID)
		}
		seen[provider.ID] = true
	}
	return nil
}

// Validate checks a login request crossing a transport boundary.
func (input StartLoginInput) Validate() error {
	if !validProviderID(input.Provider) {
		return fmt.Errorf("provider id %q is invalid", input.Provider)
	}
	if input.Method != LoginMethodDevice && input.Method != LoginMethodBrowser {
		return fmt.Errorf("login method must be device or browser")
	}
	return nil
}

// Validate checks a login crossing a transport boundary.
func (login ProviderLogin) Validate() error {
	if !identifier.Valid(login.ID, "login_") {
		return fmt.Errorf("login id %q is not canonical", login.ID)
	}
	if !validProviderID(login.Provider) {
		return fmt.Errorf("login provider id %q is invalid", login.Provider)
	}
	if login.Method != LoginMethodDevice && login.Method != LoginMethodBrowser {
		return fmt.Errorf("login method must be device or browser")
	}
	switch login.State {
	case LoginStatePending, LoginStateSucceeded, LoginStateFailed, LoginStateExpired, LoginStateCancelled, LoginStateSuperseded:
	default:
		return fmt.Errorf("login state %q is invalid", login.State)
	}
	if login.Device != nil && login.Method != LoginMethodDevice || login.Browser != nil && login.Method != LoginMethodBrowser {
		return fmt.Errorf("login instructions do not match its method")
	}
	if login.State == LoginStatePending && login.Device == nil && login.Browser == nil {
		return fmt.Errorf("a pending login must carry its instructions")
	}
	if login.State != LoginStatePending && (login.Device != nil || login.Browser != nil) {
		return fmt.Errorf("a finished login carries no instructions")
	}
	if device := login.Device; device != nil {
		if !validHTTPSURL(device.VerificationURI, false) {
			return fmt.Errorf("device verification URI must be an HTTPS URL")
		}
		if device.UserCode == "" || !validRendererText(device.UserCode, 64) {
			return fmt.Errorf("device user code is invalid")
		}
		if _, err := time.Parse(time.RFC3339Nano, device.ExpiresAt); err != nil {
			return fmt.Errorf("device code expiry is not an RFC 3339 timestamp")
		}
	}
	if browser := login.Browser; browser != nil {
		if !validHTTPSURL(browser.AuthorizationURL, false) {
			return fmt.Errorf("browser authorization URL must be an HTTPS URL")
		}
		if !validHTTPSURL(browser.RedirectURI, true) {
			return fmt.Errorf("browser redirect URI must be HTTPS or loopback HTTP")
		}
	}
	if login.Failure != "" && login.State != LoginStateFailed {
		return fmt.Errorf("only a failed login carries a failure")
	}
	if login.Failure != "" && !validRendererText(login.Failure, 1024) {
		return fmt.Errorf("login failure is not renderer-safe text")
	}
	if _, err := time.Parse(time.RFC3339Nano, login.ExpiresAt); err != nil {
		return fmt.Errorf("login expiry is not an RFC 3339 timestamp")
	}
	return nil
}

// Validate checks a browser authorization crossing a transport boundary.
func (input LoginAuthorizationInput) Validate() error {
	if strings.TrimSpace(input.Authorization) == "" || !validProtocolText(input.Authorization, providerlogin.MaxAuthorizationBytes) {
		return fmt.Errorf("authorization must be non-empty text of at most %d bytes", providerlogin.MaxAuthorizationBytes)
	}
	return nil
}

// Validate checks an API key crossing a transport boundary.
func (input ProviderAPIKeyInput) Validate() error {
	if input.APIKey == "" || len(input.APIKey) > MaxAPIKeyBytes || strings.TrimSpace(input.APIKey) != input.APIKey ||
		strings.ContainsFunc(input.APIKey, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return fmt.Errorf("API key must be non-empty, without whitespace or control characters, and at most %d bytes", MaxAPIKeyBytes)
	}
	return nil
}
