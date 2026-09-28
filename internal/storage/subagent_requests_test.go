package storage

import (
	"errors"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/subagent"
)

func TestSubagentRequestAdmissionAndExplicitReply(t *testing.T) {
	store, owner := newSubagentStore(t)
	senderAdmission := testAdmission(owner, "research")
	senderAdmission.Definition.Name = "researcher"
	sender, _, err := store.Admit(t.Context(), senderAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	recipientAdmission := testAdmission(owner, "implement")
	recipientAdmission.Definition.Name = "implementer"
	recipient, _, err := store.Admit(t.Context(), recipientAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	admission := subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: "implementer",
		CallIdentity: "turn1/call1", DeliveryMode: "send", Message: "Check this finding",
		Now: time.Now(),
	}
	request, err := store.CreateSubagentRequest(t.Context(), admission)
	if err != nil {
		t.Fatal(err)
	}
	if request.SenderConversationID != sender.ID || request.RecipientConversationID != recipient.ID || request.State != subagent.RequestOpen {
		t.Fatalf("request = %#v", request)
	}
	replayed, err := store.CreateSubagentRequest(t.Context(), admission)
	if err != nil || replayed.ID != request.ID {
		t.Fatalf("replayed request = %#v, %v", replayed, err)
	}
	admission.Message = "different message"
	if _, err := store.CreateSubagentRequest(t.Context(), admission); !errors.Is(err, subagent.ErrConflict) {
		t.Fatalf("changed tool call error = %v", err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), sender.ID, request.ID, "turn2/call1", "wrong child"); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("unauthorized reply error = %v", err)
	}
	replied, err := store.ReplySubagentRequest(t.Context(), recipient.ID, request.ID, "turn2/call1", "Verified")
	if err != nil || replied.State != subagent.RequestReplied || replied.Reply != "Verified" {
		t.Fatalf("reply = %#v, %v", replied, err)
	}
	if replay, err := store.ReplySubagentRequest(t.Context(), recipient.ID, request.ID, "turn2/call1", "Verified"); err != nil || replay.ID != request.ID {
		t.Fatalf("replayed reply = %#v, %v", replay, err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), recipient.ID, request.ID, "turn2/call2", "Changed"); !errors.Is(err, subagent.ErrConflict) {
		t.Fatalf("second reply error = %v", err)
	}
	var deliveries int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM subagent_request_deliveries WHERE request_id = ?`, request.ID).Scan(&deliveries); err != nil || deliveries != 2 {
		t.Fatalf("deliveries = %d, %v", deliveries, err)
	}
}

func TestSubagentRequestRejectsSelfAndOtherOwner(t *testing.T) {
	store, owner := newSubagentStore(t)
	conversation, _, err := store.Admit(t.Context(), testAdmission(owner, "work"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	admission := subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: conversation.ID, RecipientName: conversation.Agent.Name,
		CallIdentity: "turn/call", DeliveryMode: "send", Message: "self",
	}
	if _, err := store.CreateSubagentRequest(t.Context(), admission); !errors.Is(err, subagent.ErrInvalidInput) {
		t.Fatalf("self-send error = %v", err)
	}
	admission.SenderConversationID = "subagent_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := store.CreateSubagentRequest(t.Context(), admission); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("other sender error = %v", err)
	}
}

func TestSubagentRequestDeliveryUsesChildQueueWithoutParentMailbox(t *testing.T) {
	store, owner := newSubagentStore(t)
	senderAdmission := testAdmission(owner, "research")
	senderAdmission.Definition.Name = "researcher"
	sender, _, err := store.Admit(t.Context(), senderAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	recipientAdmission := testAdmission(owner, "implement")
	recipientAdmission.Definition.Name = "implementer"
	recipient, _, err := store.Admit(t.Context(), recipientAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: "implementer",
		CallIdentity: "turn1/call1", DeliveryMode: "send", Message: "Check this finding",
	})
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits())
	if err != nil || incoming.ConversationID != recipient.ID || incoming.Origin != subagent.TaskOriginRequest || incoming.RequestID != request.ID {
		t.Fatalf("incoming = %#v, %v", incoming, err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits()); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("duplicate delivery = %v", err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), recipient.ID, request.ID, "turn2/call1", "Verified"); err != nil {
		t.Fatal(err)
	}
	outgoing, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits())
	if err != nil || outgoing.ConversationID != sender.ID || outgoing.Origin != subagent.TaskOriginReply || outgoing.RequestID != request.ID {
		t.Fatalf("return delivery = %#v, %v", outgoing, err)
	}
	// The ordinary child task ahead of it must settle before the inbox task can run.
	claim, err := store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.Complete(t.Context(), subagent.Completion{
		TaskID: claim.Task.ID, ConversationID: claim.Task.ConversationID,
		CancellationGeneration: claim.Task.CancellationGeneration, State: subagent.TaskCompleted,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Mark the earlier parent task in the other conversation complete too.
	claim, err = store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.Complete(t.Context(), subagent.Completion{
		TaskID: claim.Task.ID, ConversationID: claim.Task.ConversationID,
		CancellationGeneration: claim.Task.CancellationGeneration, State: subagent.TaskCompleted,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		claim, err = store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		completed, mailbox, err := store.Complete(t.Context(), subagent.Completion{
			TaskID: claim.Task.ID, ConversationID: claim.Task.ConversationID,
			CancellationGeneration: claim.Task.CancellationGeneration, State: subagent.TaskCompleted,
		})
		if err != nil || completed.Origin == subagent.TaskOriginParent || mailbox != nil {
			t.Fatalf("inbox completion = %#v mailbox = %#v, %v", completed, mailbox, err)
		}
	}
}

func TestParentSendCreatesRecipientAndRoutesOnlyExplicitReply(t *testing.T) {
	store, owner := newSubagentStore(t)
	definition := testAdmission(owner, "parent request").Definition
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: "/tmp", Model: "test/model", ThinkingLevel: "medium",
		CallIdentity: "turn_parent/call_send", DeliveryMode: "send", Message: "Investigate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.SenderKind != "parent" || request.RecipientConversationID == "" {
		t.Fatalf("request = %#v", request)
	}
	if items, err := store.PendingMailbox(t.Context(), owner, 8); err != nil || len(items) != 0 {
		t.Fatalf("premature result = %#v, %v", items, err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), request.RecipientConversationID, request.ID, "turn_child/call_reply", "Answer"); err != nil {
		t.Fatal(err)
	}
	beforeDelivery, err := store.InspectSubagentRequest(t.Context(), owner, "", request.ID)
	if err != nil || !beforeDelivery.DeliveryPending || beforeDelivery.Reply != "" || beforeDelivery.State != subagent.RequestReplied {
		t.Fatalf("inspection before mailbox admission = %#v, %v", beforeDelivery, err)
	}
	items, err := store.PendingMailbox(t.Context(), owner, 8)
	if err != nil || len(items) != 1 || items[0].RequestID != request.ID || items[0].Summary != "Answer" || items[0].AgentName != definition.Name {
		t.Fatalf("parent result = %#v, %v", items, err)
	}
	owners, err := store.PendingMailboxOwners(t.Context(), "", 8)
	if err != nil || len(owners) != 1 || owners[0] != owner {
		t.Fatalf("pending owners = %v, %v", owners, err)
	}
	if err := store.MarkMailboxDelivered(t.Context(), []string{parentRequestDeliveryID(request.ID)}, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	afterDelivery, err := store.InspectSubagentRequest(t.Context(), owner, "", request.ID)
	if err != nil || afterDelivery.DeliveryPending || afterDelivery.Reply != "Answer" {
		t.Fatalf("inspection after mailbox admission = %#v, %v", afterDelivery, err)
	}
	if items, err := store.PendingMailbox(t.Context(), owner, 8); err != nil || len(items) != 0 {
		t.Fatalf("delivered result = %#v, %v", items, err)
	}
}

func TestParentAskReplyHasNoCompletionMailbox(t *testing.T) {
	store, owner := newSubagentStore(t)
	definition := testAdmission(owner, "parent request").Definition
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: "/tmp", Model: "test/model", CallIdentity: "turn_parent/call_ask",
		DeliveryMode: "ask", Message: "Investigate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), request.RecipientConversationID, request.ID, "turn_child/call_reply", "Answer"); err != nil {
		t.Fatal(err)
	}
	read, err := store.InspectSubagentRequest(t.Context(), owner, "", request.ID)
	if err != nil || read.Reply != "Answer" || read.State != subagent.RequestReplied {
		t.Fatalf("ask result = %#v, %v", read, err)
	}
	if items, err := store.PendingMailbox(t.Context(), owner, 8); err != nil || len(items) != 0 {
		t.Fatalf("ask mailbox = %#v, %v", items, err)
	}
}

func TestChildRequestInitializesConfiguredUnstartedRecipient(t *testing.T) {
	store, owner := newSubagentStore(t)
	senderAdmission := testAdmission(owner, "sender work")
	senderAdmission.Definition.Name = "sender"
	sender, _, err := store.Admit(t.Context(), senderAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	definition := testAdmission(owner, "recipient work").Definition
	definition.Name = "recipient"
	admission := subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: definition.Name,
		NewRecipient: &definition, CWD: "/tmp", Model: "test/model", ThinkingLevel: "medium",
		CallIdentity: "turn_sender/call_send", DeliveryMode: "send", Message: "question",
	}
	request, err := store.CreateSubagentRequest(t.Context(), admission)
	if err != nil || request.SenderConversationID != sender.ID || request.RecipientConversationID == "" {
		t.Fatalf("initial child send = %#v, %v", request, err)
	}
	recipient, err := store.Conversation(t.Context(), request.RecipientConversationID)
	if err != nil || recipient.Agent != definition || recipient.CWD != admission.CWD || recipient.Model != admission.Model || recipient.ThinkingLevel != admission.ThinkingLevel {
		t.Fatalf("initialized recipient = %#v, %v", recipient, err)
	}
	retry, err := store.CreateSubagentRequest(t.Context(), admission)
	if err != nil || retry.ID != request.ID {
		t.Fatalf("retried send = %#v, %v", retry, err)
	}
	inbox, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits())
	if err != nil || inbox.RequestID != request.ID || inbox.ConversationID != recipient.ID || inbox.Origin != subagent.TaskOriginRequest {
		t.Fatalf("new recipient inbox = %#v, %v", inbox, err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), recipient.ID, request.ID, "turn_recipient/call_reply", "answer"); err != nil {
		t.Fatal(err)
	}
	returned, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits())
	if err != nil || returned.ConversationID != sender.ID || returned.RequestID != request.ID || returned.Origin != subagent.TaskOriginReply {
		t.Fatalf("return to sender = %#v, %v", returned, err)
	}
}

func TestChildRequestCannotReinitializeDismissedRecipient(t *testing.T) {
	store, owner := newSubagentStore(t)
	senderAdmission := testAdmission(owner, "sender work")
	senderAdmission.Definition.Name = "sender"
	sender, _, err := store.Admit(t.Context(), senderAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	recipientAdmission := testAdmission(owner, "recipient work")
	recipientAdmission.Definition.Name = "recipient"
	recipient, _, err := store.Admit(t.Context(), recipientAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Dismiss(t.Context(), recipient.ID, recipient.Generation, "dismissed", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: recipient.Agent.Name,
		NewRecipient: &recipientAdmission.Definition, CWD: "/tmp", Model: "test/model",
		CallIdentity: "turn_sender/call_send", DeliveryMode: "send", Message: "question",
	}); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("dismissed sibling reinitialized: %v", err)
	}
	if active, err := store.ListConversations(t.Context(), owner); err != nil || len(active) != 1 || active[0].ID != sender.ID {
		t.Fatalf("active children after rejected initialization = %#v, %v", active, err)
	}
}

func TestConcurrentChildRequestsInitializeOneRecipient(t *testing.T) {
	store, owner := newSubagentStore(t)
	definition := testAdmission(owner, "recipient work").Definition
	definition.Name = "recipient"
	var senders []subagent.Conversation
	for _, name := range []string{"sender-a", "sender-b"} {
		admission := testAdmission(owner, "sender work")
		admission.Definition.Name = name
		sender, _, err := store.Admit(t.Context(), admission, subagent.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		senders = append(senders, sender)
	}
	start := make(chan struct{})
	type result struct {
		request subagent.Request
		err     error
	}
	results := make(chan result, len(senders))
	for _, sender := range senders {
		go func() {
			<-start
			request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
				OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: definition.Name,
				NewRecipient: &definition, CWD: "/tmp", Model: "test/model",
				CallIdentity: "turn_sender/call_send", DeliveryMode: "send", Message: "question",
			})
			results <- result{request, err}
		}()
	}
	close(start)
	var requests []subagent.Request
	for range senders {
		res := <-results
		if res.err != nil {
			t.Fatalf("concurrent initialization = %v", res.err)
		}
		requests = append(requests, res.request)
	}
	if requests[0].RecipientConversationID == "" || requests[0].RecipientConversationID != requests[1].RecipientConversationID || requests[0].ID == requests[1].ID {
		t.Fatalf("two requests to one initialized child = %#v", requests)
	}
	conversations, err := store.ListConversations(t.Context(), owner)
	if err != nil || len(conversations) != 3 {
		t.Fatalf("one recipient conversation = %#v, %v", conversations, err)
	}
}
