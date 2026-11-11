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
	// View is the one workspace view this client shows in place of the
	// whole session, or the zero View for the whole session.
	View View
	// WorkspacePanes is where workspace views open: "rex" for Rex panes
	// when the client runs in Rex, or "tabs" for the client's own tabs.
	WorkspacePanes string
	// Views builds the command lines that show this client's session's
	// views in panes, or is nil when views can't open in panes.
	Views *ViewLauncher
}

// The kinds of View.
const (
	ViewDiff       = "diff"
	ViewFile       = "file"
	ViewHostFile   = "host-file"
	ViewSubagent   = "subagent"
	ViewScratchpad = "scratchpad"
)

// View names one workspace view of a session: a terminal client started
// with `kit --view` shows only it (ADR 0067).
type View struct {
	// Kind is one of ViewDiff, ViewFile, ViewHostFile, ViewSubagent, and
	// ViewScratchpad, or empty for the whole session.
	Kind string
	// Path is a File view's workspace-relative path, or a host File view's
	// absolute or ~/ path on the session's server.
	Path string
	// Line is the line a File view opens at, or 0.
	Line int
	// Conversation is a Subagent view's conversation.
	Conversation string
	// Annotation is the annotation a Diff or File view reveals, or 0.
	Annotation uint64
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
