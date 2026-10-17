package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/daemonctl"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/serveridentity"
)

func TestProvidersFromEnvironmentIncludesOpenAICodex(t *testing.T) {
	for _, name := range []string{
		"OPENAI_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_BASE_URL", "OPENCODE_API_KEY",
		"OPENAI_CODEX_REFRESH_TOKEN", "OPENAI_CODEX_ID_TOKEN", "OPENAI_CODEX_FEDRAMP",
		"OPENAI_CODEX_EXPIRES_AT",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("OPENAI_CODEX_ACCESS_TOKEN", "test-access")
	t.Setenv("OPENAI_CODEX_ACCOUNT_ID", "test-account")
	t.Setenv("OPENCODE_API_KEY", "test-opencode")

	providers, sources, err := providersFromEnvironment(context.Background(), apphome.FromHome(filepath.Join(t.TempDir(), "kit")), nil)
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("openai-codex/gpt-5.6-sol")
	if !ok || model.Provider != "openai-codex" {
		t.Fatalf("Codex model = %#v, %v", model, ok)
	}
	if source := sources[auth.OpenAICodexProviderID]; source != localdiscovery.CredentialSourceEnvironment {
		t.Fatalf("Codex credential source = %q", source)
	}
	for _, providerID := range []string{auth.OpenCodeProviderID, auth.OpenCodeGoProviderID} {
		if source := sources[providerID]; source != localdiscovery.CredentialSourceEnvironment {
			t.Fatalf("credential source for %s = %q", providerID, source)
		}
	}
	available := availableProviderIDs(t.Context(), apphome.FromHome(filepath.Join(t.TempDir(), "kit")), sources)
	if !slices.Contains(available, auth.OpenCodeProviderID) || !slices.Contains(available, auth.OpenCodeGoProviderID) {
		t.Fatalf("OpenCode providers unavailable with OPENCODE_API_KEY: %v", available)
	}
	ids := droids.ProviderIDs(providers)
	if slices.Index(ids, auth.OpenCodeGoProviderID) > slices.Index(ids, auth.OpenCodeProviderID) {
		t.Fatalf("provider order reroutes bare selectors to Zen: %v", ids)
	}
}

func TestProvidersFromEnvironmentUsesCredentialStoreByDefault(t *testing.T) {
	for _, name := range []string{
		"OPENAI_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_BASE_URL", "OPENCODE_API_KEY",
		"OPENAI_CODEX_ACCESS_TOKEN", "OPENAI_CODEX_REFRESH_TOKEN", "OPENAI_CODEX_ID_TOKEN",
		"OPENAI_CODEX_ACCOUNT_ID", "OPENAI_CODEX_FEDRAMP", "OPENAI_CODEX_EXPIRES_AT",
	} {
		t.Setenv(name, "")
	}
	providers, sources, err := providersFromEnvironment(context.Background(), apphome.FromHome(filepath.Join(t.TempDir(), "kit")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := providers.Model("openai-codex/gpt-5.6-sol"); !ok {
		t.Fatal("stored-credential Codex provider was not registered")
	}
	for _, providerID := range []string{auth.OpenAIProviderID, auth.AnthropicProviderID, auth.OpenCodeProviderID, auth.OpenCodeGoProviderID, auth.OpenAICodexProviderID} {
		if source := sources[providerID]; source != localdiscovery.CredentialSourceStore {
			t.Fatalf("credential source for %s = %q", providerID, source)
		}
	}
}

func TestProvidersFromEnvironmentLoadsStoredAPIKeys(t *testing.T) {
	for _, name := range []string{
		"OPENAI_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_BASE_URL", "OPENCODE_API_KEY",
		"OPENAI_CODEX_ACCESS_TOKEN", "OPENAI_CODEX_REFRESH_TOKEN", "OPENAI_CODEX_ID_TOKEN",
		"OPENAI_CODEX_ACCOUNT_ID", "OPENAI_CODEX_FEDRAMP", "OPENAI_CODEX_EXPIRES_AT",
	} {
		t.Setenv(name, "")
	}
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	store := auth.NewStore(paths.Auth)
	if err := store.ReplaceAPIKey(context.Background(), auth.OpenAIProviderID, "openai-secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAPIKey(context.Background(), auth.AnthropicProviderID, "anthropic-secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAPIKey(context.Background(), auth.OpenCodeProviderID, "opencode-secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAPIKey(context.Background(), auth.OpenCodeGoProviderID, "opencode-go-secret"); err != nil {
		t.Fatal(err)
	}
	_, sources, err := providersFromEnvironment(context.Background(), paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := availableProviderIDs(context.Background(), paths, sources)
	if want := []string{auth.AnthropicProviderID, auth.OpenAIProviderID, auth.OpenCodeProviderID, auth.OpenCodeGoProviderID}; !slices.Equal(got, want) {
		t.Fatalf("available providers = %v, want %v", got, want)
	}
}

func TestProvidersFromEnvironmentDiscoversStoredAnthropicOAuth(t *testing.T) {
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_BASE_URL"} {
		t.Setenv(name, "")
	}
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	store := auth.NewStore(paths.Auth)
	if err := store.ReplaceAnthropicOAuthCredentials(context.Background(), droids.AnthropicCredentials{
		AccessToken: "oauth-access", RefreshToken: "oauth-refresh", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	_, sources, err := providersFromEnvironment(context.Background(), paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := availableProviderIDs(context.Background(), paths, sources); !slices.Contains(got, auth.AnthropicProviderID) {
		t.Fatalf("available providers = %v", got)
	}
}

func TestProvidersFromEnvironmentRejectsInvalidCodexMetadata(t *testing.T) {
	t.Setenv("OPENAI_CODEX_ACCESS_TOKEN", "test-access")
	t.Setenv("OPENAI_CODEX_FEDRAMP", "not-a-bool")
	if _, _, err := providersFromEnvironment(context.Background(), apphome.FromHome(filepath.Join(t.TempDir(), "kit")), nil); err == nil {
		t.Fatal("invalid OPENAI_CODEX_FEDRAMP was accepted")
	}
}

func TestRunServesHealthAndStopsGracefully(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, RunOptions{
			Paths:  paths,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()

	client := daemonctl.NewClient(paths)
	probeContext, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	var registry localdiscovery.Registry
	var health daemonctl.Health
	for {
		var err error
		registry, health, err = client.Probe(probeContext)
		if err == nil {
			break
		}
		select {
		case <-probeContext.Done():
			t.Fatalf("daemon did not become ready: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if registry.PID != os.Getpid() {
		t.Errorf("PID = %d, want in-process test PID %d", registry.PID, os.Getpid())
	}
	serverID, err := serveridentity.ServerID(paths)
	if err != nil {
		t.Fatal(err)
	}
	machineID, err := serveridentity.MachineID(paths)
	if err != nil {
		t.Fatal(err)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if health.ServerID != serverID || health.MachineID != machineID || health.Home != filepath.Clean(userHome) {
		t.Errorf("health identity = (%q, %q, %q), want persisted server %q, machine %q, home %q",
			health.ServerID, health.MachineID, health.Home, serverID, machineID, filepath.Clean(userHome))
	}
	info, err := os.Stat(paths.ServerToken)
	if err != nil {
		t.Fatalf("stat token: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token mode = %o, want 600", info.Mode().Perm())
	}

	token, err := os.ReadFile(paths.ServerToken)
	if err != nil {
		t.Fatal(err)
	}
	healthWithInstance := func(instance *string) (int, httpapi.Health) {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, registry.URL+"/v1/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		if instance != nil {
			request.Header.Set(httpapi.InstanceHeader, *instance)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var health httpapi.Health
		_ = json.NewDecoder(response.Body).Decode(&health)
		return response.StatusCode, health
	}
	if status, learned := healthWithInstance(nil); status != http.StatusOK || learned.InstanceID != registry.InstanceID {
		t.Errorf("health without an instance header = %d %q, want 200 with instance %q", status, learned.InstanceID, registry.InstanceID)
	}
	stale := "instance_stale"
	if status, _ := healthWithInstance(&stale); status != http.StatusConflict {
		t.Errorf("health with a stale instance header = %d, want %d", status, http.StatusConflict)
	}

	response, err := http.Get(registry.URL + "/v1/health")
	if err != nil {
		t.Fatalf("unauthenticated health request: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}

	stopContext, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := daemonctl.NewManager(paths).Stop(stopContext); err != nil {
		t.Fatalf("Manager.Stop() error = %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-stopContext.Done():
		t.Fatal("daemon did not stop")
	}
	if _, err := os.Stat(paths.ServerRegistry); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("registry still exists after shutdown: %v", err)
	}
	if _, err := os.Stat(paths.ServerToken); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("token still exists after shutdown: %v", err)
	}
}

func TestRunListensOnTheRequestedAddress(t *testing.T) {
	t.Parallel()
	reserved, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reserved.Addr().(*net.TCPAddr).Port
	_ = reserved.Close()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, RunOptions{
			Paths: paths, Listen: fmt.Sprintf("127.0.0.1:%d", port),
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()

	client := daemonctl.NewClient(paths)
	probeContext, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	var registry localdiscovery.Registry
	for {
		registry, _, err = client.Probe(probeContext)
		if err == nil {
			break
		}
		select {
		case <-probeContext.Done():
			t.Fatalf("daemon did not become ready: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if want := fmt.Sprintf("http://127.0.0.1:%d", port); registry.URL != want {
		t.Fatalf("published URL = %q, want %q", registry.URL, want)
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func TestRunRejectsAnUnreachableListenOverride(t *testing.T) {
	t.Parallel()
	err := Run(context.Background(), RunOptions{
		Paths: apphome.FromHome(filepath.Join(t.TempDir(), "kit")), Listen: "192.168.1.10:7777",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err == nil || !strings.Contains(err.Error(), "loopback address or 0.0.0.0/::") {
		t.Fatalf("Run() error = %v, want the listen address rule", err)
	}
}
