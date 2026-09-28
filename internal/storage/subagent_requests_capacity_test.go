package storage

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/subagent"
)

func requestQueueLimits() subagent.Limits {
	return subagent.Limits{
		GlobalRunning: 1, PerSessionRunning: 1,
		PerConversationQueue: 1, PerSessionQueue: 2, GlobalQueue: 2,
	}
}

func TestFullRecipientQueueRetainsRequestUntilCapacityOpens(t *testing.T) {
	store, owner := newSubagentStore(t)
	limits := requestQueueLimits()
	conversation, parentTask, err := store.Admit(t.Context(), testAdmission(owner, "parent work"), limits)
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: conversation.Agent.Name,
		CallIdentity: "turn_parent/call_send", DeliveryMode: "send", Message: "wait for capacity",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("full recipient queue admission = %v, want no eligible delivery", err)
	}
	owners, err := store.PendingSubagentDeliveryOwners(t.Context(), 8)
	if err != nil || len(owners) != 1 || owners[0] != owner {
		t.Fatalf("delivery retained while full = %#v, %v", owners, err)
	}
	pending, err := store.InspectSubagentRequest(t.Context(), owner, "", request.ID)
	if err != nil || pending.State != subagent.RequestOpen {
		t.Fatalf("request retained while full = %#v, %v", pending, err)
	}
	claim, err := store.ClaimNext(t.Context(), owner, limits, time.Now())
	if err != nil || claim.Task.ID != parentTask.ID {
		t.Fatalf("claim original work = %#v, %v", claim, err)
	}
	inbox, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits)
	if err != nil || inbox.Origin != subagent.TaskOriginRequest || inbox.RequestID != request.ID || inbox.ConversationID != conversation.ID || inbox.State != subagent.TaskQueued {
		t.Fatalf("admitted request when queue opens = %#v, %v", inbox, err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("duplicate request admission = %v", err)
	}
	tasks, err := store.ListTasks(t.Context(), conversation.ID)
	if err != nil || len(tasks) != 2 || tasks[0].ID != parentTask.ID || tasks[1].ID != inbox.ID {
		t.Fatalf("FIFO after capacity opens = %#v, %v", tasks, err)
	}
}

func TestFullRecipientDoesNotBlockAnotherRecipientsRequest(t *testing.T) {
	store, owner := newSubagentStore(t)
	limits := requestQueueLimits()
	full, fullTask, err := store.Admit(t.Context(), testAdmission(owner, "parent work"), limits)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: full.Agent.Name,
		CallIdentity: "turn_parent/call_first", DeliveryMode: "send", Message: "full child",
	})
	if err != nil {
		t.Fatal(err)
	}
	definition := testAdmission(owner, "second child").Definition
	definition.Name = "other"
	second, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: "/tmp", Model: "test/model", CallIdentity: "turn_parent/call_second",
		DeliveryMode: "send", Message: "available child",
	})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits)
	if err != nil || admitted.RequestID != second.ID || admitted.ConversationID != second.RecipientConversationID {
		t.Fatalf("skip full recipient = %#v, %v", admitted, err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("first request while still full = %v", err)
	}
	claim, err := store.ClaimNext(t.Context(), owner, limits, time.Now())
	if err != nil || claim.Task.ID != fullTask.ID {
		t.Fatalf("claim full recipient work = %#v, %v", claim, err)
	}
	deferred, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits)
	if err != nil || deferred.RequestID != first.ID || deferred.ConversationID != full.ID {
		t.Fatalf("deferred request admission = %#v, %v", deferred, err)
	}
}

