package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/subagent"
)

func TestSubagentRequestSurvivesStoreReopenBeforeAndAfterReply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kit.db")
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			_ = store.Close()
		}
	})
	owner := "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err = store.CreateSession(t.Context(), session.NewSession{
		ID: owner, ScratchpadOwnerID: owner, CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "model", ThinkingLevel: "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	definition := testAdmission(owner, "work").Definition
	admission := subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: "/tmp", Model: "test/model", CallIdentity: "turn_parent/call_send",
		DeliveryMode: "send", Message: "Check this after restart",
	}
	request, err := store.CreateSubagentRequest(t.Context(), admission)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	owners, err := store.PendingSubagentDeliveryOwners(t.Context(), 8)
	if err != nil || len(owners) != 1 || owners[0] != owner {
		t.Fatalf("pending delivery owners = %v, %v", owners, err)
	}
	replayed, err := store.CreateSubagentRequest(t.Context(), admission)
	if err != nil || replayed.ID != request.ID {
		t.Fatalf("retried request = %#v, %v", replayed, err)
	}
	inbox, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits())
	if err != nil || inbox.Origin != subagent.TaskOriginRequest || inbox.RequestID != request.ID {
		t.Fatalf("recovered inbox task = %#v, %v", inbox, err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), request.RecipientConversationID, request.ID, "turn_child/call_reply", "Verified"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	replies, err := store.PendingMailbox(t.Context(), owner, 8)
	if err != nil || len(replies) != 1 || replies[0].RequestID != request.ID || replies[0].Summary != "Verified" {
		t.Fatalf("recovered parent reply = %#v, %v", replies, err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), request.RecipientConversationID, request.ID, "turn_child/call_reply", "Verified"); err != nil {
		t.Fatalf("replayed reply after reopen: %v", err)
	}
	replies, err = store.PendingMailbox(t.Context(), owner, 8)
	if err != nil || len(replies) != 1 || replies[0].RequestID != request.ID {
		t.Fatalf("duplicate parent reply = %#v, %v", replies, err)
	}
	if err := store.MarkMailboxDelivered(t.Context(), []string{parentRequestDeliveryID(request.ID)}, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	replies, err = store.PendingMailbox(t.Context(), owner, 8)
	if err != nil || len(replies) != 0 {
		t.Fatalf("acknowledged parent replies = %#v, %v", replies, err)
	}
}

func TestInterruptedInboxTaskLeavesRequestOpenUntilExpiry(t *testing.T) {
	store, owner := newSubagentStore(t)
	definition := testAdmission(owner, "work").Definition
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: "/tmp", Model: "test/model", CallIdentity: "turn_parent/call_send",
		DeliveryMode: "send", Message: "answer only when ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
	if err != nil || claim.Task.ID != inbox.ID {
		t.Fatalf("claimed inbox = %#v, %v", claim, err)
	}
	recovery, err := store.RecoverRunning(t.Context(), time.Now())
	if err != nil || len(recovery.InterruptedTasks) != 1 || recovery.InterruptedTasks[0].ID != inbox.ID {
		t.Fatalf("recovery = %#v, %v", recovery, err)
	}
	unanswered, err := store.InspectSubagentRequest(t.Context(), owner, "", request.ID)
	if err != nil || unanswered.State != subagent.RequestOpen {
		t.Fatalf("unanswered request = %#v, %v", unanswered, err)
	}
	completions, err := store.PendingMailbox(t.Context(), owner, 8)
	if err != nil || len(completions) != 0 {
		t.Fatalf("parent completion for inbox interruption = %#v, %v", completions, err)
	}
	settled, err := store.SettleSubagentRequests(t.Context(), request.DeadlineAt.Add(time.Second), 8)
	if err != nil || settled != 1 {
		t.Fatalf("expired requests = %d, %v", settled, err)
	}
	results, err := store.PendingMailbox(t.Context(), owner, 8)
	if err != nil || len(results) != 1 || results[0].RequestID != request.ID || results[0].State != subagent.TaskFailed || results[0].Error != "request expired" {
		t.Fatalf("expiry result = %#v, %v", results, err)
	}
	if settled, err := store.SettleSubagentRequests(t.Context(), request.DeadlineAt.Add(2*time.Second), 8); err != nil || settled != 0 {
		t.Fatalf("repeated settlement = %d, %v", settled, err)
	}
}
