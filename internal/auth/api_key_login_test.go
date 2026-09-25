package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestAPIKeyLoginGuardsCredentialSave(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "kit", "auth.json"))
	var acquired, released string
	login, err := NewAPIKeyLogin(APIKeyLoginOptions{
		Store: store,
		AcquireSave: func(_ context.Context, providerID string) (func() error, error) {
			acquired = providerID
			return func() error {
				released = providerID
				return nil
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := login.Login(context.Background(), AnthropicProviderID, "anthropic-secret"); err != nil {
		t.Fatal(err)
	}
	if acquired != AnthropicProviderID || released != AnthropicProviderID {
		t.Fatalf("lifecycle = acquired %q, released %q", acquired, released)
	}
	record, err := store.LoadAPIKey(context.Background(), AnthropicProviderID)
	if err != nil {
		t.Fatal(err)
	}
	if record.APIKey != "anthropic-secret" {
		t.Fatalf("stored API key = %q", record.APIKey)
	}
}

func TestAPIKeyLoginReportsFailedGuardRelease(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "kit", "auth.json"))
	login, err := NewAPIKeyLogin(APIKeyLoginOptions{
		Store: store,
		AcquireSave: func(context.Context, string) (func() error, error) {
			return func() error { return errors.New("release failed") }, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := login.Login(context.Background(), OpenAIProviderID, "openai-secret"); err == nil {
		t.Fatal("Login succeeded after the save guard failed to release")
	}
}
