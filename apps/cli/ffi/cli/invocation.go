package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
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
	// InvocationServers picks a saved server, then runs the terminal client
	// attached to it.
	InvocationServers = "servers"
)

// Invocation is a parsed command line. Headless commands run during Parse;
// interactive ones are left to the entry point.
type Invocation struct {
	Kind     string
	ExitCode int
	options  interactiveOptions
	servers  []ServerChoice
}

// Servers lists the servers an InvocationServers picks from.
func (i Invocation) Servers() []ServerChoice { return i.servers }

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
	deps.attach = func(ctx context.Context, options interactiveOptions, server string, _, stderr io.Writer) int {
		if server == "" {
			choices, err := serverChoices(ctx)
			if err != nil {
				fmt.Fprintf(stderr, "kit: %v\n", err)
				return 1
			}
			if len(choices) == 0 {
				fmt.Fprintln(stderr, "kit: no saved servers; add one with `kit servers add NAME HOME`")
				return 2
			}
			invocation = Invocation{Kind: InvocationServers, options: options, servers: choices}
			return 0
		}
		target, err := resolveAttachTarget(ctx, server)
		if err != nil {
			return reportAttachError(stderr, err)
		}
		options.Server = target
		invocation = Invocation{Kind: InvocationInteractive, options: options}
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

// Attach returns the invocation as an interactive one attached to the server
// named server, as chosen from the server picker. A server that cannot be
// resolved is reported to stderr and yields an InvocationExit.
func Attach(ctx context.Context, invocation Invocation, server string, stderr io.Writer) Invocation {
	target, err := resolveAttachTarget(ctx, server)
	if err != nil {
		return Invocation{Kind: InvocationExit, ExitCode: reportAttachError(stderr, err)}
	}
	invocation.Kind = InvocationInteractive
	invocation.options.Server = target
	invocation.servers = nil
	return invocation
}

func reportAttachError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "kit: %v\n", err)
	var usage usageError
	if errors.As(err, &usage) {
		return 2
	}
	return 1
}

// Environment is the local server connection and the user's settings that an
// interactive invocation runs with. A failed Open reports to stderr and sets
// ExitCode; Close releases the connection either way.
type Environment struct {
	ExitCode        int
	Client          *kit.Client
	ThemeDefinition kittheme.Definition
	// ServerHome is the server user's home directory, for abbreviating
	// server paths.
	ServerHome string

	// paths is this client's home, for settings and themes.
	paths apphome.Paths
	// serverPaths is the Kit home of an attached server on this machine, for
	// provider login; it is empty for a URL server.
	serverPaths apphome.Paths
	// server labels an attached server other than this home's own.
	server string
	// serverURL is the origin of a URL server.
	serverURL string
	cwd       string
	settings  *settings.Store
	themeName string
	diffWrap  bool
}

// OpenEnvironment validates the working directory, connects to the server,
// and loads the user's settings and theme. This home's own server is started
// when needed; another server is only attached to.
func OpenEnvironment(ctx context.Context, invocation Invocation, stderr io.Writer) Environment {
	failed := func(code int) Environment { return Environment{ExitCode: interrupted(ctx, code)} }
	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return failed(1)
	}
	target := invocation.options.Server
	needsCWD := invocation.Kind == InvocationInteractive && invocation.options.SessionID == ""
	// A server attached by Kit home runs on this machine, so the shell's
	// working directory is meaningful to it. A URL server may not be, so its
	// directory is chosen once it reports where it runs.
	cwd := ""
	if needsCWD && target.URL == "" {
		cwd, err = resolveCWD(invocation.options.CWD)
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return failed(1)
		}
	}
	var serverPaths apphome.Paths
	var client *kit.Client
	switch {
	case target.own():
		serverPaths = paths
		client, err = bootstrapLocalClient(ctx, paths)
	case target.Home != "":
		serverPaths = apphome.FromHome(target.Home)
		client, err = connectAttachTarget(ctx, target)
	default:
		client, err = connectAttachTarget(ctx, target)
	}
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return failed(1)
	}
	// A server attached by Kit home shares this machine and user. A URL
	// server reports its home, which places new sessions on another machine.
	serverHome, _ := os.UserHomeDir()
	if target.URL != "" {
		infoContext, cancel := context.WithTimeout(ctx, 3*time.Second)
		info, err := client.Server(infoContext)
		cancel()
		if err == nil && needsCWD {
			cwd, err = serverCWD(info, target.Name, invocation.options.CWD)
		} else if err != nil {
			err = fmt.Errorf("inspect server %q: %w", target.Name, err)
		}
		if err != nil {
			client.Close()
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return failed(1)
		}
		serverHome = info.Home
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
		ServerHome:      serverHome,
		paths:           paths,
		serverPaths:     serverPaths,
		server:          target.Name,
		serverURL:       target.URL,
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
	info, err := e.Client.Server(probeContext)
	probeCancel()
	if err != nil {
		fmt.Fprintf(stderr, "kit: inspect server providers: %v\n", err)
		return failed(1)
	}
	// Credential-source support is checked under the daemon startup lock at
	// login time, not by restarting a healthy daemon during client startup.
	providers, defaultModel := interactiveProviders(info.Providers)
	authenticated := len(providers) > 0
	missingProvider := ""
	if options.Model != "" {
		defaultModel = options.Model
		provider, _, _ := strings.Cut(options.Model, "/")
		if !providers[provider] {
			authenticated = false
			missingProvider = provider
		}
	}
	if e.serverPaths.Home == "" {
		// Provider login writes the server's credential store, which a URL
		// server does not expose until login moves behind the session API.
		if !authenticated {
			if missingProvider != "" {
				fmt.Fprintf(stderr, "kit: server %q has no credentials for %s; log in on that machine with `kit`, then attach again\n", e.server, missingProvider)
			} else {
				fmt.Fprintf(stderr, "kit: server %q has no provider credentials; log in on that machine with `kit`, then attach again\n", e.server)
			}
			return failed(1)
		}
		return e.prepared(ctx, options, cwd, defaultModel, providers, authenticated, remoteDeviceLogin{}, remoteBrowserLogin{}, remoteAPIKeyLogin{})
	}
	manager := daemonctl.NewManager(e.serverPaths)
	credentialStore := auth.NewStore(e.serverPaths.Auth)
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
	return e.prepared(ctx, options, cwd, defaultModel, providers, authenticated, login, browserLogin, apiKeyLogin)
}

