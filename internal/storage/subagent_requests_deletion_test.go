package storage

import (
	"errors"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/subagent"
)

func TestDismissedRecipientSettlesParentRequest(t *testing.T) {
	for _, mode := range []string{"send", "ask"} {
		t.Run(mode, func(t *testing.T) {
			store, owner := newSubagentStore(t)
			definition := testAdmission(owner, "work").Definition
			request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
				OwnerSessionID: owner, RecipientName: definition.Name, NewRecipient: &definition,
				CWD: "/tmp", Model: "test/model", CallIdentity: "turn_parent/call_" + mode,
				DeliveryMode: mode, Message: "question for recipient",
			})
			if err != nil {
				t.Fatal(err)
			}
			recipient, err := store.Conversation(t.Context(), request.RecipientConversationID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Dismiss(t.Context(), recipient.ID, recipient.Generation, "deleted", time.Now()); err != nil {
				t.Fatal(err)
			}
			assertDismissedRequestSettled(t, store, request, owner)
			if _, err := store.ReplySubagentRequest(t.Context(), recipient.ID, request.ID, "turn_child/call_reply", "late answer"); !errors.Is(err, subagent.ErrConflict) {
				t.Fatalf("late reply = %v, want conflict", err)
			}
			results, err := store.PendingMailbox(t.Context(), owner, 8)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "ask" {
				if len(results) != 0 {
					t.Fatalf("ask has no asynchronous result = %#v", results)
				}
				return
			}
			if len(results) != 1 || results[0].RequestID != request.ID || results[0].State != subagent.TaskFailed || results[0].Error != "recipient unavailable" {
				t.Fatalf("parent failure result = %#v", results)
			}
			if err := store.MarkMailboxDelivered(t.Context(), []string{parentRequestDeliveryID(request.ID)}, 1, time.Now()); err != nil {
				t.Fatal(err)
			}
			if results, err := store.PendingMailbox(t.Context(), owner, 8); err != nil || len(results) != 0 {
				t.Fatalf("acknowledged failure = %#v, %v", results, err)
			}
			read, err := store.InspectSubagentRequest(t.Context(), owner, "", request.ID)
			if err != nil || read.DeliveryPending || read.Failure != "recipient unavailable" {
				t.Fatalf("failure after mailbox delivery = %#v, %v", read, err)
			}
		})
	}
}

