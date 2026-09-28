package session_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
)

type unavailableMailboxAcknowledgement struct{ *storage.Store }

func (s unavailableMailboxAcknowledgement) MarkMailboxDelivered(context.Context, []string, uint64, time.Time) error {
	return errors.New("simulated acknowledgement outage")
}

func TestConsumedParentRequestDeliveryReconcilesAfterManagerReconstruction(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	droids := filepath.Join(root, "droids")
	first, err := session.NewManager(unavailableMailboxAcknowledgement{store}, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droids))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Shutdown(context.Background()) })
	owner, err := first.Create(t.Context(), session.CreateInput{ID: "session_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	definition := subagent.Definition{
		Name: "researcher", Description: "answers requests", Instructions: "Reply explicitly.",
		Source: subagent.Source{Kind: subagent.SourceUser, Path: "/tmp/researcher.md"},
	}
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner.ID, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: root, Model: "test/echo", DeliveryMode: "send", CallIdentity: "turn_parent/call_ack_failure", Message: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), request.RecipientConversationID, request.ID, "turn_child/call_ack_failure", "durable answer"); err != nil {
		t.Fatal(err)
	}
	if result, err := first.RunPrompt(t.Context(), owner.ID, "consume answer"); err != nil || result.Status != session.RunStatusCompleted {
		t.Fatalf("first parent turn = %#v, %v", result, err)
	}
	pending, err := store.PendingMailbox(t.Context(), owner.ID, 8)
	id := "mail_" + strings.TrimPrefix(request.ID, "subrequest_")
	if err != nil || len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("acknowledgement outage pending result = %#v, %v", pending, err)
	}
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	second, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droids))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Shutdown(context.Background()) })
	if err := second.StartSubagentMailbox(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		pending, pendingErr := store.PendingMailbox(t.Context(), owner.ID, 8)
		providers.mu.Lock()
		calls := providers.calls
		providers.mu.Unlock()
		if pendingErr == nil && len(pending) == 0 && calls == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("consumed result reconciled: pending=%#v, calls=%d, error=%v", pending, calls, pendingErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot, err := second.Snapshot(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	var seen int
	for _, message := range snapshot.Messages {
		if message.Role == "context" && message.BoundaryID == id && message.BoundaryKind == "subagent_request_result" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("reconstructed request-result boundary count = %d: %#v", seen, snapshot.Messages)
	}
}
