package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func testMCPSession(access string) MCPOAuthSession {
	return MCPOAuthSession{
		Config: oauth2.Config{
			ClientID: "client", ClientSecret: "secret", RedirectURL: "http://127.0.0.1/callback",
			Endpoint: oauth2.Endpoint{AuthURL: "https://auth.example/authorize", TokenURL: "https://auth.example/token"},
			Scopes:   []string{"read", "offline_access"},
		},
		Token:  oauth2.Token{AccessToken: access, RefreshToken: "refresh", TokenType: "Bearer", Expiry: time.Now().UTC().Add(time.Hour).Truncate(time.Second)},
		Issuer: "https://auth.example",
	}
}

func TestMCPOAuthStoreSaveLoadReplaceAndDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp-auth.json")
	store := NewStore(path)
	ctx := t.Context()

	revision, err := store.SaveMCPOAuth(ctx, "docs@abc", "", testMCPSession("first"))
	if err != nil {
		t.Fatal(err)
	}
	if len(revision) != 32 {
		t.Fatalf("revision = %q", revision)
	}
	record, err := store.LoadMCPOAuth(ctx, "docs@abc")
	if err != nil {
		t.Fatal(err)
	}
	if !record.Found || record.Revision != revision || record.Session.Token.AccessToken != "first" || record.Session.Config.ClientSecret != "secret" || record.Session.Issuer != "https://auth.example" {
		t.Fatalf("record = %#v", record)
	}

	next, err := store.SaveMCPOAuth(ctx, "docs@abc", revision, testMCPSession("second"))
	if err != nil {
		t.Fatal(err)
	}
	if next == revision {
		t.Fatal("replacement reused its generation")
	}
	if _, err := store.SaveMCPOAuth(ctx, "docs@abc", revision, testMCPSession("stale")); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("stale save = %v, want generation conflict", err)
	}
	if err := store.DeleteMCPOAuth(ctx, "docs@abc"); err != nil {
		t.Fatal(err)
	}
	missing, err := store.LoadMCPOAuth(ctx, "docs@abc")
	if err != nil || missing.Found || missing.Revision == "" {
		t.Fatalf("after delete = %#v, %v", missing, err)
	}
	if _, err := store.SaveMCPOAuth(ctx, "docs@abc", next, testMCPSession("resurrected")); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("pre-logout generation resurrected credentials: %v", err)
	}
}

func TestMCPOAuthRejectedGenerationCannotDeleteNewerCredentials(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "mcp-auth.json"))
	first, err := store.SaveMCPOAuth(t.Context(), "docs@abc", "", testMCPSession("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveMCPOAuth(t.Context(), "docs@abc", first, testMCPSession("second"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteMCPOAuthRevision(t.Context(), "docs@abc", first); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("stale rejection delete = %v", err)
	}
	record, err := store.LoadMCPOAuth(t.Context(), "docs@abc")
	if err != nil || !record.Found || record.Revision != second || record.Session.Token.AccessToken != "second" {
		t.Fatalf("newer credentials = %#v, %v", record, err)
	}
}

func TestMCPOAuthStorePreservesOtherServersAndProtectsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp-auth.json")
	store := NewStore(path)
	for _, key := range []string{"one@abc", "two@def"} {
		if _, err := store.SaveMCPOAuth(t.Context(), key, "", testMCPSession(key)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteMCPOAuth(t.Context(), "one@abc"); err != nil {
		t.Fatal(err)
	}
	if record, err := store.LoadMCPOAuth(t.Context(), "two@def"); err != nil || !record.Found {
		t.Fatalf("unrelated server = %#v, %v", record, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("credential permissions = %o, want private", info.Mode().Perm())
	}
}

func TestMCPOAuthStoreRejectsMalformedAndOversizedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp-auth.json")
	store := NewStore(path)
	if _, err := store.SaveMCPOAuth(t.Context(), "bad\x00key", "", testMCPSession("token")); err == nil {
		t.Fatal("accepted malformed server key")
	}
	session := testMCPSession("token")
	session.Token.RefreshToken = string(make([]byte, maximumOAuthValueLen+1))
	if _, err := store.SaveMCPOAuth(t.Context(), "docs@abc", "", session); err == nil {
		t.Fatal("accepted oversized token")
	}
	if err := os.WriteFile(path, []byte(`{"docs@abc":{"type":"mcp_oauth","revision":"bad"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadMCPOAuth(context.Background(), "docs@abc"); err == nil {
		t.Fatal("accepted malformed stored credentials")
	}
}
