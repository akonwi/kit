// Package cli is Kit's executable role dispatcher.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/auth"
	kitclient "github.com/akonwi/kit/internal/client"
	"github.com/akonwi/kit/internal/daemon"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"github.com/akonwi/kit/internal/settings"
	kittheme "github.com/akonwi/kit/internal/theme"
	"github.com/akonwi/kit/internal/tui"
)

type interactiveOptions struct {
	SessionID    string
	NewSessionID string
	Temporary    bool
	CWD          string
	Name         string
	Model        string
	Thinking     string
}

func runInteractive(ctx context.Context, options interactiveOptions, _ io.Writer, stderr io.Writer) int {
	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	cwd := ""
	if options.SessionID == "" {
		cwd, err = resolveCWD(options.CWD)
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
	}
	if options.Temporary && options.NewSessionID == "" {
		options.NewSessionID, err = identifier.New("session_")
		if err != nil {
			fmt.Fprintf(stderr, "kit: prepare temporary session: %v\n", err)
			return 1
		}
	}
	manager := daemon.NewManager(paths)
	startContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	_, err = manager.Ensure(startContext)
	cancel()
	if err != nil {
		fmt.Fprintf(stderr, "kit: start local daemon: %v\n", err)
		return 1
	}
	server := kitclient.NewLocalServer(paths)
	if options.SessionID != "" {
		resolveContext, resolveCancel := context.WithTimeout(ctx, 3*time.Second)
		selected, resolveErr := sessionclient.ResolveSession(resolveContext, server, options.SessionID)
		resolveCancel()
		if resolveErr != nil {
			fmt.Fprintf(stderr, "kit: resolve session: %v\n", resolveErr)
			return 1
		}
		options.SessionID = selected.ID
		cwd = selected.CWD
	}

	probeContext, probeCancel := context.WithTimeout(ctx, 3*time.Second)
	registry, health, err := daemon.NewClient(paths).Probe(probeContext)
	probeCancel()
	if err != nil {
		fmt.Fprintf(stderr, "kit: inspect daemon providers: %v\n", err)
		return 1
	}
	if !supportsInteractiveAPIKeyLogin(registry) {
		restartContext, restartCancel := context.WithTimeout(ctx, 12*time.Second)
		err = manager.Restart(restartContext)
		restartCancel()
		if err != nil {
			fmt.Fprintf(stderr, "kit: reload local daemon for provider login: %v\n", err)
			return 1
		}
		probeContext, probeCancel = context.WithTimeout(ctx, 3*time.Second)
		_, health, err = daemon.NewClient(paths).Probe(probeContext)
		probeCancel()
		if err != nil {
			fmt.Fprintf(stderr, "kit: inspect reloaded daemon providers: %v\n", err)
			return 1
		}
	}
	providers, defaultModel := interactiveProviders(health.Providers)
	authenticated := len(providers) > 0
	if options.Model != "" {
		defaultModel = options.Model
		provider, _, _ := strings.Cut(options.Model, "/")
		if !providers[provider] {
			authenticated = false
		}
	}
	defaultThinking := options.Thinking
	if defaultThinking == "" {
		defaultThinking = "medium"
	}
	credentialStore := auth.NewStore(paths.Auth)
	login, err := auth.NewOpenAICodexDeviceLogin(auth.OpenAICodexDeviceLoginOptions{
		Store: credentialStore,
		AcquireSave: func(ctx context.Context) (func() error, error) {
			return manager.AcquireCredentialStoreMutation(ctx, auth.OpenAICodexProviderID)
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "kit: configure OpenAI Codex login: %v\n", err)
		return 1
	}
	browserLogin, err := auth.NewAnthropicOAuthLogin(auth.AnthropicOAuthLoginOptions{
		Store: credentialStore,
		AcquireSave: func(ctx context.Context) (func() error, error) {
			return manager.AcquireCredentialStoreMutation(ctx, auth.AnthropicProviderID)
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "kit: configure Claude subscription login: %v\n", err)
		return 1
	}
	apiKeyLogin, err := auth.NewAPIKeyLogin(auth.APIKeyLoginOptions{
		Store: credentialStore, AcquireSave: manager.AcquireCredentialStoreMutation,
	})
	if err != nil {
		fmt.Fprintf(stderr, "kit: configure API-key login: %v\n", err)
		return 1
	}
	settingsStore, err := settings.NewStore(paths.Settings)
	if err != nil {
		fmt.Fprintf(stderr, "kit: configure settings: %v\n", err)
		return 1
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
	runErr := tui.Run(tui.Options{
		Context:              ctx,
		Server:               server,
		CWD:                  cwd,
		Location:             interactiveLocation(ctx, cwd),
		ResolveLocation:      interactiveLocation,
		DefaultModel:         defaultModel,
		DefaultThinking:      defaultThinking,
		ResumeModelFilter:    options.Model,
		ResumeThinkingFilter: options.Thinking,
		AvailableProviders:   providers,
		Authenticated:        authenticated,
		SessionID:            options.SessionID,
		NewSessionID:         options.NewSessionID,
		NewSessionName:       options.Name,
		TemporarySession:     options.Temporary,
		Login:                login,
		BrowserLogin:         browserLogin,
		APIKeyLogin:          apiKeyLogin,
		ThemeName:            loadedSettings.Theme,
		ThemeDefinition:      themeDefinition,
		ThemeService:         interactiveThemeService{directory: paths.Themes, settings: settingsStore},
		ModelOverrideService: interactiveModelOverrideService{settings: settingsStore},
	})
	if options.Temporary {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		cleanupErr := cleanupTemporarySession(cleanupContext, server, options.NewSessionID)
		cleanupCancel()
		if cleanupErr != nil {
			fmt.Fprintf(stderr, "kit: dispose temporary session: %v\n", cleanupErr)
			if runErr == nil {
				return 1
			}
		}
	}
	if runErr != nil {
		fmt.Fprintf(stderr, "kit: terminal UI: %v\n", runErr)
		return 1
	}
	if ctx.Err() != nil {
		return 130
	}
	return 0
}

func resolveCWD(requested string) (string, error) {
	cwd := requested
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determine current directory: %w", err)
		}
	}
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect working directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", absolute)
	}
	return filepath.Clean(absolute), nil
}

