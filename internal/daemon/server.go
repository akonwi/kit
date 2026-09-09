package daemon

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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/promptcommands"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/systemprompt"
	"github.com/akonwi/kit/internal/version"
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
		store             *storage.Store
		sessionManager    *kitsession.Manager
		listener          net.Listener
		instanceID        string
		tokenPublished    bool
		registryPublished bool
	)
	defer func() {
		if listener != nil {
			_ = listener.Close()
		}
		if sessionManager != nil {
			shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := sessionManager.Shutdown(shutdownContext); err != nil {
				logger.Error("stop session runtimes", "error", err)
			}
			cancel()
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
	bundleBuilder, err := kitsession.NewRuntimeBundleBuilder(kitsession.RuntimeBundleOptions{
		Core: systemPrompt, SkillLoader: skillLoader, PromptCommandLoader: promptCommandLoader,
		Context: &systemprompt.ContextBuilderOptions{Paths: paths},
	})
	if err != nil {
		return fmt.Errorf("create runtime bundle builder: %w", err)
	}
	sessionManager, err = kitsession.NewManager(
		store, providers, bundleBuilder,
		kitsession.WithDroidStoreDirectory(paths.Droids),
	)
	if err != nil {
		return fmt.Errorf("create session manager: %w", err)
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
	handler := newHandler(localHandlerOptions{
		baseURL:      baseURL,
		expectedHost: listener.Addr().String(),
		registry:     registry,
		token:        token,
		store:        store,
		sessions:     runtimeSessionService{manager: sessionManager, availableProviders: providerAvailability},
		providers:    providerAvailability,
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
	providers    func(context.Context) []string
	requestStop  func()
}

func newHandler(options localHandlerOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		databaseReady := options.store.Ready(ctx) == nil
		writeJSON(writer, http.StatusOK, Health{
			InstanceID:      options.registry.InstanceID,
			PID:             options.registry.PID,
			KitVersion:      options.registry.KitVersion,
			ProtocolVersion: options.registry.ProtocolVersion,
			DatabaseReady:   databaseReady,
			Providers:       options.providers(ctx),
		})
	})
	mux.HandleFunc("POST /v1/shutdown", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusAccepted, map[string]bool{"stopping": true})
		options.requestStop()
	})
	if options.sessions != nil {
		registerSessionRoutes(mux, options.sessions)
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != options.expectedHost {
			http.Error(writer, "invalid host", http.StatusMisdirectedRequest)
			return
		}
		if origin := request.Header.Get("Origin"); origin != "" && origin != options.baseURL {
			http.Error(writer, "invalid origin", http.StatusForbidden)
			return
		}
		if subtle.ConstantTimeCompare(
			[]byte(request.Header.Get("Authorization")),
			[]byte("Bearer "+options.token),
		) != 1 {
			writer.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		if subtle.ConstantTimeCompare(
			[]byte(request.Header.Get(instanceHeader)),
			[]byte(options.registry.InstanceID),
		) != 1 {
			http.Error(writer, "daemon instance mismatch", http.StatusConflict)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/v1/sessions") &&
			request.Header.Get(protocolHeader) != strconv.Itoa(version.SessionProtocolVersion) {
			http.Error(writer, "session protocol mismatch", http.StatusUpgradeRequired)
			return
		}
		mux.ServeHTTP(writer, request)
	})
}

func providersFromEnvironment(_ context.Context, paths apphome.Paths) (droids.Providers, map[string]CredentialSource, error) {
	store := auth.NewStore(paths.Auth)
	openAIKey, openAIKeySource, openAISource := providerAPIKey(store, auth.OpenAIProviderID, os.Getenv("OPENAI_API_KEY"))
	anthropicKey, anthropicKeySource, anthropicSource := providerAPIKey(store, auth.AnthropicProviderID, os.Getenv("ANTHROPIC_API_KEY"))
	configs := []droids.ProviderConfig{
		droids.OpenAI{APIKey: openAIKey, APIKeySource: openAIKeySource, BaseURL: os.Getenv("OPENAI_BASE_URL")},
		droids.Anthropic{APIKey: anthropicKey, APIKeySource: anthropicKeySource, BaseURL: os.Getenv("ANTHROPIC_BASE_URL")},
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
	providers, err := droids.NewProviders(configs...)
	if err != nil {
		return nil, nil, fmt.Errorf("configure droids providers: %w", err)
	}
	return providers, map[string]CredentialSource{
		auth.OpenAIProviderID:      openAISource,
		auth.AnthropicProviderID:   anthropicSource,
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
	for _, providerID := range []string{auth.OpenAIProviderID, auth.AnthropicProviderID} {
		if sources[providerID] == CredentialSourceEnvironment {
			environmentName := "OPENAI_API_KEY"
			if providerID == auth.AnthropicProviderID {
				environmentName = "ANTHROPIC_API_KEY"
			}
			set[providerID] = os.Getenv(environmentName) != ""
			continue
		}
		record, err := store.LoadAPIKey(ctx, providerID)
		set[providerID] = err == nil && record.APIKey != ""
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
