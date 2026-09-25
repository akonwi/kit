package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/gofrs/flock"
)

func TestStorePersistsCodexCredentialsAndPreservesOtherProviders(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "kit")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "auth.json")
	original := []byte(`{"anthropic":{"type":"api_key","key":"other-secret","future":{"field":true}}}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	credentials := droids.OpenAICodexCredentials{
		AccessToken: "access", RefreshToken: "refresh", IDToken: "id",
		AccountID: "account", ExpiresAt: time.UnixMilli(1_900_000_000_123).UTC(), FedRAMP: true,
	}
	if err := store.ReplaceOpenAICodexCredentials(context.Background(), credentials); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Credentials, credentials) || loaded.Revision == "" {
		t.Fatalf("loaded = %#v, want credentials %#v", loaded, credentials)
	}
	entries, err := readAuthEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	var preserved map[string]any
	if err := json.Unmarshal(entries["anthropic"], &preserved); err != nil {
		t.Fatal(err)
	}
	if preserved["key"] != "other-secret" || preserved["future"] == nil {
		t.Fatalf("preserved entry = %#v", preserved)
	}
	listed, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantList := []CredentialInfo{{ProviderID: "anthropic", Type: "api_key"}, {ProviderID: "openai-codex", Type: "oauth"}}
	if !reflect.DeepEqual(listed, wantList) {
		t.Fatalf("List() = %#v, want %#v", listed, wantList)
	}
	for _, protectedPath := range []string{path, path + ".lock"} {
		info, err := os.Stat(protectedPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode for %q = %o, want 600", protectedPath, info.Mode().Perm())
		}
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %o, want 700", info.Mode().Perm())
	}
}

func TestStorePersistsAPIKeyCredentials(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "kit", "auth.json"))
	ctx := context.Background()
	if err := store.ReplaceAPIKey(ctx, OpenAIProviderID, "openai-secret"); err != nil {
		t.Fatal(err)
	}
	first, err := store.LoadAPIKey(ctx, OpenAIProviderID)
	if err != nil {
		t.Fatal(err)
	}
	if first.APIKey != "openai-secret" || first.Revision == "" {
		t.Fatalf("stored OpenAI credential = %#v", first)
	}
	if err := store.ReplaceAPIKey(ctx, AnthropicProviderID, "anthropic-secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAPIKey(ctx, OpenCodeGoProviderID, "opencode-go-secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAPIKey(ctx, OpenAIProviderID, "replacement-secret"); err != nil {
		t.Fatal(err)
	}
	replacement, err := store.LoadAPIKey(ctx, OpenAIProviderID)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.APIKey != "replacement-secret" || replacement.Revision == first.Revision {
		t.Fatalf("replacement OpenAI credential = %#v; first revision %q", replacement, first.Revision)
	}
	anthropic, err := store.LoadAPIKey(ctx, AnthropicProviderID)
	if err != nil {
		t.Fatal(err)
	}
	if anthropic.APIKey != "anthropic-secret" {
		t.Fatalf("stored Anthropic key = %q", anthropic.APIKey)
	}
	listed, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []CredentialInfo{{ProviderID: AnthropicProviderID, Type: "api_key"}, {ProviderID: OpenAIProviderID, Type: "api_key"}, {ProviderID: OpenCodeGoProviderID, Type: "api_key"}}
	if !reflect.DeepEqual(listed, want) {
		t.Fatalf("List() = %#v, want %#v", listed, want)
	}
}

func TestStoreRejectsMalformedAPIKeysWithoutEchoingThem(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "kit", "auth.json"))
	for _, test := range []struct {
		provider string
		key      string
	}{
		{provider: OpenAICodexProviderID, key: "secret"},
		{provider: OpenAIProviderID, key: " secret-value "},
		{provider: AnthropicProviderID, key: "secret\nvalue"},
	} {
		err := store.ReplaceAPIKey(context.Background(), test.provider, test.key)
		if err == nil {
			t.Fatalf("ReplaceAPIKey(%q) accepted malformed input", test.provider)
		}
		if strings.Contains(err.Error(), test.key) {
			t.Fatalf("error exposed API key: %v", err)
		}
	}
}

func TestStoreRepresentsMissingAPIKeyAsZeroValue(t *testing.T) {
	t.Parallel()
	record, err := NewStore(filepath.Join(t.TempDir(), "kit", "auth.json")).LoadAPIKey(context.Background(), OpenAIProviderID)
	if err != nil {
		t.Fatal(err)
	}
	if record != (APIKeyRecord{}) {
		t.Fatalf("missing API key = %#v", record)
	}
}

func TestStoreRepresentsMissingCodexCredentialAsZeroValue(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "kit", "auth.json"))
	credentials, err := store.LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if openAICodexCredentialConfigured(credentials.Credentials) || credentials.Revision != "" {
		t.Fatalf("missing credentials = %#v", credentials)
	}
}

func TestStoreUpgradesLegacyCodexCredentialWithCASRevision(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"openai-codex":{"type":"oauth","access":"access","refresh":"refresh","accountId":"account"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	legacy, err := store.LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(legacy.Revision, "legacy:") {
		t.Fatalf("legacy revision = %q", legacy.Revision)
	}
	formattedRevision, err := decodeOpenAICodexRevision(json.RawMessage(`{
		"accountId": "account",
		"refresh": "refresh",
		"access": "access",
		"type": "oauth"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if formattedRevision != legacy.Revision {
		t.Fatalf("formatted legacy revision = %q, want %q", formattedRevision, legacy.Revision)
	}
	next, err := store.SaveOpenAICodexCredentials(context.Background(), legacy.Revision, legacy.Credentials)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(next, "v1:") {
		t.Fatalf("next revision = %q", next)
	}
}

func TestStoreDoesNotOverwriteMalformedCredentialFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte("{ malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	_, err := store.SaveOpenAICodexCredentials(context.Background(), "expected", testCredentials("account"))
	if err == nil {
		t.Fatal("SaveOpenAICodexCredentials() accepted malformed storage")
	}
	body, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != "{ malformed" {
		t.Fatalf("malformed file was overwritten: %q", body)
	}
}

func TestStoreSeparatesRefreshAndIntentionalAccountReplacement(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := store.ReplaceOpenAICodexCredentials(context.Background(), testCredentials("account-a")); err != nil {
		t.Fatal(err)
	}
	current, err := store.LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveOpenAICodexCredentials(context.Background(), current.Revision, testCredentials("account-b")); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("cross-account refresh error = %v", err)
	}
	loaded, err := store.LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Credentials.AccountID != "account-a" {
		t.Fatalf("account after rejected refresh = %q", loaded.Credentials.AccountID)
	}
	if err := store.ReplaceOpenAICodexCredentials(context.Background(), testCredentials("account-b")); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Credentials.AccountID != "account-b" {
		t.Fatalf("account after replacement = %q", loaded.Credentials.AccountID)
	}
}