func cleanupTemporarySession(ctx context.Context, server sessionclient.Server, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	err := server.DisposeTemporarySession(ctx, sessionID)
	var apiError *daemon.APIError
	if errors.As(err, &apiError) && apiError.StatusCode == 404 {
		return nil
	}
	return err
}

func supportsInteractiveAPIKeyLogin(registry daemon.Registry) bool {
	return registry.CredentialSources[auth.OpenAIProviderID] != "" &&
		registry.CredentialSources[auth.AnthropicProviderID] != ""
}

func interactiveProviders(providerIDs []string) (map[string]bool, string) {
	providers := make(map[string]bool, len(providerIDs))
	for _, providerID := range providerIDs {
		providers[providerID] = true
	}
	switch {
	case providers[auth.OpenAICodexProviderID]:
		return providers, "openai-codex/gpt-5.6-sol"
	case providers["openai"]:
		return providers, "openai/gpt-5.6-sol"
	case providers["anthropic"]:
		return providers, "anthropic/claude-sonnet-4-6"
	default:
		return providers, ""
	}
}

func interactiveLocation(_ context.Context, cwd string) string {
	location := cwd
	if home, err := os.UserHomeDir(); err == nil {
		if relative, err := filepath.Rel(home, cwd); err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			location = filepath.Join("~", relative)
		} else if relative == "." {
			location = "~"
		}
	}
	return location
}

func runSessions(ctx context.Context, options interactiveOptions, stdout, stderr io.Writer) int {
	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	startContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	_, err = daemon.NewManager(paths).Ensure(startContext)
	cancel()
	if err != nil {
		fmt.Fprintf(stderr, "kit: start local daemon: %v\n", err)
		return 1
	}
	selected, err := tui.RunSessionPicker(tui.SessionPickerOptions{
		Context: ctx,
		Server:  kitclient.NewLocalServer(paths),
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 130
		}
		fmt.Fprintf(stderr, "kit: session picker: %v\n", err)
		return 1
	}
	if selected == "" {
		return 0
	}
	options.SessionID = selected
	return runInteractive(ctx, options, stdout, stderr)
}