func TestReturnReplyWaitsForSenderQueueCapacity(t *testing.T) {
	store, owner := newSubagentStore(t)
	limits := requestQueueLimits()
	recipientAdmission := testAdmission(owner, "recipient parent work")
	recipientAdmission.Definition.Name = "recipient"
	recipient, recipientParent, err := store.Admit(t.Context(), recipientAdmission, limits)
	if err != nil {
		t.Fatal(err)
	}
	senderAdmission := testAdmission(owner, "sender parent work")
	senderAdmission.Definition.Name = "sender"
	sender, senderParent, err := store.Admit(t.Context(), senderAdmission, limits)
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: recipient.Agent.Name,
		CallIdentity: "turn_sender/call_send", DeliveryMode: "send", Message: "reply when ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimNext(t.Context(), owner, limits, time.Now())
	if err != nil || claim.Task.ID != recipientParent.ID {
		t.Fatalf("claim recipient parent work = %#v, %v", claim, err)
	}
	if _, _, err := store.Complete(t.Context(), subagent.Completion{
		TaskID: claim.Task.ID, ConversationID: recipient.ID,
		CancellationGeneration: claim.Task.CancellationGeneration, State: subagent.TaskCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	inbox, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits)
	if err != nil || inbox.RequestID != request.ID || inbox.Origin != subagent.TaskOriginRequest {
		t.Fatalf("recipient inbox = %#v, %v", inbox, err)
	}
	claim, err = store.ClaimNext(t.Context(), owner, limits, time.Now())
	if err != nil || claim.Task.ID != senderParent.ID {
		t.Fatalf("claim sender parent work = %#v, %v", claim, err)
	}
	senderAdmission.Message = "sender queued follow-up"
	senderAdmission.Now = time.Now()
	_, queuedSender, err := store.Admit(t.Context(), senderAdmission, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Complete(t.Context(), subagent.Completion{
		TaskID: claim.Task.ID, ConversationID: sender.ID,
		CancellationGeneration: claim.Task.CancellationGeneration, State: subagent.TaskCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	claim, err = store.ClaimNext(t.Context(), owner, limits, time.Now())
	if err != nil || claim.Task.ID != inbox.ID {
		t.Fatalf("recipient learns the receipt from inbox = %#v, %v", claim, err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), recipient.ID, request.ID, "turn_recipient/call_reply", "answer"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("return reply while sender queue is full = %v", err)
	}
	resolved, err := store.InspectSubagentRequest(t.Context(), owner, sender.ID, request.ID)
	if err != nil || resolved.State != subagent.RequestReplied || resolved.Reply != "answer" {
		t.Fatalf("reply retained while queue full = %#v, %v", resolved, err)
	}
	if _, _, err := store.Complete(t.Context(), subagent.Completion{
		TaskID: claim.Task.ID, ConversationID: recipient.ID,
		CancellationGeneration: claim.Task.CancellationGeneration, State: subagent.TaskCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	claim, err = store.ClaimNext(t.Context(), owner, limits, time.Now())
	if err != nil || claim.Task.ID != queuedSender.ID {
		t.Fatalf("claim sender queued work = %#v, %v", claim, err)
	}
	returnTask, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits)
	if err != nil || returnTask.Origin != subagent.TaskOriginReply || returnTask.RequestID != request.ID || returnTask.ConversationID != sender.ID || returnTask.Message != "Subagent reply to "+request.ID+" from "+recipient.Agent.Name+":\nanswer" {
		t.Fatalf("queued return reply = %#v, %v", returnTask, err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("duplicate return reply = %v", err)
	}
}

func TestOutstandingRecipientAndOwnerRequestLimits(t *testing.T) {
	store, owner := newSubagentStore(t)
	var first, last subagent.Request
	for recipientIndex := 0; recipientIndex < maxOutstandingRequestsPerOwner/maxOutstandingRequestsPerRecipient; recipientIndex++ {
		definition := testAdmission(owner, "work").Definition
		definition.Name = fmt.Sprintf("recipient-%d", recipientIndex)
		for i := 0; i < maxOutstandingRequestsPerRecipient; i++ {
			admission := subagent.RequestAdmission{
				OwnerSessionID: owner, RecipientName: definition.Name,
				CallIdentity: fmt.Sprintf("turn_parent/call_%d_%d", recipientIndex, i),
				DeliveryMode: "send", Message: "wait for reply",
			}
			if i == 0 {
				admission.NewRecipient = &definition
				admission.CWD, admission.Model = "/tmp", "test/model"
			}
			request, err := store.CreateSubagentRequest(t.Context(), admission)
			if err != nil {
				t.Fatalf("request %d/%d: %v", recipientIndex, i, err)
			}
			if recipientIndex == 0 && i == 0 {
				first = request
			}
			last = request
		}
		if recipientIndex == 0 {
			if _, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
				OwnerSessionID: owner, RecipientName: definition.Name,
				CallIdentity: "turn_parent/call_recipient_overflow", DeliveryMode: "send", Message: "overflow",
			}); !errors.Is(err, subagent.ErrQueueFull) {
				t.Fatalf("recipient overflow = %v", err)
			}
		}
	}
	// An idempotent retry is accepted even when the owner is at capacity.
	retry, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: first.RecipientName,
		CallIdentity: first.CallIdentity, DeliveryMode: first.DeliveryMode, Message: first.Message,
	})
	if err != nil || retry.ID != first.ID {
		t.Fatalf("retry at capacity = %#v, %v", retry, err)
	}
	extra := testAdmission(owner, "work").Definition
	extra.Name = "overflow"
	if _, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: extra.Name, NewRecipient: &extra,
		CWD: "/tmp", Model: "test/model", CallIdentity: "turn_parent/call_owner_overflow",
		DeliveryMode: "send", Message: "overflow",
	}); !errors.Is(err, subagent.ErrQueueFull) {
		t.Fatalf("owner overflow = %v", err)
	}
	if _, err := store.ConversationByAgent(t.Context(), owner, extra.Name); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("new child created by rejected request = %v", err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), last.RecipientConversationID, last.ID, "turn_child/call_reply", "answer"); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: extra.Name, NewRecipient: &extra,
		CWD: "/tmp", Model: "test/model", CallIdentity: "turn_parent/call_owner_overflow",
		DeliveryMode: "send", Message: "overflow",
	})
	if err != nil || recovered.State != subagent.RequestOpen {
		t.Fatalf("admission after capacity opens = %#v, %v", recovered, err)
	}
}

