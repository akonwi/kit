package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akonwi/kit/droids"
	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/githubpr"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/mcpconfig"
	"github.com/akonwi/kit/internal/mcpruntime"
	"github.com/akonwi/kit/internal/peer"
	"github.com/akonwi/kit/internal/promptcommands"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/sessiontool"
	"github.com/akonwi/kit/internal/settings"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
	"github.com/akonwi/kit/internal/systemprompt"
	"github.com/akonwi/kit/internal/version"
	"github.com/akonwi/kit/internal/workingdiff"
	"github.com/akonwi/kit/internal/workspace"
	"github.com/gofrs/flock"
)

var ErrAlreadyRunning = errors.New("local daemon is already running")

// RunOptions configures the internal local daemon process.
type RunOptions struct {
	Paths             apphome.Paths
	Logger            *slog.Logger
	Providers         droids.Providers
	CredentialSources map[string]CredentialSource
	SystemPrompt      string
}

// Run serves the authenticated local daemon until cancellation or shutdown.
func Run(ctx context.Context, options RunOptions) error {
	paths := options.Paths
	if err := paths.Ensure(); err != nil {
		return err
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	lifetimeLock := flock.New(paths.ServerLock)
	locked, err := lifetimeLock.TryLock()
	if err != nil {
		return fmt.Errorf("lock local daemon lifetime: %w", err)
	}
	if !locked {
		return ErrAlreadyRunning
	}

	var (
		pullRequests      *githubpr.Cache
		store             *storage.Store
		sessionManager    *kitsession.Manager
		subagents         *subagent.Supervisor
		listener          net.Listener
		instanceID        string
		tokenPublished    bool
		registryPublished bool
	)
	defer func() {
		if listener != nil {
			_ = listener.Close()
		}
		if subagents != nil {
			shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := subagents.Shutdown(shutdownContext); err != nil {
				logger.Error("stop subagent runtimes", "error", err)
			}
			cancel()
		}
		if sessionManager != nil {
			shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := sessionManager.Shutdown(shutdownContext); err != nil {
				logger.Error("stop session runtimes", "error", err)
			}
			cancel()
		}
		if pullRequests != nil {
			pullRequests.Close()
		}
		if store != nil {
			if err := store.Close(); err != nil {
				logger.Error("close database", "error", err)
			}
		}
		if registryPublished {
			removeRegistration(paths, instanceID, logger)
		} else if tokenPublished {
			_ = os.Remove(paths.ServerToken)
		}
		if err := lifetimeLock.Unlock(); err != nil {
			logger.Error("unlock local daemon lifetime", "error", err)
		}
	}()

	// Holding the lifetime lock proves no live daemon owns these files. Remove a
	// dead process's publication before writing a new token and registry.
	if err := clearRegistration(paths); err != nil {
		return err
	}

	store, err = storage.Open(ctx, paths.Database)
	if err != nil {
		return err
	}
	attachmentStore, err := attachment.NewFilesystem(paths.Attachments)
	if err != nil {
		return err
	}
	providers := options.Providers
	customProviders := providers != nil
	credentialSources := cloneCredentialSources(options.CredentialSources)
	if providers == nil {
		providers, credentialSources, err = providersFromEnvironment(ctx, paths)
		if err != nil {
			return err
		}
	}
	systemPrompt := options.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = systemprompt.DefaultCore
	}
	providerAvailability := func(ctx context.Context) []string {
		if customProviders {
			return configuredProviderIDs(providers)
		}
		return availableProviderIDs(ctx, paths, credentialSources)
	}
	skillLoader, err := skills.NewFilesystemLoader(paths)
	if err != nil {
		return fmt.Errorf("create skill loader: %w", err)
	}
	promptCommandLoader, err := promptcommands.NewFilesystemLoader(paths)
	if err != nil {
		return fmt.Errorf("create prompt command loader: %w", err)
	}
	subagentLoader, err := subagent.NewFilesystemLoader(paths)
	if err != nil {
		return fmt.Errorf("create subagent loader: %w", err)
	}
	settingsStore, err := settings.NewStore(paths.Settings)
	if err != nil {
		return fmt.Errorf("create settings store: %w", err)
	}
	// Claude Code command discovery follows the setting at each discovery, so a
	// change applies when a runtime next loads or reloads.
	promptCommandLoader.ReadClaudeCommands = func() bool {
		current, _, loadErr := settingsStore.Load()
		if loadErr != nil {
			logger.Warn("load Claude Code configuration setting", "error", loadErr)
			return true
		}
		return current.ReadClaudeConfigs
	}
	modelContextWindow := func(selector string) int {
		current, _, loadErr := settingsStore.Load()
		if loadErr != nil {
			logger.Warn("load model overrides", "error", loadErr)
			return 0
		}
		return current.ModelOverrides[selector].ContextWindow
	}
	childBundleBuilder, err := kitsession.NewRuntimeBundleBuilder(kitsession.RuntimeBundleOptions{
		Core: systemPrompt, SkillLoader: skillLoader, PromptCommandLoader: promptCommandLoader,
		Context: &systemprompt.ContextBuilderOptions{Paths: paths},
	})
	if err != nil {
		return fmt.Errorf("create child runtime bundle builder: %w", err)
	}
	childFactory, err := kitsession.NewChildRuntimeFactory(providers, childBundleBuilder, filepath.Join(paths.Droids, "subagents"), modelContextWindow)
	if err != nil {
		return fmt.Errorf("create child runtime factory: %w", err)
	}
	subagents, err = subagent.NewSupervisor(store, childFactory, subagent.DefaultLimits(), nil)
	if err != nil {
		return fmt.Errorf("create subagent supervisor: %w", err)
	}
	subagentTools := &subagent.ToolService{
		Supervisor: subagents, Requests: store, Owners: store, Definitions: subagentLoader,
		ResolveConfiguration: func(ctx context.Context, selector, thinking string) (string, string, error) {
			model, err := resolveSubagentModel(providers, selector, providerAvailability(ctx))
			if err != nil {
				return "", "", err
			}
			effectiveThinking, err := kitsession.ResolveCompatibleThinkingLevel(model, thinking)
			if err != nil {
				return "", "", err
			}
			return model.Provider + "/" + model.ID, effectiveThinking, nil
		},
	}
	peerTools := &peer.ToolService{}
	sessionTools := &sessiontool.ToolService{}
	mcpLoader, err := mcpconfig.NewLoader(paths)
	if err != nil {
		return fmt.Errorf("create MCP configuration loader: %w", err)
	}
	// Server processes are bound to the daemon lifetime, so shutdown terminates
	// every process group even when a graceful close does not complete.
	mcpLauncher, err := mcpruntime.NewLauncher(ctx, mcpruntime.WithMCPAuthStore(auth.NewStore(paths.MCPAuth)))
	if err != nil {
		return fmt.Errorf("create MCP launcher: %w", err)
	}
	bundleBuilder, err := kitsession.NewRuntimeBundleBuilder(kitsession.RuntimeBundleOptions{
		Core: systemPrompt, SkillLoader: skillLoader, PromptCommandLoader: promptCommandLoader,
		SubagentLoader: subagentLoader, SubagentToolFactory: subagentTools, PeerToolFactory: peerTools, SessionToolFactory: sessionTools,
		AttachmentStore:     attachmentStore,
		PresentImageEnabled: func(kitsession.SessionRecord) bool { return true },
		InspectImageEnabled: inspectImageEnabled(providers),
		Context:             &systemprompt.ContextBuilderOptions{Paths: paths},
		MCPLoader:           mcpLoader,
		MCPLauncher:         mcpLauncher,
	})
	if err != nil {
		return fmt.Errorf("create runtime bundle builder: %w", err)
	}
	workspaceService := workspace.NewService()
	diffService, err := workingdiff.NewService(workspaceService)
	if err != nil {
		return fmt.Errorf("configure working-tree diff service: %w", err)
	}
	annotationService, err := kitannotation.NewService(store, annotationWorkspaceReader{service: workspaceService}, annotationDiffReader{service: diffService})
	if err != nil {
		return fmt.Errorf("configure annotation service: %w", err)
	}
	pullRequests = githubpr.NewCache(ctx)
	sessionManager, err = kitsession.NewManager(
		store, providers, bundleBuilder,
		kitsession.WithDroidStoreDirectory(paths.Droids),
		kitsession.WithAttachmentStore(attachmentStore),
		kitsession.WithAnnotationService(annotationService),
		kitsession.WithModelContextWindow(modelContextWindow),
		kitsession.WithPluginHostFactory(pluginHostFactory(paths, pullRequests, logger)),
		kitsession.WithPluginSubagentCatalogRegistry(subagentTools),
		kitsession.WithAutomaticNaming(),
	)
	if err != nil {
		return fmt.Errorf("create session manager: %w", err)
	}
	childFactory.PluginInterceptors = sessionManager.PluginInterceptors
	childFactory.MCPTools = sessionManager.MCPTools
	childFactory.SiblingTools = &subagent.SiblingToolService{
		Repository: store, Supervisor: subagents,
		ResolveRecipient: subagentTools.ResolveConfiguredRecipient,
	}
	annotationService.SetObserver(sessionManager)
	peerTools.Service = sessionManager
	sessionTools.Service = modelSessionService{
		manager: sessionManager, providers: providers, availableProviders: providerAvailability,
		configuredDefault: func() (string, error) {
			current, _, loadErr := settingsStore.Load()
			if loadErr != nil {
				return "", fmt.Errorf("load default model: %w", loadErr)
			}
			return current.DefaultModel, nil
		},
	}
	if err := subagents.SetEventSink(sessionManager); err != nil {
		return fmt.Errorf("connect subagent event sink: %w", err)
	}
	if err := sessionManager.SetSubagentOwnerDeletion(subagents); err != nil {
		return fmt.Errorf("connect subagent owner deletion: %w", err)
	}
	if _, err := subagents.Start(ctx); err != nil {
		return fmt.Errorf("recover and start subagent supervisor: %w", err)
	}
	if err := sessionManager.StartSubagentMailbox(); err != nil {
		return fmt.Errorf("start subagent mailbox delivery: %w", err)
	}
	if err := sessionManager.StartPeerQueries(ctx); err != nil {
		return fmt.Errorf("start peer query delivery: %w", err)
	}
	listener, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on loopback: %w", err)
	}

	token, err := newToken()
	if err != nil {
		return err
	}
	instanceID, err = newInstanceID()
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC()
	baseURL := "http://" + listener.Addr().String()
	registry := Registry{
		RegistryVersion:   version.LocalRegistryVersion,
		ProtocolVersion:   version.SessionProtocolVersion,
		KitVersion:        version.Version,
		Commit:            version.Commit,
		PID:               os.Getpid(),
		InstanceID:        instanceID,
		URL:               baseURL,
		StartedAt:         startedAt,
		CredentialSources: credentialSources,
	}

	if err := writePrivateFile(paths.ServerToken, []byte(token+"\n")); err != nil {
		return err
	}
	tokenPublished = true
	// The registry is the publication marker and is written only after its token
	// exists. Clients never discover a mixed token/registry generation.
	if err := writeRegistry(paths, registry); err != nil {
		return fmt.Errorf("publish daemon registry: %w", err)
	}
	registryPublished = true

	stop := make(chan struct{}, 1)
	streamShutdown, endStreams := context.WithCancel(context.Background())
	defer endStreams()
	handler := newHandler(localHandlerOptions{
		baseURL:        baseURL,
		streamShutdown: streamShutdown,
		expectedHost:   listener.Addr().String(),
		registry:       registry,
		token:          token,
		store:          store,
		logger:         logger,
		sessions: runtimeSessionService{
			manager: sessionManager, availableProviders: providerAvailability, modelContextWindow: modelContextWindow, fileIndexes: newSessionFileIndexCache(),
			workspaces: workspaceService, diffs: diffService, annotations: annotationService, annotationCursorKey: []byte(token), subagents: subagents, subagentTools: subagentTools, attachments: attachmentStore,
		},
		attachments: runtimeAttachmentService{manager: sessionManager, store: attachmentStore},
		providers:   providerAvailability,
		requestStop: func() {
			select {
			case stop <- struct{}{}:
			default:
			}
		},
	})
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	// Shutdown waits for active requests, and attached clients hold event
	// streams open indefinitely. End those streams as soon as shutdown begins.
	server.RegisterOnShutdown(endStreams)

	serveResult := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveResult <- err
	}()
	logger.Info("local daemon ready", "url", baseURL, "instance", instanceID)

	var runErr error
	select {
	case <-ctx.Done():
		runErr = context.Cause(ctx)
		if errors.Is(runErr, context.Canceled) {
			runErr = nil
		}
	case <-stop:
	case err := <-serveResult:
		return err
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		_ = server.Close()
		if runErr == nil {
			runErr = fmt.Errorf("shut down local daemon: %w", err)
		}
	}
	select {
	case err := <-serveResult:
		if runErr == nil {
			runErr = err
		}
	default:
	}
	return runErr
}