func TestStoreRevisionRejectsStaleSameAccountRefreshAndDeletion(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "auth.json"))
	credentials := testCredentials("account")
	if err := store.ReplaceOpenAICodexCredentials(context.Background(), credentials); err != nil {
		t.Fatal(err)
	}
	first, err := store.LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	credentials.RefreshToken = "new-login-refresh"
	if err := store.ReplaceOpenAICodexCredentials(context.Background(), credentials); err != nil {
		t.Fatal(err)
	}
	second, err := store.LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision == second.Revision {
		t.Fatal("replacement reused a credential revision")
	}
	if _, err := store.SaveOpenAICodexCredentials(context.Background(), "", first.Credentials); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("create-through-refresh error = %v", err)
	}
	if _, err := store.SaveOpenAICodexCredentials(context.Background(), first.Revision, first.Credentials); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("stale same-account refresh error = %v", err)
	}
	if err := store.Delete(context.Background(), OpenAICodexProviderID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveOpenAICodexCredentials(context.Background(), second.Revision, second.Credentials); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("refresh after deletion error = %v", err)
	}
}

func TestStoreDeletePreservesOtherEntries(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"anthropic":{"type":"api_key","key":"secret"},"openai-codex":{"type":"oauth","access":"access","accountId":"account"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if err := store.Delete(context.Background(), OpenAICodexProviderID); err != nil {
		t.Fatal(err)
	}
	entries, err := readAuthEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := entries[OpenAICodexProviderID]; exists {
		t.Fatal("Codex credential still exists")
	}
	if _, exists := entries["anthropic"]; !exists {
		t.Fatal("unrelated credential was removed")
	}
}

func TestStoreSerializesConcurrentWriters(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "auth.json")
	stores := []*Store{NewStore(path), NewStore(path)}
	if err := stores[0].ReplaceOpenAICodexCredentials(context.Background(), testCredentials("account")); err != nil {
		t.Fatal(err)
	}
	initial, err := stores[0].LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, len(stores))
	for index, store := range stores {
		wait.Add(1)
		go func(index int, store *Store) {
			defer wait.Done()
			credentials := testCredentials("account")
			credentials.AccessToken += string(rune('a' + index))
			_, err := store.SaveOpenAICodexCredentials(context.Background(), initial.Revision, credentials)
			errorsFound <- err
		}(index, store)
	}
	wait.Wait()
	close(errorsFound)
	var succeeded, conflicted int
	for err := range errorsFound {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrCredentialsChanged):
			conflicted++
		default:
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("successful writes = %d, conflicts = %d", succeeded, conflicted)
	}
	if _, err := stores[0].LoadOpenAICodexCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStoreLockWaitHonorsContext(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "auth.json")
	store := NewStore(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := prepareLockFile(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	held := flock.New(path + ".lock")
	locked, err := held.TryLock()
	if err != nil || !locked {
		t.Fatalf("hold lock = %v, %v", locked, err)
	}
	defer held.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := store.LoadOpenAICodexCredentials(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("LoadOpenAICodexCredentials() error = %v", err)
	}
}

func TestStoreRejectsNonRegularCredentialAndLockPaths(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	credentialLink := filepath.Join(directory, "auth.json")
	if err := os.Symlink(target, credentialLink); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(credentialLink).LoadOpenAICodexCredentials(context.Background()); err == nil {
		t.Fatal("credential symlink was accepted")
	}

	lockStore := NewStore(filepath.Join(directory, "other.json"))
	if err := os.Symlink(target, lockStore.lockPath); err != nil {
		t.Fatal(err)
	}
	if _, err := lockStore.LoadOpenAICodexCredentials(context.Background()); err == nil {
		t.Fatal("lock symlink was accepted")
	}
}

func TestStoreRejectsUnsafeMetadataBeforeDisplay(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bad\nprovider":{"type":"oauth"}}`,
		`{"bad\u009bprovider":{"type":"oauth"}}`,
		`{"provider":{"type":"bad\ttype"}}`,
	} {
		path := filepath.Join(t.TempDir(), "auth.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(path).List(context.Background()); err == nil {
			t.Fatalf("List() accepted unsafe metadata in %s", body)
		}
	}
}

