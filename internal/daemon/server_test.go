package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/droids"
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

	providers, sources, err := providersFromEnvironment(context.Background(), apphome.FromHome(filepath.Join(t.TempDir(), "kit")))
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("openai-codex/gpt-5.6-sol")
	if !ok || model.Provider != "openai-codex" {
		t.Fatalf("Codex model = %#v, %v", model, ok)
	}
	if source := sources[auth.OpenAICodexProviderID]; source != CredentialSourceEnvironment {
		t.Fatalf("credential source = %q", source)
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
	providers, sources, err := providersFromEnvironment(context.Background(), apphome.FromHome(filepath.Join(t.TempDir(), "kit")))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := providers.Model("openai-codex/gpt-5.6-sol"); !ok {
		t.Fatal("stored-credential Codex provider was not registered")
	}
	for _, providerID := range []string{auth.OpenAIProviderID, auth.AnthropicProviderID, auth.OpenCodeGoProviderID, auth.OpenAICodexProviderID} {
		if source := sources[providerID]; source != CredentialSourceStore {
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
	if err := store.ReplaceAPIKey(context.Background(), auth.OpenCodeGoProviderID, "opencode-go-secret"); err != nil {
		t.Fatal(err)
	}
	_, sources, err := providersFromEnvironment(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	got := availableProviderIDs(context.Background(), paths, sources)
	if want := []string{auth.AnthropicProviderID, auth.OpenAIProviderID, auth.OpenCodeGoProviderID}; !slices.Equal(got, want) {
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
	_, sources, err := providersFromEnvironment(context.Background(), paths)
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
	if _, _, err := providersFromEnvironment(context.Background(), apphome.FromHome(filepath.Join(t.TempDir(), "kit"))); err == nil {
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

	client := NewClient(paths)
	probeContext, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	var registry Registry
	for {
		var err error
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
	if registry.PID != os.Getpid() {
		t.Errorf("PID = %d, want in-process test PID %d", registry.PID, os.Getpid())
	}
	info, err := os.Stat(paths.ServerToken)
	if err != nil {
		t.Fatalf("stat token: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token mode = %o, want 600", info.Mode().Perm())
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
	if err := NewManager(paths).Stop(stopContext); err != nil {
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