type localHandlerOptions struct {
	baseURL      string
	expectedHost string
	registry     Registry
	token        string
	store        *storage.Store
	sessions     sessionService
	attachments  attachmentService
	providers    func(context.Context) []string
	requestStop  func()
	// streamShutdown ends open event streams when it is done.
	streamShutdown context.Context
	// logger receives causes of failed requests; nil disables reporting.
	logger *slog.Logger
}

func newHandler(options localHandlerOptions) http.Handler {
	mux := http.NewServeMux()
	lifecycleOptions := httpapi.ServeOptions{WriteError: func(w http.ResponseWriter, err error) {
		apiError, ok := err.(*httpapi.APIError)
		if !ok {
			apiError = httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal error", nil)
		}
		httpapi.WriteError(w, apiError)
	}}
	httpapi.Handle(mux, lifecycleOptions, httpapi.GetHealth, func(requestContext context.Context, _ httpapi.ServerPath, _ httpapi.NoBody) (httpapi.Health, error) {
		ctx, cancel := context.WithTimeout(requestContext, 2*time.Second)
		defer cancel()
		return Health{
			InstanceID: options.registry.InstanceID, PID: options.registry.PID,
			KitVersion: options.registry.KitVersion, ProtocolVersion: options.registry.ProtocolVersion,
			DatabaseReady: options.store.Ready(ctx) == nil, Providers: options.providers(ctx),
		}, nil
	})
	httpapi.Handle(mux, lifecycleOptions, httpapi.Shutdown, func(context.Context, httpapi.ServerPath, httpapi.NoBody) (httpapi.ShutdownResult, error) {
		options.requestStop()
		return httpapi.ShutdownResult{Stopping: true}, nil
	})
	if options.sessions != nil {
		registerSessionRoutesWithStreamShutdown(mux, options.sessions, options.streamShutdown)
	}
	if options.attachments != nil {
		registerAttachmentRoutes(mux, options.attachments)
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != options.expectedHost {
			httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusMisdirectedRequest, httpapi.ErrorInvalidHost, "invalid host", nil))
			return
		}
		if origin := request.Header.Get("Origin"); origin != "" && origin != options.baseURL {
			httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusForbidden, httpapi.ErrorForbidden, "forbidden", nil))
			return
		}
		if subtle.ConstantTimeCompare(
			[]byte(request.Header.Get("Authorization")),
			[]byte("Bearer "+options.token),
		) != 1 {
			writer.Header().Set("WWW-Authenticate", "Bearer")
			httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusUnauthorized, httpapi.ErrorUnauthorized, "unauthorized", nil))
			return
		}
		if subtle.ConstantTimeCompare(
			[]byte(request.Header.Get(instanceHeader)),
			[]byte(options.registry.InstanceID),
		) != 1 {
			httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorInstanceMismatch, "daemon instance mismatch", nil))
			return
		}
		if (strings.HasPrefix(request.URL.Path, "/v1/sessions") || strings.HasPrefix(request.URL.Path, "/v1/models")) &&
			request.Header.Get(protocolHeader) != strconv.Itoa(version.SessionProtocolVersion) {
			httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusUpgradeRequired, httpapi.ErrorProtocolMismatch, "session protocol mismatch", nil))
			return
		}
		mux.ServeHTTP(withRequestErrorReporting(writer, request, options.logger), request)
	})
}

