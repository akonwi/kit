package session_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
)

func TestLoadedParentPublishesSubagentLifecycleInvalidation(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = manager.Shutdown(context.Background())
		_ = store.Close()
	})
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_88888888888888888888888888888888", CWD: root, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager.SubagentChanged(t.Context(), record.ID,
		subagent.ConversationID("subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		subagent.TaskID("task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	page, err := manager.Events(t.Context(), record.ID, snapshot.EventStreamID, snapshot.EventCursor)
	if err != nil || len(page.Events) != 1 || page.Events[0].Kind != session.EventSubagentChanged {
		t.Fatalf("subagent event page = %#v, %v", page, err)
	}
}

func TestIdleSubagentCompletionWaitsForNextUserRun(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = manager.Shutdown(context.Background())
		_ = store.Close()
	})
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_1234567890abcdef1234567890abcdef", CWD: root, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	mailbox := completeMailboxTask(t, store, record.ID, "scout result")
	manager.MailboxAdded(t.Context(), mailbox)
	providers.mu.Lock()
	calls := providers.calls
	providers.mu.Unlock()
	if calls != 0 {
		t.Fatalf("idle mailbox started %d provider calls", calls)
	}
	pending, err := store.PendingMailbox(t.Context(), record.ID, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending mailbox = %#v, %v", pending, err)
	}
	result, err := manager.RunPrompt(t.Context(), record.ID, "continue")
	if err != nil || result.Status != session.RunStatusCompleted {
		t.Fatalf("RunPrompt() = %#v, %v", result, err)
	}
	pending, err = store.PendingMailbox(t.Context(), record.ID, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after user run = %#v, %v", pending, err)
	}
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, message := range snapshot.Messages {
		if message.Role == "context" && message.BoundaryID == mailbox.ID && message.BoundaryKind == "subagent_result" {
			found = true
		}
	}
	if !found {
		t.Fatalf("snapshot does not contain delivered mailbox boundary: %#v", snapshot.Messages)
	}
}

func TestActiveSubagentCompletionArrivesAtSafeBoundary(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = manager.Shutdown(context.Background())
		_ = store.Close()
	})
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_99999999999999999999999999999999", CWD: root, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan session.PromptResult, 1)
	runErr := make(chan error, 1)
	go func() {
		outcome, err := manager.RunPrompt(context.Background(), record.ID, "work while scout finishes")
		if err != nil {
			runErr <- err
			return
		}
		result <- outcome
	}()
	select {
	case <-providers.started:
	case <-time.After(3 * time.Second):
		t.Fatal("parent provider did not start")
	}
	mailbox := completeMailboxTask(t, store, record.ID, "arrived while active")
	manager.MailboxAdded(t.Context(), mailbox)
	close(providers.block)
	select {
	case err := <-runErr:
		t.Fatal(err)
	case outcome := <-result:
		if outcome.Status != session.RunStatusCompleted || outcome.Text != "reply 2" {
			t.Fatalf("outcome = %#v", outcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("parent did not settle after mailbox delivery")
	}
	providers.mu.Lock()
	calls := providers.calls
	providers.mu.Unlock()
	if calls != 2 {
		t.Fatalf("provider calls = %d, want safe-boundary continuation", calls)
	}
	pending, err := store.PendingMailbox(t.Context(), record.ID, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending mailbox = %#v, %v", pending, err)
	}
}

func completeMailboxTask(t *testing.T, store *storage.Store, owner, summary string) subagent.MailboxItem {
	t.Helper()
	conversation, _, err := store.Admit(t.Context(), subagent.Admission{
		OwnerSessionID: owner,
		Definition: subagent.Definition{
			Name: "scout", Description: "finds things", Instructions: "Inspect.",
			Source: subagent.Source{Kind: subagent.SourceUser, Path: "/tmp/scout.md"},
		},
		CWD: "/tmp", Model: "test/echo", ThinkingLevel: "medium",
		Message: "inspect", Now: time.Now(),
	}, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, mailbox, err := store.Complete(t.Context(), subagent.Completion{
		TaskID: claim.Task.ID, ConversationID: conversation.ID,
		CancellationGeneration: claim.Task.CancellationGeneration,
		State:                  subagent.TaskCompleted, ResultSummary: summary,
	})
	if err != nil || mailbox == nil {
		t.Fatalf("Complete() = %#v, %v", mailbox, err)
	}
	return *mailbox
}