func runPrintOptions(ctx context.Context, options printOptions, stdout, stderr io.Writer) int {
	if options.SessionID == "" {
		cwd, err := resolveCWD(options.CWD)
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		options.CWD = cwd
	}
	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	startContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	_, err = daemon.NewManager(paths).Ensure(startContext)
	cancel()
	if err != nil {
		fmt.Fprintf(stderr, "kit: start local daemon: %v\n", err)
		return 1
	}
	probeContext, probeCancel := context.WithTimeout(ctx, 3*time.Second)
	_, health, probeErr := daemon.NewClient(paths).Probe(probeContext)
	probeCancel()
	if probeErr != nil {
		fmt.Fprintf(stderr, "kit: inspect daemon providers: %v\n", probeErr)
		return 1
	}
	options.AvailableProviders, options.DefaultModel = interactiveProviders(health.Providers)
	return executePrint(ctx, kitclient.NewLocalServer(paths), options, stdout, stderr)
}

type printOptions struct {
	CWD                string
	Name               string
	Model              string
	ThinkingLevel      string
	SessionID          string
	NewSession         bool
	Temporary          bool
	DefaultModel       string
	AvailableProviders map[string]bool
	Prompt             string
}

func printModelAvailable(providers map[string]bool, selector string) bool {
	if providers == nil {
		return true
	}
	provider, _, ok := strings.Cut(selector, "/")
	return ok && providers[provider]
}

func executePrint(
	ctx context.Context,
	client sessionclient.Server,
	options printOptions,
	stdout, stderr io.Writer,
) (code int) {
	sessionID := options.SessionID
	if sessionID != "" {
		selected, err := sessionclient.ResolveSession(ctx, client, sessionID)
		if err != nil {
			fmt.Fprintf(stderr, "kit: resolve session: %v\n", err)
			return 1
		}
		sessionID = selected.ID
	}
	if sessionID == "" {
		if !options.NewSession && !options.Temporary {
			sessions, err := client.ListSessions(ctx, options.CWD)
			if err != nil {
				fmt.Fprintf(stderr, "kit: list sessions: %v\n", err)
				return 1
			}
			for _, candidate := range sessions {
				if printModelAvailable(options.AvailableProviders, candidate.Model) &&
					(options.Model == "" || candidate.Model == options.Model) &&
					(options.ThinkingLevel == "" || candidate.ThinkingLevel == options.ThinkingLevel) {
					sessionID = candidate.ID
					break
				}
			}
		}
		if sessionID == "" {
			model := options.Model
			if model == "" {
				model = options.DefaultModel
			}
			if model == "" {
				fmt.Fprintln(stderr, "kit: --model is required when no authenticated default model is available")
				return 2
			}
			if !printModelAvailable(options.AvailableProviders, model) {
				fmt.Fprintf(stderr, "kit: model provider for %q is not authenticated\n", model)
				return 1
			}
			thinking := options.ThinkingLevel
			if thinking == "" {
				thinking = "medium"
			}
			requestedID := ""
			if options.Temporary {
				var err error
				requestedID, err = identifier.New("session_")
				if err != nil {
					fmt.Fprintf(stderr, "kit: prepare temporary session: %v\n", err)
					return 1
				}
				defer func(temporarySessionID string) {
					cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					if err := cleanupTemporarySession(cleanupContext, client, temporarySessionID); err != nil {
						fmt.Fprintf(stderr, "kit: dispose temporary session: %v\n", err)
						if code == 0 {
							code = 1
						}
					}
					cancel()
				}(requestedID)
			}
			created, err := client.CreateSession(ctx, protocol.CreateSessionInput{
				ID: requestedID, CWD: options.CWD, Name: options.Name, Model: model,
				ThinkingLevel: thinking, Temporary: options.Temporary,
			})
			if err != nil {
				fmt.Fprintf(stderr, "kit: create session: %v\n", err)
				return 1
			}
			sessionID = created.ID
		}
	}

	bound, err := client.Attach(ctx, sessionID)
	if err != nil {
		fmt.Fprintf(stderr, "kit: attach session: %v\n", err)
		return 1
	}
	run, err := bound.StartPrompt(ctx, options.Prompt)
	if err != nil {
		fmt.Fprintf(stderr, "kit: start prompt: %v\n", err)
		return 1
	}
	outcome, err := run.Wait(ctx)
	if err != nil {
		if ctx.Err() != nil {
			abortContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			abortErr := run.Abort(abortContext)
			cancel()
			var apiError *daemon.APIError
			if abortErr != nil && (!errors.As(abortErr, &apiError) || apiError.StatusCode != 409) {
				fmt.Fprintf(stderr, "kit: abort session after interruption: %v\n", abortErr)
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				fmt.Fprintln(stderr, "kit: interrupted")
				return 130
			}
			fmt.Fprintln(stderr, "kit: prompt timed out")
			return 1
		}
		fmt.Fprintf(stderr, "kit: run prompt: %v\n", err)
		return 1
	}
	if outcome.Status != protocol.RunStatusCompleted {
		message := outcome.ErrorMessage
		if message == "" {
			message = "agent run " + string(outcome.Status)
		}
		fmt.Fprintf(stderr, "kit: %s\n", message)
		return 1
	}
	fmt.Fprint(stdout, outcome.Text)
	return 0
}