// inspectImageEnabled offers inspect_image to sessions whose model accepts
// images in tool results, where the tool returns the image it inspects.
func inspectImageEnabled(providers droids.Providers) func(kitsession.SessionRecord) bool {
	return func(record kitsession.SessionRecord) bool {
		model, err := providers.Resolve(record.ModelProvider + "/" + record.ModelID)
		return err == nil && model.ImagePolicy().Accepts(droids.ImagePlacementToolResult)
	}
}

// promptCacheRetention reads the prompt cache retention setting for each
// request, so a change applies to the next request. An unreadable settings
// file selects the default.
func promptCacheRetention(paths apphome.Paths) func() droids.PromptCacheRetention {
	store, err := settings.NewStore(paths.Settings)
	return func() droids.PromptCacheRetention {
		if err != nil {
			return droids.PromptCacheShort
		}
		current, _, loadErr := store.Load()
		if loadErr == nil && current.PromptCacheRetention == settings.PromptCacheLong {
			return droids.PromptCacheLong
		}
		return droids.PromptCacheShort
	}
}

func providersFromEnvironment(ctx context.Context, paths apphome.Paths) (droids.Providers, map[string]CredentialSource, error) {
	store := auth.NewStore(paths.Auth)
	openAIKey, openAIKeySource, openAISource := providerAPIKey(store, auth.OpenAIProviderID, os.Getenv("OPENAI_API_KEY"))
	anthropicSource := CredentialSourceStore
	anthropicConfig := droids.Anthropic{CredentialStore: store, BaseURL: os.Getenv("ANTHROPIC_BASE_URL")}
	if token := os.Getenv("ANTHROPIC_OAUTH_TOKEN"); token != "" {
		anthropicSource = CredentialSourceEnvironment
		anthropicConfig = droids.Anthropic{Credentials: droids.AnthropicCredentials{AccessToken: token}, BaseURL: os.Getenv("ANTHROPIC_BASE_URL")}
	} else if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		anthropicSource = CredentialSourceEnvironment
		anthropicConfig = droids.Anthropic{APIKey: key, BaseURL: os.Getenv("ANTHROPIC_BASE_URL")}
	}
	anthropicConfig.PromptCacheRetention = promptCacheRetention(paths)
	openCodeGoKey, openCodeGoKeySource, openCodeGoSource := providerAPIKey(store, auth.OpenCodeGoProviderID, os.Getenv("OPENCODE_API_KEY"))
	configs := []droids.ProviderConfig{
		droids.OpenAI{APIKey: openAIKey, APIKeySource: openAIKeySource, BaseURL: os.Getenv("OPENAI_BASE_URL"), PromptCacheRetention: promptCacheRetention(paths)},
		anthropicConfig,
		droids.OpenCodeGo{APIKey: openCodeGoKey, APIKeySource: openCodeGoKeySource},
	}
	accessToken := os.Getenv("OPENAI_CODEX_ACCESS_TOKEN")
	refreshToken := os.Getenv("OPENAI_CODEX_REFRESH_TOKEN")
	codexSource := CredentialSourceStore
	if accessToken != "" || refreshToken != "" {
		codexSource = CredentialSourceEnvironment
		credentials := droids.OpenAICodexCredentials{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			IDToken:      os.Getenv("OPENAI_CODEX_ID_TOKEN"),
			AccountID:    os.Getenv("OPENAI_CODEX_ACCOUNT_ID"),
		}
		if raw := os.Getenv("OPENAI_CODEX_FEDRAMP"); raw != "" {
			fedRAMP, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, nil, fmt.Errorf("parse OPENAI_CODEX_FEDRAMP: %w", err)
			}
			credentials.FedRAMP = fedRAMP
		}
		if raw := os.Getenv("OPENAI_CODEX_EXPIRES_AT"); raw != "" {
			expiresAt, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return nil, nil, fmt.Errorf("parse OPENAI_CODEX_EXPIRES_AT as Unix milliseconds: %w", err)
			}
			credentials.ExpiresAt = time.UnixMilli(expiresAt)
		}
		configs = append(configs, droids.OpenAICodex{Credentials: credentials, Originator: "kit"})
	} else {
		configs = append(configs, droids.OpenAICodex{CredentialStore: store, Originator: "kit"})
	}
	providers, err := droids.NewProvidersWithOptions(ctx, droids.RegistryOptions{
		ModelCatalogCache: droids.FileModelCatalogCache{Path: paths.ModelCatalogCache},
	}, configs...)
	if err != nil {
		return nil, nil, fmt.Errorf("configure droids providers: %w", err)
	}
	return providers, map[string]CredentialSource{
		auth.OpenAIProviderID:      openAISource,
		auth.AnthropicProviderID:   anthropicSource,
		auth.OpenCodeGoProviderID:  openCodeGoSource,
		auth.OpenAICodexProviderID: codexSource,
	}, nil
}

