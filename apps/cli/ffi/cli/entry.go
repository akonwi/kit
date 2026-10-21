package cli

import (
	"context"

	kit "github.com/akonwi/kit/api"
	kittheme "github.com/akonwi/kit/internal/theme"
)

// TUIOptions configures one terminal client attached to one session.
type TUIOptions struct {
	Context context.Context
	Client  *kit.Client
	// Server labels the attached server, or is empty for this home's own.
	Server string
	// PairingURLs are the URLs that reach the attached server, for pairing
	// links: the saved URL of a URL server, or a home's served URLs.
	PairingURLs []string
	// ServerHome is the server user's home directory, for abbreviating
	// server paths, or empty when unknown.
	ServerHome      string
	CWD             string
	Location        string
	ResolveLocation func(context.Context, string) string
	// Model and Thinking are what the command line named: they select which
	// session resumes and start a new one. Empty lets the server choose.
	Model              string
	Thinking           string
	AvailableProviders map[string]bool
	Authenticated      bool
	SessionID          string
	NewSessionID       string
	NewSessionName     string
	TemporarySession   bool
	Login              DeviceLogin
	BrowserLogin       BrowserLogin
	APIKeyLogin        APIKeyLogin
	Logout             ProviderLogout
	ThemeName          string
	ThemeDefinition    kittheme.Definition
	ThemeService       ThemeService
	DiffWrapLines      bool
	// DiffLayout is the saved diff layout, "unified" or "split".
	DiffLayout            string
	DiffPreferenceService DiffPreferenceService
	// Relaunch receives the client's request to relaunch after an update.
	Relaunch *Relaunch
}

// DeviceLogin performs OpenAI Codex device-code login on the attached server.
type DeviceLogin interface {
	Login(context.Context, func(DeviceLoginInstructions) error) error
}

// BrowserLogin performs Claude subscription browser OAuth on the attached
// server. The channel carries a code the user pasted from the authorization
// page.
type BrowserLogin interface {
	Login(context.Context, <-chan string, func(BrowserLoginInstructions) error) error
}

// APIKeyLogin installs an API-key credential and activates its provider.
type APIKeyLogin interface {
	Login(context.Context, string, string) error
}

// ProviderLogout removes the attached server's stored credential for a
// provider.
type ProviderLogout interface {
	Logout(context.Context, string) error
}

// ThemeService discovers, loads, and persists terminal themes.
type ThemeService interface {
	Discover() ([]string, error)
	Load(string) (kittheme.Definition, []kittheme.Diagnostic, error)
	Save(string) error
}

// DiffPreferenceService persists working-tree diff presentation preferences.
type DiffPreferenceService interface {
	SetWrapLines(bool) error
	// SetLayout saves the diff layout, "unified" or "split".
	SetLayout(string) error
}
