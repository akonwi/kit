package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
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
	"github.com/akonwi/kit/internal/devices"
	"github.com/akonwi/kit/internal/githubpr"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/mcpconfig"
	"github.com/akonwi/kit/internal/mcpruntime"
	"github.com/akonwi/kit/internal/pairing"
	"github.com/akonwi/kit/internal/peer"
	"github.com/akonwi/kit/internal/promptcommands"
	"github.com/akonwi/kit/internal/serveraccess"
	"github.com/akonwi/kit/internal/serveridentity"
	"github.com/akonwi/kit/internal/serverlisten"
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

const (
	instanceHeader = httpapi.InstanceHeader
	protocolHeader = httpapi.ProtocolHeader
)

// ErrAlreadyRunning indicates that another daemon holds the lifetime lock.
var ErrAlreadyRunning = errors.New("local daemon is already running")

// RunOptions configures the internal local daemon process.
type RunOptions struct {
	Paths             apphome.Paths
	Logger            *slog.Logger
	Providers         droids.Providers
	CredentialSources map[string]localdiscovery.CredentialSource
	SystemPrompt      string
	// Listen overrides the server.listen setting for this process. Empty uses
	// the setting, or loopback on an ephemeral port when it is unset.
	Listen string
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
		pullRequests   *githubpr.Cache
		store          *storage.Store
		sessionManager *kitsession.Manager
		subagents      *subagent.Supervisor
		listener       net.Listener
		instanceID     string
		published      bool
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
		if published {
			if err := localdiscovery.Withdraw(paths, instanceID); err != nil {
				logger.Warn("withdraw daemon publication", "error", err)
			}
		}
		if err := lifetimeLock.Unlock(); err != nil {
			logger.Error("unlock local daemon lifetime", "error", err)
		}
	}()

	// Holding the lifetime lock proves no live daemon owns these files. Remove a
	// dead process's publication before writing a new token and registry.
	if err := localdiscovery.Clear(paths); err != nil {
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
	compactionModel := func() string {
		current, _, loadErr := settingsStore.Load()
		if loadErr != nil {
			logger.Warn("load compaction model", "error", loadErr)
			return ""
		}
		return current.CompactionModel
	}
	childBundleBuilder, err := kitsession.NewRuntimeBundleBuilder(kitsession.RuntimeBundleOptions{
		Core: systemPrompt, SkillLoader: skillLoader, PromptCommandLoader: promptCommandLoader,
		Context: &systemprompt.ContextBuilderOptions{Paths: paths},
	})
	if err != nil {
		return fmt.Errorf("create child runtime bundle builder: %w", err)
	}
	childFactory, err := kitsession.NewChildRuntimeFactory(providers, childBundleBuilder, filepath.Join(paths.Droids, "subagents"), modelContextWindow, kitsession.WithChildCompactionModel(compactionModel))
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
		kitsession.WithCompactionModel(compactionModel),
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
	address, err := listenAddress(options.Listen, settingsStore, logger)
	if err != nil {
		return err
	}
	listener, err = address.Listen()
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	baseURL, err := address.LoopbackURL(listener.Addr())
	if err != nil {
		return err
	}
	if address.Exposed() {
		logger.Warn("daemon listens beyond loopback", "address", address.String())
	}

	identity, err := loadServerIdentity(paths)
	if err != nil {
		return err
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
	registry := localdiscovery.Registry{
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

	if err := localdiscovery.Publish(paths, token, registry); err != nil {
		return err
	}
	published = true

	deviceService, err := devices.NewService(ctx, store, devices.Options{Logger: logger})
	if err != nil {
		return err
	}
	pairingRegistry := pairing.NewRegistry(nil)
	allowed := loadAccessSettings(settingsStore, logger)
	purgeContext, stopPurge := context.WithCancel(ctx)
	purgeDone := make(chan struct{})
	go func() {
		defer close(purgeDone)
		deviceService.RunPurge(purgeContext, devicePurgeInterval)
	}()
	defer func() {
		stopPurge()
		<-purgeDone
	}()

	stop := make(chan struct{}, 1)
	streamShutdown, endStreams := context.WithCancel(context.Background())
	defer endStreams()
	handler := newHandler(localHandlerOptions{
		streamShutdown: streamShutdown,
		access:         allowed.policy(),
		registry:       registry,
		identity:       identity,
		token:          token,
		devices:        deviceService,
		pairing:        pairingRegistry,
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
	if err := announcePairing(logger, deviceService, pairingRegistry, address.Exposed() || len(allowed.hosts) > 0, allowed, baseURL); err != nil {
		logger.Warn("issue startup pairing link", "error", err)
	}

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
	// access decides which Host and Origin headers name this daemon.
	access   serveraccess.Policy
	registry localdiscovery.Registry
	identity serverIdentity
	token    string
	// devices and pairing serve device tokens; nil accepts only token.
	devices     *devices.Service
	pairing     *pairing.Registry
	store       *storage.Store
	sessions    sessionService
	attachments attachmentService
	providers   func(context.Context) []string
	requestStop func()
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
		return httpapi.Health{
			InstanceID: options.registry.InstanceID, PID: options.registry.PID,
			KitVersion: options.registry.KitVersion, ProtocolVersion: options.registry.ProtocolVersion,
			DatabaseReady: options.store.Ready(ctx) == nil, Providers: options.providers(ctx),
			ServerID: options.identity.serverID, MachineID: options.identity.machineID, Home: options.identity.home,
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
	if options.pairing == nil {
		options.pairing = pairing.NewRegistry(nil)
	}
	credentials := newAccess(options.token, options.devices, options.pairing)
	if options.devices != nil {
		registerDeviceRoutes(mux, credentials, options.logger)
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !options.access.HostAllowed(request.Host) {
			limitUnauthenticatedRead(writer)
			httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusMisdirectedRequest, httpapi.ErrorInvalidHost, "invalid host", nil))
			return
		}
		if !options.access.OriginAllowed(request.Header.Get("Origin"), request.Host) {
			limitUnauthenticatedRead(writer)
			httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusForbidden, httpapi.ErrorForbidden, "forbidden", nil))
			return
		}
		request = withRequestAddress(request)
		authenticated, release, ok := credentials.authenticate(writer, request)
		if !ok {
			return
		}
		defer release()
		request = authenticated
		// Health without an instance header is how an explicitly addressed
		// client learns the running instance, and a new device redeems a
		// pairing code before it knows any; every other request is bound to
		// the running instance.
		learningInstance := httpapi.GetHealth.OptionalInstance && request.Method == httpapi.GetHealth.Method &&
			request.URL.Path == httpapi.GetHealth.Path && len(request.Header.Values(instanceHeader)) == 0
		if !learningInstance && !credentials.skipsCredential(request) && subtle.ConstantTimeCompare(
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

func providersFromEnvironment(ctx context.Context, paths apphome.Paths) (droids.Providers, map[string]localdiscovery.CredentialSource, error) {
	store := auth.NewStore(paths.Auth)
	openAIKey, openAIKeySource, openAISource := providerAPIKey(store, auth.OpenAIProviderID, os.Getenv("OPENAI_API_KEY"))
	anthropicSource := localdiscovery.CredentialSourceStore
	anthropicConfig := droids.Anthropic{CredentialStore: store, BaseURL: os.Getenv("ANTHROPIC_BASE_URL")}
	if token := os.Getenv("ANTHROPIC_OAUTH_TOKEN"); token != "" {
		anthropicSource = localdiscovery.CredentialSourceEnvironment
		anthropicConfig = droids.Anthropic{Credentials: droids.AnthropicCredentials{AccessToken: token}, BaseURL: os.Getenv("ANTHROPIC_BASE_URL")}
	} else if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		anthropicSource = localdiscovery.CredentialSourceEnvironment
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
	codexSource := localdiscovery.CredentialSourceStore
	if accessToken != "" || refreshToken != "" {
		codexSource = localdiscovery.CredentialSourceEnvironment
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
	return providers, map[string]localdiscovery.CredentialSource{
		auth.OpenAIProviderID:      openAISource,
		auth.AnthropicProviderID:   anthropicSource,
		auth.OpenCodeGoProviderID:  openCodeGoSource,
		auth.OpenAICodexProviderID: codexSource,
	}, nil
}

func providerAPIKey(store *auth.Store, providerID, environmentValue string) (string, droids.APIKeySource, localdiscovery.CredentialSource) {
	if environmentValue != "" {
		return environmentValue, nil, localdiscovery.CredentialSourceEnvironment
	}
	return "", func(ctx context.Context) (string, error) {
		record, err := store.LoadAPIKey(ctx, providerID)
		if err != nil {
			return "", err
		}
		return record.APIKey, nil
	}, localdiscovery.CredentialSourceStore
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

func availableProviderIDs(ctx context.Context, paths apphome.Paths, sources map[string]localdiscovery.CredentialSource) []string {
	set := map[string]bool{}
	store := auth.NewStore(paths.Auth)
	if sources[auth.OpenAIProviderID] == localdiscovery.CredentialSourceEnvironment {
		set[auth.OpenAIProviderID] = os.Getenv("OPENAI_API_KEY") != ""
	} else {
		record, err := store.LoadAPIKey(ctx, auth.OpenAIProviderID)
		set[auth.OpenAIProviderID] = err == nil && record.APIKey != ""
	}
	if sources[auth.OpenCodeGoProviderID] == localdiscovery.CredentialSourceEnvironment {
		set[auth.OpenCodeGoProviderID] = os.Getenv("OPENCODE_API_KEY") != ""
	} else {
		record, err := store.LoadAPIKey(ctx, auth.OpenCodeGoProviderID)
		set[auth.OpenCodeGoProviderID] = err == nil && record.APIKey != ""
	}
	if sources[auth.AnthropicProviderID] == localdiscovery.CredentialSourceEnvironment {
		set[auth.AnthropicProviderID] = os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("ANTHROPIC_OAUTH_TOKEN") != ""
	} else {
		record, err := store.LoadAnthropicCredentials(ctx)
		set[auth.AnthropicProviderID] = err == nil && (record.Credentials.APIKey != "" || record.Credentials.AccessToken != "" || record.Credentials.RefreshToken != "")
	}
	if sources[auth.OpenAICodexProviderID] == localdiscovery.CredentialSourceEnvironment {
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

func cloneCredentialSources(input map[string]localdiscovery.CredentialSource) map[string]localdiscovery.CredentialSource {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]localdiscovery.CredentialSource, len(input))
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

// listenAddress selects the explicit override, then the server.listen
// setting, then the default loopback address. An invalid override fails
// startup; an invalid setting falls back to the default with a warning.
func listenAddress(override string, store *settings.Store, logger *slog.Logger) (serverlisten.Address, error) {
	if override != "" {
		address, err := serverlisten.Parse(override)
		if err != nil {
			return serverlisten.Address{}, err
		}
		return address, nil
	}
	current, warnings, err := store.Load()
	if err != nil {
		logger.Warn("settings unavailable; listening on the default address", "error", err)
		return serverlisten.MustParse(serverlisten.Default), nil
	}
	for _, warning := range warnings {
		if warning.Field == "server" || warning.Field == "server.listen" {
			logger.Warn("ignoring invalid listen setting", "error", warning)
		}
	}
	if current.ServerListen == "" {
		return serverlisten.MustParse(serverlisten.Default), nil
	}
	return serverlisten.MustParse(current.ServerListen), nil
}

// accessPolicy reads the allowed hosts and origins from settings. Settings
// that cannot be read leave only loopback hosts and same-host origins.
// accessSettings are the hosts and origins the daemon accepts beyond loopback.
type accessSettings struct {
	hosts, origins []string
}

func (a accessSettings) policy() serveraccess.Policy {
	return serveraccess.NewPolicy(a.hosts, a.origins)
}

func loadAccessSettings(store *settings.Store, logger *slog.Logger) accessSettings {
	current, warnings, err := store.Load()
	if err != nil {
		logger.Warn("settings unavailable; accepting only loopback hosts", "error", err)
		return accessSettings{}
	}
	for _, warning := range warnings {
		if warning.Field == "server.allowedHosts" || warning.Field == "server.allowedOrigins" {
			logger.Warn("ignoring invalid access setting", "error", warning)
		}
	}
	return accessSettings{hosts: current.ServerAllowedHosts, origins: current.ServerAllowedOrigins}
}

// announcePairing logs a pairing link for every served URL when the daemon
// serves beyond loopback and no device is paired, so a deployment whose only
// visible output is its log can pair its first device.
func announcePairing(logger *slog.Logger, deviceService *devices.Service, registry *pairing.Registry, serving bool, allowed accessSettings, baseURL string) error {
	if !serving || deviceService.Active() > 0 {
		return nil
	}
	code, expires, err := registry.Issue(pairing.StartupLifetime)
	if err != nil {
		return err
	}
	for _, served := range pairing.ServedURLs(allowed.hosts, allowed.origins, baseURL) {
		logger.Info("pair a device with this link", "link", pairing.Link(served, code), "expires", expires.Format(time.RFC3339))
	}
	return nil
}

// serverIdentity is the stable identity reported through health.
type serverIdentity struct {
	serverID, machineID, home string
}

func loadServerIdentity(paths apphome.Paths) (serverIdentity, error) {
	serverID, err := serveridentity.ServerID(paths)
	if err != nil {
		return serverIdentity{}, fmt.Errorf("load server id: %w", err)
	}
	machineID, err := serveridentity.MachineID(paths)
	if err != nil {
		return serverIdentity{}, fmt.Errorf("load machine id: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return serverIdentity{}, fmt.Errorf("resolve server user home: %w", err)
	}
	return serverIdentity{serverID: serverID, machineID: machineID, home: filepath.Clean(home)}, nil
}

func newToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate daemon token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func newInstanceID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate daemon instance id: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