func TestStoreRejectsRelativePathAndMalformedCredentials(t *testing.T) {
	t.Parallel()
	if _, err := NewStore("auth.json").LoadOpenAICodexCredentials(context.Background()); err == nil {
		t.Fatal("relative credential path was accepted")
	}
	store := NewStore(filepath.Join(t.TempDir(), "auth.json"))
	credentials := testCredentials("account")
	credentials.AccessToken = " secret "
	if _, err := store.SaveOpenAICodexCredentials(context.Background(), "expected", credentials); err == nil {
		t.Fatal("credential with whitespace was accepted")
	}
}

func testCredentials(accountID string) droids.OpenAICodexCredentials {
	return droids.OpenAICodexCredentials{
		AccessToken: "access", RefreshToken: "refresh", AccountID: accountID,
		ExpiresAt: time.UnixMilli(1_900_000_000_000).UTC(),
	}
}

func openAICodexCredentialConfigured(credentials droids.OpenAICodexCredentials) bool {
	return credentials.AccessToken != "" || credentials.RefreshToken != "" ||
		credentials.IDToken != "" || credentials.AccountID != "" ||
		!credentials.ExpiresAt.IsZero() || credentials.FedRAMP
}

func TestAnthropicOAuthCredentialRefreshUsesRevisionGuard(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "kit", "auth.json"))
	ctx := context.Background()
	initial := droids.AnthropicCredentials{
		AccessToken: "access-one", RefreshToken: "refresh-one", ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.ReplaceAnthropicOAuthCredentials(ctx, initial); err != nil {
		t.Fatal(err)
	}
	record, err := store.LoadAnthropicCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Credentials.AccessToken != initial.AccessToken || record.Revision == "" {
		t.Fatalf("loaded credential = %#v", record)
	}
	replacement := droids.AnthropicCredentials{
		AccessToken: "access-two", RefreshToken: "refresh-two", ExpiresAt: time.Now().Add(2 * time.Hour),
	}
	if err := store.ReplaceAnthropicOAuthCredentials(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveAnthropicCredentials(ctx, record.Revision, droids.AnthropicCredentials{
		AccessToken: "stale-access", RefreshToken: "stale-refresh", ExpiresAt: time.Now().Add(3 * time.Hour),
	}); !errors.Is(err, droids.ErrAnthropicCredentialsChanged) {
		t.Fatalf("stale refresh save error = %v", err)
	}
	got, err := store.LoadAnthropicCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Credentials.AccessToken != replacement.AccessToken {
		t.Fatalf("stale refresh replaced current credential: %#v", got)
	}
}
