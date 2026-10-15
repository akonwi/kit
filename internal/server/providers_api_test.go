package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/pairing"
	"github.com/akonwi/kit/internal/providerlogin"
	"github.com/akonwi/kit/internal/serveraccess"
)

type providerHarness struct {
	*deviceHarness
	store    *auth.Store
	approved chan error
}

// newProviderHarness serves providers with fake login flows: a device login
// finishes with the error sent on approved, and a browser login accepts the
// authorization "code#state".
func newProviderHarness(t *testing.T, sources map[string]localdiscovery.CredentialSource) *providerHarness {
	t.Helper()
	devices := newDeviceHarness(t)
	store := auth.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	harness := &providerHarness{deviceHarness: devices, store: store, approved: make(chan error, 1)}
	logins := providerlogin.NewRegistry(providerlogin.Flows{
		Device: func(ctx context.Context, _ string, notify func(providerlogin.DeviceInstructions)) error {
			notify(providerlogin.DeviceInstructions{VerificationURI: "https://auth.openai.com/codex/device", UserCode: "ABCD-1234", ExpiresAt: devices.now.Add(15 * 60e9)})
			select {
			case err := <-harness.approved:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		Browser: func(ctx context.Context, _ string, authorization <-chan string, notify func(providerlogin.BrowserInstructions)) error {
			notify(providerlogin.BrowserInstructions{AuthorizationURL: "https://claude.ai/oauth/authorize?state=state", RedirectURI: "http://localhost:53692/callback"})
			select {
			case input := <-authorization:
				if input != "code#state" {
					return errors.New("Anthropic OAuth state mismatch")
				}
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}, nil)
	t.Cleanup(logins.Close)
	next := newHandler(localHandlerOptions{
		access: serveraccess.NewPolicy([]string{"kit.example.ts.net"}, nil), token: "test",
		registry: localdiscovery.Registry{InstanceID: "instance_test"},
		devices:  devices.devices, pairing: pairing.NewRegistry(nil),
		providerAccess: &providerAccess{logins: logins, store: store, sources: sources, configured: func(ctx context.Context) []string {
			record, err := store.LoadAPIKey(ctx, auth.OpenAIProviderID)
			if err == nil && record.APIKey != "" {
				return []string{auth.OpenAIProviderID}
			}
			return nil
		}},
	})
	devices.handler = contractConformanceMiddleware(t, next, func(err error) { t.Errorf("contract violation: %v", err) })
	return harness
}

func TestProvidersListTheirMethodsAndCredentialSources(t *testing.T) {
	harness := newProviderHarness(t, map[string]localdiscovery.CredentialSource{auth.AnthropicProviderID: localdiscovery.CredentialSourceEnvironment})
	if status, code := harness.do(http.MethodPut, "/v1/providers/openai/api-key", "test", protocol.ProviderAPIKeyInput{APIKey: "sk-test"}, nil); status != http.StatusNoContent {
		t.Fatalf("set API key = %d %s", status, code)
	}
	var list protocol.ProviderList
	if status, code := harness.do(http.MethodGet, "/v1/providers", "test", nil, &list); status != http.StatusOK {
		t.Fatalf("list = %d %s", status, code)
	}
	want := []protocol.ProviderInfo{
		{ID: "openai-codex", Name: "OpenAI Codex", Methods: []protocol.LoginMethod{"device"}, Source: "store"},
		{ID: "anthropic", Name: "Anthropic", Methods: []protocol.LoginMethod{"browser", "apiKey"}, Source: "environment"},
		{ID: "openai", Name: "OpenAI", Methods: []protocol.LoginMethod{"apiKey"}, Configured: true, Source: "store"},
		{ID: "opencode-go", Name: "OpenCode Go", Methods: []protocol.LoginMethod{"apiKey"}, Source: "store"},
	}
	if len(list.Providers) != len(want) {
		t.Fatalf("providers = %+v", list.Providers)
	}
	for index, provider := range list.Providers {
		expected := want[index]
		if provider.ID != expected.ID || provider.Name != expected.Name || provider.Configured != expected.Configured ||
			provider.Source != expected.Source || len(provider.Methods) != len(expected.Methods) || provider.Methods[0] != expected.Methods[0] {
			t.Errorf("provider %d = %+v, want %+v", index, provider, expected)
		}
	}
}

func TestADeviceLoginIsFollowedUntilItSucceeds(t *testing.T) {
	harness := newProviderHarness(t, nil)
	var started protocol.ProviderLogin
	if status, code := harness.do(http.MethodPost, "/v1/logins", "test", protocol.StartLoginInput{Provider: "openai-codex", Method: "device"}, &started); status != http.StatusCreated {
		t.Fatalf("start = %d %s", status, code)
	}
	if started.State != "pending" || started.Device == nil || started.Device.UserCode != "ABCD-1234" {
		t.Fatalf("started = %+v", started)
	}
	var pending protocol.ProviderLogin
	if status, _ := harness.do(http.MethodGet, "/v1/logins/"+started.ID, "test", nil, &pending); status != http.StatusOK || pending.State != "pending" {
		t.Fatalf("get = %d %+v", status, pending)
	}
	harness.approved <- nil
	var finished protocol.ProviderLogin
	if status, _ := harness.do(http.MethodGet, "/v1/logins/"+started.ID+"?waitSeconds=5", "test", nil, &finished); status != http.StatusOK || finished.State != "succeeded" {
		t.Fatalf("wait = %d %+v", status, finished)
	}
	if status, code := harness.do(http.MethodGet, "/v1/logins/"+started.ID+"?waitSeconds=26", "test", nil, nil); status != http.StatusBadRequest || code != "invalid_request" {
		t.Fatalf("overlong wait = %d %s", status, code)
	}
}

func TestABrowserLoginTakesARelayedAuthorization(t *testing.T) {
	harness := newProviderHarness(t, nil)
	var started protocol.ProviderLogin
	if status, code := harness.do(http.MethodPost, "/v1/logins", "test", protocol.StartLoginInput{Provider: "anthropic", Method: "browser"}, &started); status != http.StatusCreated {
		t.Fatalf("start = %d %s", status, code)
	}
	if started.Browser == nil || started.Browser.RedirectURI != "http://localhost:53692/callback" {
		t.Fatalf("started = %+v", started)
	}
	if status, code := harness.do(http.MethodPost, "/v1/logins/"+started.ID+"/authorization", "test", protocol.LoginAuthorizationInput{Authorization: "code#state"}, nil); status != http.StatusNoContent {
		t.Fatalf("submit = %d %s", status, code)
	}
	var finished protocol.ProviderLogin
	if status, _ := harness.do(http.MethodGet, "/v1/logins/"+started.ID+"?waitSeconds=5", "test", nil, &finished); status != http.StatusOK || finished.State != "succeeded" {
		t.Fatalf("wait = %d %+v", status, finished)
	}
	if status, code := harness.do(http.MethodPost, "/v1/logins/"+started.ID+"/authorization", "test", protocol.LoginAuthorizationInput{Authorization: "code#state"}, nil); status != http.StatusConflict || code != "conflict" {
		t.Fatalf("submit after success = %d %s", status, code)
	}
}

func TestLoginsCanBeCancelled(t *testing.T) {
	harness := newProviderHarness(t, nil)
	var started protocol.ProviderLogin
	harness.do(http.MethodPost, "/v1/logins", "test", protocol.StartLoginInput{Provider: "openai-codex", Method: "device"}, &started)
	if status, code := harness.do(http.MethodDelete, "/v1/logins/"+started.ID, "test", nil, nil); status != http.StatusNoContent {
		t.Fatalf("cancel = %d %s", status, code)
	}
	var cancelled protocol.ProviderLogin
	if harness.do(http.MethodGet, "/v1/logins/"+started.ID, "test", nil, &cancelled); cancelled.State != "cancelled" {
		t.Fatalf("after cancel = %+v", cancelled)
	}
	for _, path := range []string{"/v1/logins/login_00000000000000000000000000000000", "/v1/logins/not-a-login"} {
		if status, code := harness.do(http.MethodDelete, path, "test", nil, nil); status != http.StatusNotFound || code != "not_found" {
			t.Errorf("cancel %s = %d %s", path, status, code)
		}
	}
}

func TestCredentialsFromTheEnvironmentCannotBeChanged(t *testing.T) {
	harness := newProviderHarness(t, map[string]localdiscovery.CredentialSource{auth.AnthropicProviderID: localdiscovery.CredentialSourceEnvironment})
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{method: http.MethodPost, path: "/v1/logins", body: protocol.StartLoginInput{Provider: "anthropic", Method: "browser"}},
		{method: http.MethodPut, path: "/v1/providers/anthropic/api-key", body: protocol.ProviderAPIKeyInput{APIKey: "sk-ant"}},
		{method: http.MethodDelete, path: "/v1/providers/anthropic/credentials"},
	} {
		if status, code := harness.do(request.method, request.path, "test", request.body, nil); status != http.StatusConflict || code != "conflict" {
			t.Errorf("%s %s = %d %s", request.method, request.path, status, code)
		}
	}
}

func TestAPIKeysAndLogoutChangeTheServerStore(t *testing.T) {
	harness := newProviderHarness(t, nil)
	if status, code := harness.do(http.MethodPut, "/v1/providers/opencode-go/api-key", "test", protocol.ProviderAPIKeyInput{APIKey: "oc-key"}, nil); status != http.StatusNoContent {
		t.Fatalf("set = %d %s", status, code)
	}
	if record, err := harness.store.LoadAPIKey(t.Context(), auth.OpenCodeGoProviderID); err != nil || record.APIKey != "oc-key" {
		t.Fatalf("stored = %+v, %v", record, err)
	}
	if status, code := harness.do(http.MethodDelete, "/v1/providers/opencode-go/credentials", "test", nil, nil); status != http.StatusNoContent {
		t.Fatalf("logout = %d %s", status, code)
	}
	if record, err := harness.store.LoadAPIKey(t.Context(), auth.OpenCodeGoProviderID); err != nil || record.APIKey != "" {
		t.Fatalf("after logout = %+v, %v", record, err)
	}
	for _, request := range []struct {
		method, path string
		body         any
		status       int
		code         string
	}{
		{method: http.MethodPut, path: "/v1/providers/openai-codex/api-key", body: protocol.ProviderAPIKeyInput{APIKey: "k"}, status: 400, code: "invalid_request"},
		{method: http.MethodPut, path: "/v1/providers/openai/api-key", body: protocol.ProviderAPIKeyInput{APIKey: "has space"}, status: 400, code: "invalid_request"},
		{method: http.MethodPut, path: "/v1/providers/nope/api-key", body: protocol.ProviderAPIKeyInput{APIKey: "k"}, status: 404, code: "not_found"},
		{method: http.MethodPost, path: "/v1/logins", body: protocol.StartLoginInput{Provider: "openai", Method: "device"}, status: 400, code: "invalid_request"},
	} {
		if status, code := harness.do(request.method, request.path, "test", request.body, nil); status != request.status || code != request.code {
			t.Errorf("%s %s = %d %s, want %d %s", request.method, request.path, status, code, request.status, request.code)
		}
	}
}