// prepared assembles the terminal client's options.
func (e Environment) prepared(
	ctx context.Context, options interactiveOptions, cwd, defaultModel string,
	providers map[string]bool, authenticated bool,
	login DeviceLogin, browserLogin BrowserLogin, apiKeyLogin APIKeyLogin,
) Prepared {
	prepared := Prepared{
		Options: TUIOptions{
			Context:               ctx,
			Client:                e.Client,
			Server:                e.server,
			PairingURLs:           e.pairingURLs(),
			CWD:                   cwd,
			ServerHome:            e.ServerHome,
			Location:              homeRelative(e.ServerHome, cwd),
			ResolveLocation:       func(_ context.Context, cwd string) string { return homeRelative(e.ServerHome, cwd) },
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

// pairingURLs lists the URLs that reach the attached server.
func (e Environment) pairingURLs() []string {
	if e.serverURL != "" {
		return []string{e.serverURL}
	}
	return homeServedURLs(e.serverPaths, nil)
}

// errRemoteLogin reports provider login while attached to a URL server.
var errRemoteLogin = errors.New("provider login is not available while attached to a server by URL; log in on the server's machine with `kit`")

// The provider logins of a URL server, which are unavailable.
type (
	remoteDeviceLogin  struct{}
	remoteBrowserLogin struct{}
	remoteAPIKeyLogin  struct{}
)

func (remoteDeviceLogin) Login(context.Context, func(auth.OpenAICodexDeviceInstructions) error) error {
	return errRemoteLogin
}

func (remoteBrowserLogin) Login(context.Context, <-chan string, func(auth.AnthropicLoginInstructions) error) error {
	return errRemoteLogin
}

func (remoteAPIKeyLogin) Login(context.Context, string, string) error { return errRemoteLogin }

// serverCWD chooses the working directory of a new session on a URL server:
// the requested or shell directory when the server shares this machine,
// otherwise the requested directory on the server, or its home by default.
func serverCWD(info kit.ServerInfo, server, requested string) (string, error) {
	if info.SameMachine {
		return resolveCWD(requested)
	}
	if requested == "" || requested == "~" || strings.HasPrefix(requested, "~/") {
		if info.Home == "" {
			return "", fmt.Errorf("server %q did not report its home directory; pass --cwd with an absolute path on that machine", server)
		}
		requested = info.Home + strings.TrimPrefix(requested, "~")
	}
	if !path.IsAbs(requested) {
		return "", usagef("--cwd %q must be an absolute path on server %q, or start with ~/", requested, server)
	}
	return path.Clean(requested), nil
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

// PickerFailure reports a session or server picker that could not run and
// returns the exit code.
func PickerFailure(ctx context.Context, failure string, stderr io.Writer) int {
	if ctx.Err() != nil {
		return 130
	}
	fmt.Fprintf(stderr, "kit: picker: %s\n", failure)
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