func TestDismissedRecipientRoutesFailureToSurvivingSibling(t *testing.T) {
	store, owner := newSubagentStore(t)
	senderAdmission := testAdmission(owner, "sender parent work")
	senderAdmission.Definition.Name = "sender"
	sender, _, err := store.Admit(t.Context(), senderAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	recipientAdmission := testAdmission(owner, "recipient parent work")
	recipientAdmission.Definition.Name = "recipient"
	recipient, _, err := store.Admit(t.Context(), recipientAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: recipient.Agent.Name,
		CallIdentity: "turn_sender/call_send", DeliveryMode: "send", Message: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Dismiss(t.Context(), recipient.ID, recipient.Generation, "deleted", time.Now()); err != nil {
		t.Fatal(err)
	}
	assertDismissedRequestSettled(t, store, request, owner)
	owners, err := store.PendingSubagentDeliveryOwners(t.Context(), 8)
	if err != nil || len(owners) != 1 || owners[0] != owner {
		t.Fatalf("pending surviving sibling return delivery owners = %#v, %v", owners, err)
	}
	returnTask, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits())
	if err != nil || returnTask.Origin != subagent.TaskOriginReply || returnTask.RequestID != request.ID || returnTask.ConversationID != sender.ID || returnTask.Message != "Subagent request "+request.ID+" failed: recipient unavailable" {
		t.Fatalf("sibling failure inbox = %#v, %v", returnTask, err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits()); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("duplicate failure delivery = %v", err)
	}
	if results, err := store.PendingMailbox(t.Context(), owner, 8); err != nil || len(results) != 0 {
		t.Fatalf("sibling failure routed to parent = %#v, %v", results, err)
	}
}

func TestDismissedSenderCannotReceiveLaterReply(t *testing.T) {
	store, owner := newSubagentStore(t)
	senderAdmission := testAdmission(owner, "sender parent work")
	senderAdmission.Definition.Name = "sender"
	sender, _, err := store.Admit(t.Context(), senderAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	recipientAdmission := testAdmission(owner, "recipient parent work")
	recipientAdmission.Definition.Name = "recipient"
	recipient, _, err := store.Admit(t.Context(), recipientAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: recipient.Agent.Name,
		CallIdentity: "turn_sender/call_send", DeliveryMode: "send", Message: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits())
	if err != nil || incoming.Origin != subagent.TaskOriginRequest || incoming.ConversationID != recipient.ID {
		t.Fatalf("recipient request inbox = %#v, %v", incoming, err)
	}
	answered, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: recipient.Agent.Name,
		CallIdentity: "turn_sender/call_answered", DeliveryMode: "send", Message: "already answered",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), recipient.ID, answered.ID, "turn_recipient/call_answered", "answer"); err != nil {
		t.Fatal(err)
	}
	changed, err := store.Dismiss(t.Context(), sender.ID, sender.Generation, "deleted", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var notified bool
	for _, task := range changed {
		notified = notified || task.ID == incoming.ID && task.State == subagent.TaskAborted
	}
	if !notified {
		t.Fatalf("dismissal did not publish canceled recipient inbox: %#v", changed)
	}
	canceledInbox, err := store.Task(t.Context(), incoming.ID)
	if err != nil || canceledInbox.State != subagent.TaskAborted || canceledInbox.Error != "sender unavailable" {
		t.Fatalf("queued request to dismissed sender = %#v, %v", canceledInbox, err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), recipient.ID, request.ID, "turn_recipient/call_reply", "late answer"); !errors.Is(err, subagent.ErrConflict) {
		t.Fatalf("reply to dismissed sender = %v, want conflict", err)
	}
	read, err := store.InspectSubagentRequest(t.Context(), owner, recipient.ID, request.ID)
	if err != nil || read.State != subagent.RequestFailed || read.Failure != "sender unavailable" {
		t.Fatalf("request after sender dismissal = %#v, %v", read, err)
	}
	read, err = store.InspectSubagentRequest(t.Context(), owner, recipient.ID, answered.ID)
	if err != nil || read.State != subagent.RequestReplied || read.Reply != "answer" {
		t.Fatalf("already answered request after sender dismissal = %#v, %v", read, err)
	}
	if owners, err := store.PendingSubagentDeliveryOwners(t.Context(), 8); err != nil || len(owners) != 0 {
		t.Fatalf("delivery to dismissed sender = %#v, %v", owners, err)
	}
	if _, err := store.AdmitPendingSubagentDelivery(t.Context(), owner, subagent.DefaultLimits()); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("admitted delivery to dismissed sender = %v", err)
	}
	var pending int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM subagent_request_deliveries
		WHERE task_id IS NULL AND request_id IN (?, ?)`, request.ID, answered.ID).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("orphaned deliveries for dismissed sender = %d, %v", pending, err)
	}
	if results, err := store.PendingMailbox(t.Context(), owner, 8); err != nil || len(results) != 0 {
		t.Fatalf("parent result for dismissed child sender = %#v, %v", results, err)
	}
}

func assertDismissedRequestSettled(t *testing.T, store *Store, request subagent.Request, owner string) {
	t.Helper()
	settled, err := store.SettleSubagentRequests(t.Context(), time.Now(), 8)
	if err != nil || settled != 1 {
		t.Fatalf("dismissed request settlement = %d, %v", settled, err)
	}
	read, err := store.InspectSubagentRequest(t.Context(), owner, request.SenderConversationID, request.ID)
	if err != nil || read.State != subagent.RequestFailed {
		t.Fatalf("dismissed request = %#v, %v", read, err)
	}
	if request.SenderKind == "parent" && request.DeliveryMode == "send" {
		if !read.DeliveryPending || read.Failure != "" {
			t.Fatalf("pending parent failure mailbox = %#v", read)
		}
	} else if read.Failure != "recipient unavailable" {
		t.Fatalf("failure on surviving sender = %#v", read)
	}
	if settled, err := store.SettleSubagentRequests(t.Context(), time.Now(), 8); err != nil || settled != 0 {
		t.Fatalf("duplicate settlement = %d, %v", settled, err)
	}
}

func TestDeletingOwnerRemovesRequestAndBothDeliveryKinds(t *testing.T) {
	store, owner := newSubagentStore(t)
	definition := testAdmission(owner, "work").Definition
	pending, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: "/tmp", Model: "test/model", CallIdentity: "turn_parent/call_pending",
		DeliveryMode: "send", Message: "unanswered",
	})
	if err != nil {
		t.Fatal(err)
	}
	answered, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name,
		CallIdentity: "turn_parent/call_answered", DeliveryMode: "send", Message: "answer me",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), answered.RecipientConversationID, answered.ID, "turn_child/call_reply", "answer"); err != nil {
		t.Fatal(err)
	}
	senderAdmission := testAdmission(owner, "sender parent work")
	senderAdmission.Definition.Name = "sender"
	sender, _, err := store.Admit(t.Context(), senderAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: definition.Name,
		CallIdentity: "turn_sender/call_send", DeliveryMode: "send", Message: "sibling question",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), sibling.RecipientConversationID, sibling.ID, "turn_recipient/call_reply", "sibling answer"); err != nil {
		t.Fatal(err)
	}
	var returnDeliveries int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM subagent_request_deliveries WHERE request_id = ? AND kind = 'reply'`, sibling.ID).Scan(&returnDeliveries); err != nil || returnDeliveries != 1 {
		t.Fatalf("child return delivery before deletion = %d, %v", returnDeliveries, err)
	}
	if err := store.DeleteSession(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	for _, receipt := range []string{pending.ID, answered.ID, sibling.ID} {
		if _, err := store.InspectSubagentRequest(t.Context(), owner, "", receipt); !errors.Is(err, subagent.ErrNotFound) {
			t.Fatalf("deleted owner request %s = %v", receipt, err)
		}
		for _, table := range []string{"subagent_requests", "subagent_request_deliveries", "subagent_parent_deliveries"} {
			column := "request_id"
			if table == "subagent_requests" {
				column = "id"
			}
			var count int
			if err := store.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table+" WHERE "+column+" = ?", receipt).Scan(&count); err != nil || count != 0 {
				t.Fatalf("deleted %s receipt %s rows = %d, %v", table, receipt, count, err)
			}
		}
	}
	if owners, err := store.PendingMailboxOwners(t.Context(), "", 8); err != nil || len(owners) != 0 {
		t.Fatalf("deleted owner reply mailbox = %#v, %v", owners, err)
	}
}

