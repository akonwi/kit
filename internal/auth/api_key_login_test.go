package auth

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAPIKeyLoginSavesTheKey(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "kit", "auth.json"))
	login, err := NewAPIKeyLogin(APIKeyLoginOptions{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := login.Login(context.Background(), AnthropicProviderID, "anthropic-secret"); err != nil {
		t.Fatal(err)
	}
	record, err := store.LoadAPIKey(context.Background(), AnthropicProviderID)
	if err != nil {
		t.Fatal(err)
	}
	if record.APIKey != "anthropic-secret" {
		t.Fatalf("stored API key = %q", record.APIKey)
	}
}

func TestAPIKeyLoginRejectsUnsupportedProviders(t *testing.T) {
	t.Parallel()
	login, err := NewAPIKeyLogin(APIKeyLoginOptions{Store: NewStore(filepath.Join(t.TempDir(), "kit", "auth.json"))})
	if err != nil {
		t.Fatal(err)
	}
	if err := login.Login(context.Background(), OpenAICodexProviderID, "secret"); err == nil {
		t.Fatal("an API key was accepted for a provider without API-key login")
	}
}
