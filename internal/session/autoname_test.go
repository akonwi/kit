package session_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func TestAutomaticNamingUsesPrivateForkAndPreservesExplicitNames(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(
		store, providers, staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
		session.WithAutomaticNaming(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)

	unnamed, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), unnamed.ID, "inspect the parser"); err != nil {
		t.Fatal(err)
	}
	if got, err := manager.Get(t.Context(), unnamed.ID); err != nil || got.Name != "" {
		t.Fatalf("name after one turn = %q, %v", got.Name, err)
	}
	if _, err := manager.RunPrompt(t.Context(), unnamed.ID, "then add a test"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var named session.SessionRecord
	for {
		named, err = manager.Get(t.Context(), unnamed.ID)
		if err != nil {
			t.Fatal(err)
		}
		if named.Name != "" || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if named.Name != "reply 3" {
		t.Fatalf("automatic name = %q, want reply 3", named.Name)
	}
	providers.mu.Lock()
	titleRequest := providers.requests[len(providers.requests)-1]
	providers.mu.Unlock()
	if !strings.Contains(titleRequest.SystemPrompt, "conversation titles") || len(titleRequest.Tools) != 0 {
		t.Fatalf("title request prompt = %q tools = %d", titleRequest.SystemPrompt, len(titleRequest.Tools))
	}

	explicit, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Keep this", Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	before := providers.calls
	providers.mu.Unlock()
	if _, err := manager.RunPrompt(t.Context(), explicit.ID, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), explicit.ID, "second"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		providers.mu.Lock()
		calls := providers.calls
		providers.mu.Unlock()
		if calls >= before+3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, err := manager.Get(t.Context(), explicit.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Keep this" {
		t.Fatalf("explicit name = %q", got.Name)
	}
}