func TestRequestExpiresWhileRecipientQueueIsFull(t *testing.T) {
	store, owner := newSubagentStore(t)
	limits := requestQueueLimits()
	conversation, _, err := store.Admit(t.Context(), testAdmission(owner, "blocking parent work"), limits)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Minute)
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: conversation.Agent.Name,
		CallIdentity: "turn_parent/call_send", DeliveryMode: "send", Message: "time-limited question",
		Deadline: deadline,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("full recipient admission = %v", err)
	}
	if settled, err := store.SettleSubagentRequests(t.Context(), deadline.Add(time.Second), 8); err != nil || settled != 1 {
		t.Fatalf("expiration while blocked = %d, %v", settled, err)
	}
	if _, err := store.ClaimNext(t.Context(), owner, limits, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, limits); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("expired request admitted after capacity opened = %v", err)
	}
	results, err := store.PendingMailbox(t.Context(), owner, 8)
	if err != nil || len(results) != 1 || results[0].RequestID != request.ID || results[0].State != subagent.TaskFailed || results[0].Error != "request expired" {
		t.Fatalf("expired parent result = %#v, %v", results, err)
	}
}

func TestPerSessionAndGlobalQueueBoundsIndependentlyDeferRequests(t *testing.T) {
	store, firstOwner := newSubagentStore(t)
	secondOwner := "session_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := store.CreateSession(t.Context(), session.NewSession{
		ID: secondOwner, ScratchpadOwnerID: secondOwner, CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "model", ThinkingLevel: "medium",
	}); err != nil {
		t.Fatal(err)
	}
	limits := subagent.Limits{
		GlobalRunning: 1, PerSessionRunning: 1,
		PerConversationQueue: 2, PerSessionQueue: 2, GlobalQueue: 3,
	}
	first, firstTask, err := store.Admit(t.Context(), testAdmission(firstOwner, "first parent work"), limits)
	if err != nil {
		t.Fatal(err)
	}
	otherAdmission := testAdmission(firstOwner, "second parent work")
	otherAdmission.Definition.Name = "other"
	if _, _, err := store.Admit(t.Context(), otherAdmission, limits); err != nil {
		t.Fatal(err)
	}
	firstRequest, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: firstOwner, RecipientName: first.Agent.Name,
		CallIdentity: "turn_parent/call_first", DeliveryMode: "send", Message: "session bound",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), firstOwner, limits); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("session-bound request admission = %v", err)
	}
	second, secondTask, err := store.Admit(t.Context(), testAdmission(secondOwner, "second owner parent work"), limits)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: secondOwner, RecipientName: second.Agent.Name,
		CallIdentity: "turn_parent/call_second", DeliveryMode: "send", Message: "global bound",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), secondOwner, limits); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("global-bound request admission = %v", err)
	}
	claim, err := store.ClaimNext(t.Context(), firstOwner, limits, time.Now())
	if err != nil || claim.Task.ID != firstTask.ID {
		t.Fatalf("first owner claim = %#v, %v", claim, err)
	}
	admittedSecond, err := store.AdmitPendingSubagentDelivery(t.Context(), secondOwner, limits)
	if err != nil || admittedSecond.RequestID != secondRequest.ID {
		t.Fatalf("second owner advances after global slot opens = %#v, %v", admittedSecond, err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), firstOwner, limits); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("first owner still blocked by global bound = %v", err)
	}
	if _, _, err := store.Complete(t.Context(), subagent.Completion{
		TaskID: claim.Task.ID, ConversationID: first.ID,
		CancellationGeneration: claim.Task.CancellationGeneration, State: subagent.TaskCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	claim, err = store.ClaimNext(t.Context(), secondOwner, limits, time.Now())
	if err != nil || claim.Task.ID != secondTask.ID {
		t.Fatalf("second owner claim = %#v, %v", claim, err)
	}
	admittedFirst, err := store.AdmitPendingSubagentDelivery(t.Context(), firstOwner, limits)
	if err != nil || admittedFirst.RequestID != firstRequest.ID {
		t.Fatalf("first owner's delayed request admission = %#v, %v", admittedFirst, err)
	}
}
