package session_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/promptcommands"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/systemprompt"
)

func TestPromptCommandUsesRuntimeSnapshotAndSubmitsExpandedMessage(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	for _, directory := range []string{paths.Prompts, cwd} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	globalPath := filepath.Join(paths.Prompts, "review.md")
	if err := os.WriteFile(globalPath, []byte("---\ndescription: Review globally\n---\nReview $1 carefully. Context: $@"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectDirectory := filepath.Join(cwd, ".agents", "prompts")
	if err := os.MkdirAll(projectDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDirectory, "review.md"), []byte("Project should lose"), 0o600); err != nil {
		t.Fatal(err)
	}
	promptLoader, err := promptcommands.NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	skillRegistry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	bundleBuilder, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{
		Core: systemprompt.DefaultCore, Registry: skillRegistry, PromptCommandLoader: promptLoader,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, bundleBuilder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: cwd, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.PromptCommands) != 1 || snapshot.PromptCommands[0].Name != "review" || snapshot.PromptCommands[0].Source != "user" {
		t.Fatalf("prompt command catalog = %#v", snapshot.PromptCommands)
	}
	reservation, err := manager.StartPromptCommand(t.Context(), record.ID, "review", `"auth module" thoroughly`)
	if err != nil {
		t.Fatal(err)
	}
	waitForPromptCommandRun(t, manager, record.ID, reservation.RunID)
	providers.mu.Lock()
	request := providers.requests[0]
	providers.mu.Unlock()
	lastUser := request.Messages[len(request.Messages)-1].(droids.UserMessage)
	if got := lastUser.Content[0].(droids.TextInput).Text; got != "Review auth module carefully. Context: auth module thoroughly" {
		t.Fatalf("provider prompt = %q", got)
	}
	snapshot, err = manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Messages) < 1 || snapshot.Messages[0].Role != "user" || snapshot.Messages[0].Content[0].Text != "Review auth module carefully. Context: auth module thoroughly" {
		t.Fatalf("persisted expanded prompt = %#v", snapshot.Messages)
	}

	if err := os.Remove(globalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(projectDirectory, "review.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReloadSession(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartPromptCommand(t.Context(), record.ID, "review", ""); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("removed command error = %v, want not found", err)
	}
}

func waitForPromptCommandRun(t *testing.T, manager *session.Manager, sessionID, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := manager.GetRun(t.Context(), sessionID, runID)
		if err == nil && run.Status == session.RunStatusCompleted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("prompt command run did not complete")
}
