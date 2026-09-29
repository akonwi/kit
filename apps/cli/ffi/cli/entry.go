package cli

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/sessionclient"
	kittheme "github.com/akonwi/kit/internal/theme"
	"github.com/akonwi/kit/internal/version"
)

// Dependencies are supplied by the executable entry point. Build metadata
// replaces linker-stamped version values, and the terminal client functions
// run on the calling goroutine until the client exits.
type Dependencies struct {
	Version     string
	Commit      string
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

// Main runs Kit's command line for the current process and returns its exit
// code. Interrupt and termination signals cancel the command context; a
// signal-caused interruption exits with 128 plus the signal number.
func Main(deps Dependencies) int {
	if deps.Version != "" {
		version.Version = deps.Version
	}
	if deps.Commit != "" {
		version.Commit = deps.Commit
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	var received atomic.Int32
	go func() {
		select {
		case sig := <-signals:
			if systemSignal, ok := sig.(syscall.Signal); ok {
				received.Store(int32(systemSignal))
			}
			cancel()
		case <-ctx.Done():
		}
	}()

	exitCode := Run(ctx, os.Args[1:], os.Stdout, os.Stderr, deps)
	if exitCode == 130 && received.Load() != 0 {
		exitCode = 128 + int(received.Load())
	}
	return exitCode
}