func runInternalDaemon(ctx context.Context, args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("__daemon", flag.ContinueOnError)
	flags.SetOutput(stderr)
	home := flags.String("home", "", "Kit home directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "kit: __daemon accepts no positional arguments")
		return 2
	}
	paths, err := apphome.Resolve(*home)
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := daemon.Run(ctx, daemon.RunOptions{Paths: paths, Logger: logger}); err != nil {
		if errors.Is(err, daemon.ErrAlreadyRunning) {
			fmt.Fprintln(stderr, "kit: local daemon is already running")
			return 2
		}
		fmt.Fprintf(stderr, "kit: local daemon failed: %v\n", err)
		return 1
	}
	return 0
}

func runDaemonCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: kit daemon <start|status|stop|restart>")
		return 2
	}
	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	manager := daemon.NewManager(paths)

	switch args[0] {
	case "start":
		if len(args) != 1 {
			return daemonUsage(stderr)
		}
		operationContext, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		registry, err := manager.Ensure(operationContext)
		if err != nil {
			fmt.Fprintf(stderr, "kit: start daemon: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "daemon running: pid %d, %s\n", registry.PID, registry.URL)
		return 0
	case "status":
		if len(args) != 1 {
			return daemonUsage(stderr)
		}
		operationContext, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		registry, _, err := manager.Status(operationContext)
		if err != nil {
			fmt.Fprintf(stderr, "daemon unavailable: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "daemon running: pid %d, %s, version %s\n", registry.PID, registry.URL, registry.KitVersion)
		return 0
	case "stop":
		if len(args) != 1 {
			return daemonUsage(stderr)
		}
		operationContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := manager.Stop(operationContext); err != nil {
			fmt.Fprintf(stderr, "kit: stop daemon: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "daemon stopped")
		return 0
	case "restart":
		if len(args) != 1 {
			return daemonUsage(stderr)
		}
		operationContext, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := manager.Stop(operationContext); err != nil {
			fmt.Fprintf(stderr, "kit: stop daemon: %v\n", err)
			return 1
		}
		registry, err := manager.Ensure(operationContext)
		if err != nil {
			fmt.Fprintf(stderr, "kit: restart daemon: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "daemon restarted: pid %d, %s\n", registry.PID, registry.URL)
		return 0
	default:
		return daemonUsage(stderr)
	}
}

func daemonUsage(output io.Writer) int {
	fmt.Fprintln(output, "usage: kit daemon <start|status|stop|restart>")
	return 2
}