func providerAPIKey(store *auth.Store, providerID, environmentValue string) (string, droids.APIKeySource, CredentialSource) {
	if environmentValue != "" {
		return environmentValue, nil, CredentialSourceEnvironment
	}
	return "", func(ctx context.Context) (string, error) {
		record, err := store.LoadAPIKey(ctx, providerID)
		if err != nil {
			return "", err
		}
		return record.APIKey, nil
	}, CredentialSourceStore
}

func configuredProviderIDs(providers droids.Providers) []string {
	set := map[string]bool{}
	for _, model := range providers.Models() {
		if model.Provider != "" {
			set[model.Provider] = true
		}
	}
	return sortedProviderIDs(set)
}

func availableProviderIDs(ctx context.Context, paths apphome.Paths, sources map[string]CredentialSource) []string {
	set := map[string]bool{}
	store := auth.NewStore(paths.Auth)
	if sources[auth.OpenAIProviderID] == CredentialSourceEnvironment {
		set[auth.OpenAIProviderID] = os.Getenv("OPENAI_API_KEY") != ""
	} else {
		record, err := store.LoadAPIKey(ctx, auth.OpenAIProviderID)
		set[auth.OpenAIProviderID] = err == nil && record.APIKey != ""
	}
	if sources[auth.OpenCodeGoProviderID] == CredentialSourceEnvironment {
		set[auth.OpenCodeGoProviderID] = os.Getenv("OPENCODE_API_KEY") != ""
	} else {
		record, err := store.LoadAPIKey(ctx, auth.OpenCodeGoProviderID)
		set[auth.OpenCodeGoProviderID] = err == nil && record.APIKey != ""
	}
	if sources[auth.AnthropicProviderID] == CredentialSourceEnvironment {
		set[auth.AnthropicProviderID] = os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("ANTHROPIC_OAUTH_TOKEN") != ""
	} else {
		record, err := store.LoadAnthropicCredentials(ctx)
		set[auth.AnthropicProviderID] = err == nil && (record.Credentials.APIKey != "" || record.Credentials.AccessToken != "" || record.Credentials.RefreshToken != "")
	}
	if sources[auth.OpenAICodexProviderID] == CredentialSourceEnvironment {
		set[auth.OpenAICodexProviderID] = true
	} else if record, err := store.LoadOpenAICodexCredentials(ctx); err == nil && record.Revision != "" {
		set[auth.OpenAICodexProviderID] = true
	}
	return sortedProviderIDs(set)
}

func sortedProviderIDs(set map[string]bool) []string {
	result := make([]string, 0, len(set))
	for provider, available := range set {
		if available {
			result = append(result, provider)
		}
	}
	sort.Strings(result)
	return result
}

func cloneCredentialSources(input map[string]CredentialSource) map[string]CredentialSource {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]CredentialSource, len(input))
	for providerID, source := range input {
		cloned[providerID] = source
	}
	return cloned
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func clearRegistration(paths apphome.Paths) error {
	for _, path := range []string{paths.ServerRegistry, paths.ServerToken} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale daemon registration %q: %w", path, err)
		}
	}
	return nil
}

func removeRegistration(paths apphome.Paths, instanceID string, logger *slog.Logger) {
	registry, err := LoadRegistry(paths)
	if err == nil && registry.InstanceID != instanceID {
		return
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Warn("daemon registry became unreadable during cleanup", "error", err)
	}
	// Remove the registry first so clients cannot discover a token while it is
	// being deleted. The lifetime lock still prevents a replacement daemon.
	for _, path := range []string{paths.ServerRegistry, paths.ServerToken} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("remove daemon registration", "path", path, "error", err)
		}
	}
}
