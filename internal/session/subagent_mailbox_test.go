package session_test

import (
	"context"
	"fmt"
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
	deadline := time.Now().Add(time.Second)
	for {
		page, err := manager.Events(t.Context(), record.ID, snapshot.EventStreamID, snapshot.EventCursor)
		if err == nil && len(page.Events) == 1 && page.Events[0].Kind == session.EventSubagentChanged {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("subagent event page = %#v, %v", page, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestIdleSubagentCompletionStartsAutonomousParentReaction(t *testing.T) {
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
	deadline := time.Now().Add(3 * time.Second)
	for {
		pending, pendingErr := store.PendingMailbox(t.Context(), record.ID, 10)
		snapshot, snapshotErr := manager.Snapshot(t.Context(), record.ID)
		providers.mu.Lock()
		calls := providers.calls
		providers.mu.Unlock()
		if pendingErr == nil && snapshotErr == nil && len(pending) == 0 && snapshot.ActiveRunID == "" && calls == 1 {
			var boundary, assistant bool
			for _, message := range snapshot.Messages {
				boundary = boundary || message.Role == "context" && message.BoundaryID == mailbox.ID && message.BoundaryKind == "subagent_result"
				for _, content := range message.Content {
					assistant = assistant || message.Role == "assistant" && content.Kind == session.TranscriptContentText && content.Text == "reply 1"
				}
				if message.Role == "user" {
					t.Fatalf("autonomous reaction created a synthetic user message: %#v", message)
				}
			}
			if !boundary || !assistant {
				t.Fatalf("autonomous reaction transcript = %#v", snapshot.Messages)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("autonomous reaction did not settle: pending=%#v snapshot=%#v calls=%d errors=%v/%v", pending, snapshot, calls, pendingErr, snapshotErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPendingSubagentMailboxStartsReactionAfterManagerRestart(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	providers := &authorityProviders{}
	droidDirectory := filepath.Join(root, "droids")
	first, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	record, err := first.Create(t.Context(), session.CreateInput{
		ID: "session_abcdefabcdefabcdefabcdefabcdefab", CWD: root, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	mailbox := completeMailboxTask(t, store, record.ID, "restart result")
	second, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	providers.mu.Lock()
	callsBeforeStart := providers.calls
	providers.mu.Unlock()
	if callsBeforeStart != 0 {
		t.Fatalf("mailbox scanner started before explicit composition: %d calls", callsBeforeStart)
	}
	if err := second.StartSubagentMailbox(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = second.Shutdown(context.Background())
		_ = store.Close()
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		pending, pendingErr := store.PendingMailbox(t.Context(), record.ID, 10)
		snapshot, snapshotErr := second.Snapshot(t.Context(), record.ID)
		if pendingErr == nil && snapshotErr == nil && len(pending) == 0 && snapshot.ActiveRunID == "" {
			var found bool
			for _, message := range snapshot.Messages {
				found = found || message.Role == "context" && message.BoundaryID == mailbox.ID
			}
			if found {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart mailbox reaction did not settle: pending=%#v snapshot=%#v errors=%v/%v", pending, snapshot, pendingErr, snapshotErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAutonomousSubagentReactionsRespectGlobalLimit(t *testing.T) {
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
	owners := make([]string, 5)
	for index := range owners {
		owners[index] = fmt.Sprintf("session_%032x", index+1)
		if _, err := manager.Create(t.Context(), session.CreateInput{ID: owners[index], CWD: root, Model: "test/echo"}); err != nil {
			t.Fatal(err)
		}
		mailbox := completeMailboxTask(t, store, owners[index], "bounded reaction")
		manager.MailboxAdded(t.Context(), mailbox)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		providers.mu.Lock()
		calls := providers.calls
		providers.mu.Unlock()
		if calls == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("provider calls = %d, want four occupied reaction slots", calls)
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	providers.mu.Lock()
	calls := providers.calls
	providers.mu.Unlock()
	if calls != 4 {
		t.Fatalf("provider calls exceeded autonomous limit: %d", calls)
	}
	close(providers.block)
	deadline = time.Now().Add(3 * time.Second)
	for {
		providers.mu.Lock()
		calls = providers.calls
		providers.mu.Unlock()
		if calls == 5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fifth autonomous reaction did not start after slot release: calls=%d", calls)
		}
		time.Sleep(5 * time.Millisecond)
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
		if outcome.Status != session.RunStatusCompleted {
			t.Fatalf("outcome = %#v", outcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("parent did not settle after mailbox delivery")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		providers.mu.Lock()
		calls := providers.calls
		providers.mu.Unlock()
		pending, pendingErr := store.PendingMailbox(t.Context(), record.ID, 10)
		if calls == 2 && pendingErr == nil && len(pending) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("safe-boundary delivery did not settle: calls=%d pending=%#v error=%v", calls, pending, pendingErr)
		}
		time.Sleep(5 * time.Millisecond)
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
