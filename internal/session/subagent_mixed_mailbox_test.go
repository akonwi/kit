package session_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
)

func TestAlternatingParentDeliveriesRemainBoundedAcrossTurns(t *testing.T) {
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
	owner, err := manager.Create(t.Context(), session.CreateInput{ID: "session_33333333333333333333333333333333", CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	definition := subagent.Definition{
		Name: "reviewer", Description: "answers requests", Instructions: "Reply explicitly.",
		Source: subagent.Source{Kind: subagent.SourceUser, Path: "/tmp/reviewer.md"},
	}
	for i := 0; i < 33; i++ {
		completeMailboxTask(t, store, owner.ID, fmt.Sprintf("task %d", i))
		admission := subagent.RequestAdmission{
			OwnerSessionID: owner.ID, RecipientName: definition.Name,
			CWD: root, Model: "test/echo", DeliveryMode: "send", CallIdentity: fmt.Sprintf("turn_parent/call_%d", i), Message: "question",
		}
		if i == 0 {
			admission.NewRecipient = &definition
		}
		request, err := store.CreateSubagentRequest(t.Context(), admission)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReplySubagentRequest(t.Context(), request.RecipientConversationID, request.ID, fmt.Sprintf("turn_child/call_%d", i), "answer"); err != nil {
			t.Fatal(err)
		}
	}
	for i, expectedRemaining := range []int{34, 2, 0} {
		outcome, err := manager.RunPrompt(t.Context(), owner.ID, fmt.Sprintf("drain batch %d", i))
		if err != nil || outcome.Status != session.RunStatusCompleted {
			t.Fatalf("parent turn %d = %#v, %v", i, outcome, err)
		}
		pending, err := store.PendingMailbox(t.Context(), owner.ID, 128)
		if err != nil || len(pending) != expectedRemaining {
			t.Fatalf("pending after turn %d = %d, want %d: %v", i, len(pending), expectedRemaining, err)
		}
	}
	providers.mu.Lock()
	calls := providers.calls
	providers.mu.Unlock()
	if calls != 3 {
		t.Fatalf("parent reactions = %d, want 3", calls)
	}
}

func TestMixedParentDeliveriesUseSeparateBoundariesInOneTurn(t *testing.T) {
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
	owner, err := manager.Create(t.Context(), session.CreateInput{ID: "session_22222222222222222222222222222222", CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	task := completeMailboxTask(t, store, owner.ID, "task result")
	definition := subagent.Definition{
		Name: "reviewer", Description: "answers requests", Instructions: "Reply explicitly.",
		Source: subagent.Source{Kind: subagent.SourceUser, Path: "/tmp/reviewer.md"},
	}
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner.ID, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: root, Model: "test/echo", DeliveryMode: "send", CallIdentity: "turn_parent/call_mixed", Message: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), request.RecipientConversationID, request.ID, "turn_child/call_mixed", "request result"); err != nil {
		t.Fatal(err)
	}
	pending, err := store.PendingMailbox(t.Context(), owner.ID, 10)
	if err != nil || len(pending) != 2 || pending[0].ID != task.ID || pending[1].Kind != subagent.ParentDeliveryRequest {
		t.Fatalf("mixed pending deliveries = %#v, %v", pending, err)
	}
	before, err := manager.Snapshot(t.Context(), owner.ID)
	if err != nil || len(before.SubagentMailbox) != 2 || before.SubagentMailbox[0].Kind != subagent.ParentDeliveryTask || before.SubagentMailbox[0].ID != task.ID || before.SubagentMailbox[1].Kind != subagent.ParentDeliveryRequest || before.SubagentMailbox[1].ID != pending[1].ID {
		t.Fatalf("mixed pending snapshot = %#v, %v", before.SubagentMailbox, err)
	}
	outcome, err := manager.RunPrompt(t.Context(), owner.ID, "consider both results")
	if err != nil || outcome.Status != session.RunStatusCompleted {
		t.Fatalf("parent turn = %#v, %v", outcome, err)
	}
	after, err := manager.Snapshot(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	var boundaries []session.TranscriptMessage
	for _, message := range after.Messages {
		if message.Role == "context" {
			boundaries = append(boundaries, message)
		}
	}
	if len(boundaries) != 2 || boundaries[0].BoundaryID != task.ID || boundaries[0].BoundaryKind != "subagent_result" ||
		boundaries[1].BoundaryID != pending[1].ID || boundaries[1].BoundaryKind != "subagent_request_result" {
		t.Fatalf("mixed turn boundaries = %#v", boundaries)
	}
	remaining, err := store.PendingMailbox(t.Context(), owner.ID, 10)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("mixed turn acknowledgements = %#v, %v", remaining, err)
	}
}
