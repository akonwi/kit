package session_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/systemprompt"
)

func TestForkContextSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	builder, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{Core: systemprompt.DefaultCore, Registry: registry})
	if err != nil {
		t.Fatal(err)
	}
	providers := &authorityProviders{}
	open := func() *session.Manager {
		manager, err := session.NewManager(store, providers, builder, session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(manager.Close)
		return manager
	}
	manager := open()
	parent, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Original parent name", Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	forked, err := manager.Fork(t.Context(), parent.ID, session.ForkInput{})
	if err != nil {
		t.Fatal(err)
	}
	childID := forked.Session.ID
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager = open()
	pending, err := manager.Snapshot(t.Context(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending.Boundaries) != 1 || pending.Boundaries[0].ID != "session-fork:"+childID {
		t.Fatalf("boundaries = %+v", pending.Boundaries)
	}
	providers.mu.Lock()
	calls := len(providers.requests)
	providers.mu.Unlock()
	if calls != 0 {
		t.Fatalf("fork triggered %d provider requests", calls)
	}
	if _, err := manager.RunPrompt(t.Context(), parent.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	consumed, err := manager.Snapshot(t.Context(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, message := range consumed.Messages {
		if message.Role == "context" && message.BoundaryKind == "session_forked" {
			count++
			if len(message.Content) != 2 || message.Content[1].Text != "This session was forked into session "+childID+". The fork has independent state." {
				t.Fatalf("fork notice = %+v", message)
			}
		}
	}
	if count != 1 {
		t.Fatalf("fork notifications = %d", count)
	}
	if _, err := manager.RunPrompt(t.Context(), childID, "identify parent"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	prompt := providers.requests[len(providers.requests)-1].SystemPrompt
	providers.mu.Unlock()
	want := "This session's parent session ID is " + parent.ID + ". This session has independent state."
	if !strings.Contains(prompt, want) {
		t.Fatalf("child prompt missing %q: %s", want, prompt)
	}
}

// Cancel after child publication to verify the notification has its own timeout.
type cancelAfterForkPublication struct {
	session.Repository
	cancel context.CancelFunc
}

func (r *cancelAfterForkPublication) CreateSession(ctx context.Context, input session.NewSession) (session.SessionRecord, error) {
	record, err := r.Repository.CreateSession(ctx, input)
	if err == nil && input.ParentSessionID != "" && r.cancel != nil {
		r.cancel()
	}
	return record, err
}

func TestForkNotificationSurvivesPublicationCancellation(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := &cancelAfterForkPublication{Repository: store}
	manager, err := session.NewManager(repo, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	parent, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	repo.cancel = cancel
	fork, err := manager.Fork(ctx, parent.ID, session.ForkInput{})
	if err != nil || fork.Session.ID == "" {
		t.Fatalf("published fork = %+v, %v", fork, err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("expected cancellation after publication")
	}
	snapshot, err := manager.Snapshot(t.Context(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Boundaries) != 1 || snapshot.Boundaries[0].ID != "session-fork:"+fork.Session.ID {
		t.Fatalf("notice = %+v", snapshot.Boundaries)
	}
}

func TestCreatedSessionFirstPromptIncludesParentID(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	builder, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{Core: systemprompt.DefaultCore, Registry: registry})
	if err != nil {
		t.Fatal(err)
	}
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, builder, session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	parent, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	// Agent-created sessions start fresh; they are not semantic forks.
	child, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Independent task", Model: "test/echo", ParentSessionID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), child.ID, "begin"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	defer providers.mu.Unlock()
	if len(providers.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(providers.requests))
	}
	want := "This session's parent session ID is " + parent.ID + ". This session has independent state."
	if prompt := providers.requests[0].SystemPrompt; !strings.Contains(prompt, want) {
		t.Fatalf("child prompt missing %q: %s", want, prompt)
	}
}
