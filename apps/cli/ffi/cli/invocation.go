package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/daemonctl"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/settings"
	kittheme "github.com/akonwi/kit/internal/theme"
)

// The kinds of Invocation.
const (
	// InvocationExit is a command that already ran, or failed to parse; the
	// process exits with Invocation.ExitCode.
	InvocationExit = "exit"
	// InvocationInteractive runs the terminal client.
	InvocationInteractive = "interactive"
	// InvocationSessions picks a saved session, then runs the terminal client
	// attached to it.
	InvocationSessions = "sessions"
)

// Invocation is a parsed command line. Headless commands run during Parse;
// interactive ones are left to the entry point.
type Invocation struct {
	Kind     string
	ExitCode int
	options  interactiveOptions
}

// Parse runs Kit's command tree. Help, usage errors, and headless commands
// (print, auth, server, version) run here and report their exit code; the
// interactive commands are returned for the entry point to run.
func Parse(ctx context.Context, args []string, stdout, stderr io.Writer) Invocation {
	invocation := Invocation{Kind: InvocationExit}
	deps := defaultCommandDependencies()
	deps.interactive = func(_ context.Context, options interactiveOptions, _, _ io.Writer) int {
		invocation = Invocation{Kind: InvocationInteractive, options: options}
		return 0
	}
	deps.sessions = func(_ context.Context, options interactiveOptions, _, _ io.Writer) int {
		invocation = Invocation{Kind: InvocationSessions, options: options}
		return 0
	}
	code := executeCommand(ctx, args, stdout, stderr, deps)
	if code != 0 || invocation.Kind == InvocationExit {
		return Invocation{Kind: InvocationExit, ExitCode: code}
	}
	return invocation
}

// Open returns the invocation as an interactive one attached to sessionID,
// as chosen from the session picker.
func Open(invocation Invocation, sessionID string) Invocation {
	invocation.Kind = InvocationInteractive
	invocation.options.SessionID = sessionID
	return invocation
}

// Environment is the local server connection and the user's settings that an
// interactive invocation runs with. A failed Open reports to stderr and sets
// ExitCode; Close releases the connection either way.
type Environment struct {
	ExitCode        int
	Client          *kit.Client
	ThemeDefinition kittheme.Definition

	paths     apphome.Paths
	cwd       string
	settings  *settings.Store
	themeName string
	diffWrap  bool
}

// OpenEnvironment validates the working directory, starts or connects to the
// local server, and loads the user's settings and theme.
func OpenEnvironment(ctx context.Context, invocation Invocation, stderr io.Writer) Environment {
	failed := func(code int) Environment { return Environment{ExitCode: interrupted(ctx, code)} }
	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return failed(1)
	}
	cwd := ""
	if invocation.Kind == InvocationInteractive && invocation.options.SessionID == "" {
		cwd, err = resolveCWD(invocation.options.CWD)
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return failed(1)
		}
	}
	client, err := bootstrapLocalClient(ctx, paths)
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return failed(1)
	}
	settingsStore, err := settings.NewStore(paths.Settings)
	if err != nil {
		client.Close()
		fmt.Fprintf(stderr, "kit: configure settings: %v\n", err)
		return failed(1)
	}
	loadedSettings, settingsWarnings, err := settingsStore.Load()
	if err != nil {
		fmt.Fprintf(stderr, "kit: load settings: %v; using system theme\n", err)
		loadedSettings.Theme = kittheme.SystemName
	}
	for _, warning := range settingsWarnings {
		fmt.Fprintf(stderr, "kit: %v\n", warning)
	}
	var themeDefinition kittheme.Definition
	if loadedSettings.Theme != kittheme.SystemName {
		var diagnostics []kittheme.Diagnostic
		themeDefinition, diagnostics, err = kittheme.Load(paths.Themes, loadedSettings.Theme)
		if err != nil {
			fmt.Fprintf(stderr, "kit: load theme %q: %v; using system theme\n", loadedSettings.Theme, err)
			loadedSettings.Theme = kittheme.SystemName
			themeDefinition = kittheme.Definition{}
		} else {
			for _, diagnostic := range diagnostics {
				fmt.Fprintf(stderr, "kit: theme %q: %v\n", loadedSettings.Theme, diagnostic)
			}
		}
	}
	return Environment{
		Client:          client,
		ThemeDefinition: themeDefinition,
		paths:           paths,
		cwd:             cwd,
		settings:        settingsStore,
		themeName:       loadedSettings.Theme,
		diffWrap:        loadedSettings.DiffWrapLines,
	}
}

// Close releases the server connection.
func (e Environment) Close() {
	if e.Client != nil {
		e.Client.Close()
	}
}

// Prepared is an interactive invocation ready to run as Options. A failed
// Prepare reports to stderr and sets ExitCode.
type Prepared struct {
	ExitCode int
	Options  TUIOptions

	client      *kit.Client
	temporaryID string
}

