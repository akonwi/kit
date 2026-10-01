package cli

import (
	"context"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/sessionclient"
	kittheme "github.com/akonwi/kit/internal/theme"
)

// Dependencies are supplied by the executable entry point. The terminal client
// functions run on the calling goroutine until the client exits.
type Dependencies struct {
	RunTUI      RunTUIFunc
	PickSession PickSessionFunc
}

// RunTUIFunc starts a terminal client attached to one session and blocks until
// it exits.
type RunTUIFunc func(TUIOptions) error

// PickSessionFunc presents the saved-session picker and returns the selected
// session ID, or an empty ID when the user dismisses it.
type PickSessionFunc func(SessionPickerOptions) (string, error)

// TUIOptions configures one terminal client attached to one session.
type TUIOptions struct {
	Context               context.Context
	Server                sessionclient.Server
	Transport             httpapi.Transport
	CWD                   string
	Location              string
	ResolveLocation       func(context.Context, string) string
	DefaultModel          string
	DefaultThinking       string
	ResumeModelFilter     string
	ResumeThinkingFilter  string
	AvailableProviders    map[string]bool
	Authenticated         bool
	SessionID             string
	NewSessionID          string
	NewSessionName        string
	TemporarySession      bool
	Login                 DeviceLogin
	BrowserLogin          BrowserLogin
	APIKeyLogin           APIKeyLogin
	ThemeName             string
	ThemeDefinition       kittheme.Definition
	ThemeService          ThemeService
	ModelOverrideService  ModelOverrideService
	DiffWrapLines         bool
	DiffPreferenceService DiffPreferenceService
}

// SessionPickerOptions configures the saved-session picker.
type SessionPickerOptions struct {
	Context context.Context
	Server  sessionclient.Server
}

// DeviceLogin performs OpenAI Codex device-code login.
type DeviceLogin interface {
	Login(context.Context, func(auth.OpenAICodexDeviceInstructions) error) error
}

// BrowserLogin performs Claude subscription browser OAuth.
type BrowserLogin interface {
	Login(context.Context, <-chan string, func(auth.AnthropicLoginInstructions) error) error
}

// APIKeyLogin installs an API-key credential and activates its provider.
type APIKeyLogin interface {
	Login(context.Context, string, string) error
}

// ThemeService discovers, loads, and persists terminal themes.
type ThemeService interface {
	Discover() ([]string, error)
	Load(string) (kittheme.Definition, []kittheme.Diagnostic, error)
	Save(string) error
}

// ModelOverrideService persists per-model context-window overrides.
type ModelOverrideService interface {
	SetContextWindow(selector string, contextWindow int) error
}

// DiffPreferenceService persists working-tree diff presentation preferences.
type DiffPreferenceService interface {
	SetWrapLines(bool) error
}
