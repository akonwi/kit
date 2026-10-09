package session_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
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
	if err := os.WriteFile(globalPath, []byte("---\ndescription: Review globally\nargument-hint: <scope>\n---\nReview $1 carefully. Context: $@"), 0o600); err != nil {
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
	if len(snapshot.PromptCommands) != 1 || snapshot.PromptCommands[0].Name != "review" || snapshot.PromptCommands[0].Source != "user" || snapshot.PromptCommands[0].ArgumentHint != "<scope>" {
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
	wantInput := droids.PromptCommandInput{Name: "review", Arguments: `"auth module" thoroughly`, Source: "user", Text: "Review auth module carefully. Context: auth module thoroughly"}
	if !reflect.DeepEqual(lastUser.Content, []droids.InputContent{wantInput}) {
		t.Fatalf("provider prompt = %#v, want %#v", lastUser.Content, wantInput)
	}
	snapshot, err = manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantContent := []session.TranscriptContent{{Kind: session.TranscriptContentPromptCommand, Name: "review", Arguments: `"auth module" thoroughly`, Source: "user", Text: "Review auth module carefully. Context: auth module thoroughly"}}
	if len(snapshot.Messages) < 1 || snapshot.Messages[0].Role != "user" || !reflect.DeepEqual(snapshot.Messages[0].Content, wantContent) {
		t.Fatalf("persisted prompt command = %#v, want %#v", snapshot.Messages, wantContent)
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

// TestQueuedPromptCommandPresentsItsInvocation checks that a queued prompt
// command previews and restores as its invocation, and that the turn it starts
// records the invocation with its expansion.
func TestQueuedPromptCommandPresentsItsInvocation(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	commands := filepath.Join(cwd, ".claude", "commands")
	for _, directory := range []string{paths.Prompts, commands} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(commands, "fix.md"), []byte("---\nargument-hint: [issue] [priority]\n---\nFix issue #$1 at $2 priority."), 0o600); err != nil {
		t.Fatal(err)
	}
	promptLoader, err := promptcommands.NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	promptLoader.ReadClaudeCommands = func() bool { return true }
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
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, bundleBuilder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: cwd, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartPrompt(t.Context(), record.ID, "first"); err != nil {
		t.Fatal(err)
	}
	<-providers.started

	queued, err := manager.SubmitPromptCommand(t.Context(), record.ID, "fix", "123   high")
	if err != nil || !queued.Queued || !reflect.DeepEqual(queued.Queue.Previews, []string{"/fix 123 high"}) {
		t.Fatalf("SubmitPromptCommand() = %+v, %v; want preview /fix 123 high", queued, err)
	}
	restored, err := manager.RestoreFollowUps(t.Context(), record.ID)
	if err != nil || !reflect.DeepEqual(restored.Messages, []session.PromptInput{{Text: "/fix 123   high"}}) {
		t.Fatalf("RestoreFollowUps() = %+v, %v; want the invocation", restored, err)
	}
	if _, err := manager.SubmitPromptCommand(t.Context(), record.ID, "fix", "123   high"); err != nil {
		t.Fatal(err)
	}
	close(providers.block)

	wantContent := []session.TranscriptContent{{Kind: session.TranscriptContentPromptCommand, Name: "fix", Arguments: "123   high", Source: "claude_project", Text: "Fix issue #123 at high priority."}}
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := manager.Snapshot(t.Context(), record.ID)
		if err != nil {
			t.Fatal(err)
		}
		var users [][]session.TranscriptContent
		for _, message := range snapshot.Messages {
			if message.Role == "user" {
				users = append(users, message.Content)
			}
		}
		if snapshot.ActiveRunID == "" && snapshot.FollowUps.Count == 0 && len(users) == 2 {
			if !reflect.DeepEqual(users[1], wantContent) {
				t.Fatalf("queued command message = %#v, want %#v", users[1], wantContent)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued command did not run: active=%q queue=%+v users=%d", snapshot.ActiveRunID, snapshot.FollowUps, len(users))
		}
		time.Sleep(10 * time.Millisecond)
	}
	page, err := manager.Events(t.Context(), record.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var live []string
	for _, event := range page.Events {
		if event.Kind == session.EventUserMessage {
			live = append(live, event.Text)
		}
	}
	if len(live) == 0 || live[len(live)-1] != "/fix 123   high" {
		t.Fatalf("live user messages = %q", live)
	}
}