func TestArchivingOwnerSettlesRequestsAndDropsUndeliverableResults(t *testing.T) {
	store, owner := newSubagentStore(t)
	definition := testAdmission(owner, "work").Definition
	open, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: "/tmp", Model: "test/model", CallIdentity: "turn_parent/call_open",
		DeliveryMode: "send", Message: "unanswered",
	})
	if err != nil {
		t.Fatal(err)
	}
	answered, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, RecipientName: definition.Name,
		CallIdentity: "turn_parent/call_answered", DeliveryMode: "send", Message: "answered",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), answered.RecipientConversationID, answered.ID, "turn_child/call_reply", "answer"); err != nil {
		t.Fatal(err)
	}
	senderAdmission := testAdmission(owner, "sender parent work")
	senderAdmission.Definition.Name = "sender"
	sender, _, err := store.Admit(t.Context(), senderAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: owner, SenderConversationID: sender.ID, RecipientName: definition.Name,
		CallIdentity: "turn_sender/call_send", DeliveryMode: "send", Message: "sibling question",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveSession(t.Context(), owner, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, receipt := range []string{open.ID, sibling.ID} {
		if _, err := store.InspectSubagentRequest(t.Context(), owner, "", receipt); !errors.Is(err, subagent.ErrNotFound) {
			t.Fatalf("archived request %s inspection = %v, want not found", receipt, err)
		}
		var state, failure string
		if err := store.db.QueryRowContext(t.Context(), `SELECT state, failure FROM subagent_requests WHERE id = ?`, receipt).Scan(&state, &failure); err != nil || state != "failed" || failure != "owner session was archived" {
			t.Fatalf("archived request %s = state:%s failure:%q, %v", receipt, state, failure, err)
		}
	}
	if _, err := store.InspectSubagentRequest(t.Context(), owner, "", answered.ID); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("archived answered request inspection = %v, want not found", err)
	}
	var answeredState, answeredReply string
	if err := store.db.QueryRowContext(t.Context(), `SELECT state, reply FROM subagent_requests WHERE id = ?`, answered.ID).Scan(&answeredState, &answeredReply); err != nil || answeredState != "replied" || answeredReply != "answer" {
		t.Fatalf("already resolved request retained = state:%s reply:%q, %v", answeredState, answeredReply, err)
	}
	var undelivered int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM subagent_request_deliveries WHERE task_id IS NULL`).Scan(&undelivered); err != nil || undelivered != 0 {
		t.Fatalf("undeliverable child inbox records = %d, %v", undelivered, err)
	}
	if results, err := store.PendingMailbox(t.Context(), owner, 8); err != nil || len(results) != 0 {
		t.Fatalf("undeliverable archived parent results = %#v, %v", results, err)
	}
	if settled, err := store.SettleSubagentRequests(t.Context(), open.DeadlineAt.Add(time.Second), 8); err != nil || settled != 0 {
		t.Fatalf("re-settled archived request = %d, %v", settled, err)
	}
}
