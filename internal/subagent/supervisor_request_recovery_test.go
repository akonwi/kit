package subagent_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
)

func TestSupervisorAdmitsPendingInboxAfterStoreReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kit.db")
	store, err := storage.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			_ = store.Close()
		}
	})
	owner := createSupervisorOwner(t, store, "request-recovery")
	conversation, parentTask, err := store.Admit(t.Context(), supervisorAdmission(owner, "original parent work", time.Now()), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
	if err != nil || claim.Task.ID != parentTask.ID {
		t.Fatalf("initial claim = %#v, %v", claim, err)
	}
	if _, _, err := store.Complete(t.Context(), subagent.Completion{
		TaskID: claim.Task.ID, ConversationID: conversation.ID,
		CancellationGeneration: claim.Task.CancellationGeneration, State: subagent.TaskCompleted,
		ResultSummary: "initial task complete",
	}); err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: conversation.Agent.Name,
		CallIdentity: "turn_parent/call_send", DeliveryMode: "send", Message: "inbox after restart",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	factory := newGatedChildFactory()
	supervisor, err := subagent.NewSupervisor(store, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	started := waitStarted(t, factory.started)
	inbox, err := store.Task(t.Context(), started)
	if err != nil || inbox.Origin != subagent.TaskOriginRequest || inbox.RequestID != request.ID || inbox.Message == "" {
		t.Fatalf("recovered inbox = %#v, %v", inbox, err)
	}
	factory.release(started)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := supervisor.WaitConversation(ctx, conversation.ID); err != nil {
		t.Fatal(err)
	}
	finished, err := store.Task(t.Context(), inbox.ID)
	if err != nil || finished.State != subagent.TaskCompleted {
		t.Fatalf("finished inbox = %#v, %v", finished, err)
	}
	pending, err := store.PendingSubagentDeliveryOwners(t.Context(), 8)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending delivery owners = %#v, %v", pending, err)
	}
	mailbox, err := store.PendingMailbox(t.Context(), owner, 8)
	if err != nil || len(mailbox) != 1 || mailbox[0].TaskID != parentTask.ID {
		t.Fatalf("parent completion mailbox = %#v, %v", mailbox, err)
	}
	read, err := store.InspectSubagentRequest(t.Context(), owner, "", request.ID)
	if err != nil || read.State != subagent.RequestOpen {
		t.Fatalf("unanswered request = %#v, %v", read, err)
	}
}

func TestDismissingRunningRequestInboxSettlesParentFailure(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "dismiss-request")
	definition := supervisorAdmission(owner, "parent work", time.Now()).Definition
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: t.TempDir(), Model: "test/echo", CallIdentity: "turn_parent/call_send",
		DeliveryMode: "send", Message: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	factory := newGatedChildFactory()
	supervisor, err := subagent.NewSupervisor(store, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	inboxID := waitStarted(t, factory.started)
	inbox, err := store.Task(t.Context(), inboxID)
	if err != nil || inbox.Origin != subagent.TaskOriginRequest || inbox.RequestID != request.ID {
		t.Fatalf("running request inbox = %#v, %v", inbox, err)
	}
	recipient, err := store.Conversation(t.Context(), request.RecipientConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Dismiss(t.Context(), recipient.ID, recipient.Generation, "deleted"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		results, err := store.PendingMailbox(t.Context(), owner, 8)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) == 1 && results[0].RequestID == request.ID && results[0].State == subagent.TaskFailed && results[0].Error == "recipient unavailable" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dismissed recipient failure not routed: %#v", results)
		}
		time.Sleep(5 * time.Millisecond)
	}
	finished, err := store.Task(t.Context(), inboxID)
	if err != nil || finished.State != subagent.TaskAborted {
		t.Fatalf("dismissed inbox task = %#v, %v", finished, err)
	}
	if mailbox, err := store.PendingMailbox(t.Context(), owner, 8); err != nil || len(mailbox) != 1 || mailbox[0].Kind != subagent.ParentDeliveryRequest || mailbox[0].RequestID != request.ID {
		t.Fatalf("request failure delivery after dismiss = %#v, %v", mailbox, err)
	}
}

func TestSupervisorRetriesRequestInboxAfterQueueCapacityOpens(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "request-capacity")
	limits := subagent.Limits{GlobalRunning: 1, PerSessionRunning: 1, PerConversationQueue: 1, PerSessionQueue: 1, GlobalQueue: 1}
	factory := newGatedChildFactory()
	supervisor, err := subagent.NewSupervisor(store, factory, limits, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
	conversation, parentTask, err := store.Admit(t.Context(), supervisorAdmission(owner, "parent work", time.Now()), limits)
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: conversation.Agent.Name,
		CallIdentity: "turn_parent/call_send", DeliveryMode: "send", Message: "wait in inbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits); err != subagent.ErrNotFound {
		t.Fatalf("full conversation queue admission = %v, want not found", err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if started := waitStarted(t, factory.started); started != parentTask.ID {
		t.Fatalf("first work = %s, want %s", started, parentTask.ID)
	}
	factory.release(parentTask.ID)
	inboxID := waitStarted(t, factory.started)
	inbox, err := store.Task(t.Context(), inboxID)
	if err != nil || inbox.Origin != subagent.TaskOriginRequest || inbox.RequestID != request.ID || inbox.ConversationID != conversation.ID {
		t.Fatalf("retried inbox = %#v, %v", inbox, err)
	}
	factory.release(inboxID)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := supervisor.WaitTask(ctx, inboxID); err != nil {
		t.Fatal(err)
	}
	tasks, err := supervisor.ListTasks(t.Context(), conversation.ID)
	if err != nil || len(tasks) != 2 || tasks[0].ID != parentTask.ID || tasks[1].ID != inboxID || tasks[1].State != subagent.TaskCompleted {
		t.Fatalf("single retried inbox task = %#v, %v", tasks, err)
	}
	owners, err := store.PendingSubagentDeliveryOwners(t.Context(), 8)
	if err != nil || len(owners) != 0 {
		t.Fatalf("unadmitted request after capacity opens = %#v, %v", owners, err)
	}
	mailbox, err := store.PendingMailbox(t.Context(), owner, 8)
	if err != nil || len(mailbox) != 1 || mailbox[0].TaskID != parentTask.ID {
		t.Fatalf("parent completion from inbox = %#v, %v", mailbox, err)
	}
}