// Prepare resolves the session to attach, reads the server's providers, and
// configures login, producing the terminal client's options.
func (e Environment) Prepare(ctx context.Context, invocation Invocation, stderr io.Writer) Prepared {
	failed := func(code int) Prepared { return Prepared{ExitCode: interrupted(ctx, code)} }
	options := invocation.options
	cwd := e.cwd
	var err error
	if options.Temporary && options.NewSessionID == "" {
		options.NewSessionID, err = identifier.New("session_")
		if err != nil {
			fmt.Fprintf(stderr, "kit: prepare temporary session: %v\n", err)
			return failed(1)
		}
	}
	if options.SessionID != "" {
		resolveContext, resolveCancel := context.WithTimeout(ctx, 3*time.Second)
		selected, resolveErr := e.Client.ResolveSession(resolveContext, options.SessionID)
		resolveCancel()
		if resolveErr != nil {
			fmt.Fprintf(stderr, "kit: resolve session: %v\n", resolveErr)
			return failed(1)
		}
		options.SessionID = selected.ID
		cwd = selected.CWD
	}

	probeContext, probeCancel := context.WithTimeout(ctx, 3*time.Second)
	_, health, err := daemonctl.NewClient(e.paths).Probe(probeContext)
	probeCancel()
	if err != nil {
		fmt.Fprintf(stderr, "kit: inspect daemon providers: %v\n", err)
		return failed(1)
	}
	// Credential-source support is checked under the daemon startup lock at
	// login time, not by restarting a healthy daemon during client startup.
	providers, defaultModel := interactiveProviders(health.Providers)
	authenticated := len(providers) > 0
	if options.Model != "" {
		defaultModel = options.Model
		provider, _, _ := strings.Cut(options.Model, "/")
		if !providers[provider] {
			authenticated = false
		}
	}
	manager := daemonctl.NewManager(e.paths)
	credentialStore := auth.NewStore(e.paths.Auth)
	login, err := auth.NewOpenAICodexDeviceLogin(auth.OpenAICodexDeviceLoginOptions{
		Store: credentialStore,
		AcquireSave: func(ctx context.Context) (func() error, error) {
			return manager.AcquireCredentialStoreMutation(ctx, auth.OpenAICodexProviderID)
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "kit: configure OpenAI Codex login: %v\n", err)
		return failed(1)
	}
	browserLogin, err := auth.NewAnthropicOAuthLogin(auth.AnthropicOAuthLoginOptions{
		Store: credentialStore,
		AcquireSave: func(ctx context.Context) (func() error, error) {
			return manager.AcquireCredentialStoreMutation(ctx, auth.AnthropicProviderID)
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "kit: configure Claude subscription login: %v\n", err)
		return failed(1)
	}
	apiKeyLogin, err := auth.NewAPIKeyLogin(auth.APIKeyLoginOptions{
		Store: credentialStore, AcquireSave: manager.AcquireCredentialStoreMutation,
	})
	if err != nil {
		fmt.Fprintf(stderr, "kit: configure API-key login: %v\n", err)
		return failed(1)
	}
	prepared := Prepared{
		Options: TUIOptions{
			Context:               ctx,
			Client:                e.Client,
			CWD:                   cwd,
			Location:              interactiveLocation(ctx, cwd),
			ResolveLocation:       interactiveLocation,
			DefaultModel:          defaultModel,
			DefaultThinking:       options.Thinking,
			ResumeModelFilter:     options.Model,
			ResumeThinkingFilter:  options.Thinking,
			AvailableProviders:    providers,
			Authenticated:         authenticated,
			SessionID:             options.SessionID,
			NewSessionID:          options.NewSessionID,
			NewSessionName:        options.Name,
			TemporarySession:      options.Temporary,
			Login:                 login,
			BrowserLogin:          browserLogin,
			APIKeyLogin:           apiKeyLogin,
			ThemeName:             e.themeName,
			ThemeDefinition:       e.ThemeDefinition,
			ThemeService:          interactiveThemeService{directory: e.paths.Themes, settings: e.settings},
			ModelOverrideService:  interactiveModelOverrideService{settings: e.settings},
			DiffWrapLines:         e.diffWrap,
			DiffPreferenceService: interactiveDiffPreferenceService{settings: e.settings},
		},
		client: e.Client,
	}
	if options.Temporary {
		prepared.temporaryID = options.NewSessionID
	}
	return prepared
}

// Finish disposes a temporary session and returns the process exit code for
// a terminal client that ended with failure, or with no failure when empty.
func (p Prepared) Finish(ctx context.Context, failure string, stderr io.Writer) int {
	if p.temporaryID != "" {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		cleanupErr := cleanupTemporarySession(cleanupContext, p.client, p.temporaryID)
		cleanupCancel()
		if cleanupErr != nil {
			fmt.Fprintf(stderr, "kit: dispose temporary session: %v\n", cleanupErr)
			if failure == "" {
				return interrupted(ctx, 1)
			}
		}
	}
	if failure != "" {
		fmt.Fprintf(stderr, "kit: terminal UI: %s\n", failure)
		return interrupted(ctx, 1)
	}
	if ctx.Err() != nil {
		return 130
	}
	return 0
}

// PickerFailure reports a session picker that could not run and returns the
// exit code.
func PickerFailure(ctx context.Context, failure string, stderr io.Writer) int {
	if ctx.Err() != nil {
		return 130
	}
	fmt.Fprintf(stderr, "kit: session picker: %s\n", failure)
	return 1
}

// interrupted reports a failure caused by a cancelled command as an
// interruption, as Parse does for headless commands.
func interrupted(ctx context.Context, code int) int {
	if code == 1 && errors.Is(ctx.Err(), context.Canceled) {
		return 130
	}
	return code
}
