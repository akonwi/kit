package storage

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/subagent"
)

func TestSubagentAdmissionPreservesConversationAndFIFO(t *testing.T) {
	store, owner := newSubagentStore(t)
	limits := subagent.DefaultLimits()
	firstConversation, first, err := store.Admit(t.Context(), testAdmission(owner, "first"), limits)
	if err != nil {
		t.Fatal(err)
	}
	secondConversation, second, err := store.Admit(t.Context(), testAdmission(owner, "second"), limits)
	if err != nil {
		t.Fatal(err)
	}
	if firstConversation.ID != secondConversation.ID || first.Sequence != 1 || second.Sequence != 2 || first.ID == second.ID {
		t.Fatalf("conversations/tasks = %#v %#v %#v %#v", firstConversation, secondConversation, first, second)
	}
	if secondConversation.QueuedTasks != 2 || secondConversation.State != subagent.ConversationRunning {
		t.Fatalf("conversation = %#v", secondConversation)
	}
	claim, err := store.ClaimNext(t.Context(), owner, limits, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claim.Task.ID != first.ID || claim.Task.State != subagent.TaskRunning || claim.Conversation.ActiveTaskID != first.ID {
		t.Fatalf("claim = %#v", claim)
	}
	if _, err := store.ClaimNext(t.Context(), owner, limits, time.Now()); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("second claim error = %v, want no eligible task", err)
	}
}

func TestSubagentQueueBoundsAreAtomic(t *testing.T) {
	store, owner := newSubagentStore(t)
	limits := subagent.Limits{GlobalRunning: 1, PerSessionRunning: 1, PerConversationQueue: 1, PerSessionQueue: 1, GlobalQueue: 1}
	conversation, first, err := store.Admit(t.Context(), testAdmission(owner, "first"), limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Admit(t.Context(), testAdmission(owner, "rejected"), limits); !errors.Is(err, subagent.ErrQueueFull) {
		t.Fatalf("second admission error = %v", err)
	}
	tasks, err := store.ListTasks(t.Context(), conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != first.ID {
		t.Fatalf("tasks = %#v", tasks)
	}
}

func TestSubagentContinuationCannotResurrectDismissedConversation(t *testing.T) {
	store, owner := newSubagentStore(t)
	conversation, _, err := store.Admit(t.Context(), testAdmission(owner, "first"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Dismiss(t.Context(), conversation.ID, conversation.Generation, "dismiss", time.Now()); err != nil {
		t.Fatal(err)
	}
	continuation := testAdmission(owner, "stale continuation")
	continuation.ConversationID = conversation.ID
	continuation.ExpectedGeneration = conversation.Generation
	if _, _, err := store.Admit(t.Context(), continuation, subagent.DefaultLimits()); !errors.Is(err, subagent.ErrConflict) {
		t.Fatalf("stale continuation error = %v", err)
	}
	if conversations, err := store.ListConversations(t.Context(), owner); err != nil || len(conversations) != 0 {
		t.Fatalf("active conversations = %#v, %v", conversations, err)
	}
}

func TestSubagentConcurrentClaimHasExactlyOneWinner(t *testing.T) {
	store, owner := newSubagentStore(t)
	conversation, task, err := store.Admit(t.Context(), testAdmission(owner, "work"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(8)
	claims := make(chan subagent.Claim, 8)
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			defer wg.Done()
			claim, err := store.ClaimNext(context.Background(), owner, subagent.DefaultLimits(), time.Now())
			if err != nil {
				errs <- err
				return
			}
			claims <- claim
		}()
	}
	wg.Wait()
	close(claims)
	close(errs)
	if len(claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(claims))
	}
	claim := <-claims
	if claim.Task.ID != task.ID || claim.Conversation.ID != conversation.ID {
		t.Fatalf("claim = %#v", claim)
	}
	for err := range errs {
		if !errors.Is(err, subagent.ErrNotFound) {
			t.Fatalf("losing claim error = %v", err)
		}
	}
}

func TestSubagentCompletionAndMailboxAreAtomicAndIdempotent(t *testing.T) {
	store, owner := newSubagentStore(t)
	conversation, _, err := store.Admit(t.Context(), testAdmission(owner, "work"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindChildTurn(t.Context(), claim.Task.ID, claim.Task.CancellationGeneration, "turn_child")
	if err != nil || bound.ChildTurnID != "turn_child" {
		t.Fatalf("BindChildTurn() = %#v, %v", bound, err)
	}
	completion := subagent.Completion{
		TaskID: claim.Task.ID, ConversationID: conversation.ID,
		CancellationGeneration: claim.Task.CancellationGeneration,
		State:                  subagent.TaskCompleted, ChildTurnID: "turn_child", ResultSummary: "done",
	}
	completed, mailbox, err := store.Complete(t.Context(), completion)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != subagent.TaskCompleted || mailbox == nil || mailbox.TaskID != completed.ID || mailbox.Summary != "done" {
		t.Fatalf("completion = %#v mailbox = %#v", completed, mailbox)
	}
	replayed, replayedMailbox, err := store.Complete(t.Context(), completion)
	if err != nil || replayed.ID != completed.ID || replayedMailbox == nil || replayedMailbox.ID != mailbox.ID {
		t.Fatalf("replay = %#v %#v %v", replayed, replayedMailbox, err)
	}
	pending, err := store.PendingMailbox(t.Context(), owner, 10)
	if err != nil || len(pending) != 1 || pending[0].ID != mailbox.ID {
		t.Fatalf("pending = %#v, %v", pending, err)
	}
	if err := store.MarkMailboxDelivered(t.Context(), []string{mailbox.ID}, mailbox.Generation, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkMailboxDelivered(t.Context(), []string{mailbox.ID}, mailbox.Generation, time.Now()); err != nil {
		t.Fatalf("idempotent delivery: %v", err)
	}
	pending, err = store.PendingMailbox(t.Context(), owner, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after delivery = %#v, %v", pending, err)
	}
}

func TestSubagentCancellationAndDismissal(t *testing.T) {
	store, owner := newSubagentStore(t)
	conversation, first, err := store.Admit(t.Context(), testAdmission(owner, "first"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	_, queued, err := store.Admit(t.Context(), testAdmission(owner, "queued"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
	if err != nil || claim.Task.ID != first.ID {
		t.Fatalf("claim = %#v, %v", claim, err)
	}
	canceledQueued, err := store.Cancel(t.Context(), queued.ID, queued.CancellationGeneration, "not needed", time.Now())
	if err != nil || canceledQueued.State != subagent.TaskAborted || canceledQueued.FinishedAt == nil {
		t.Fatalf("queued cancel = %#v, %v", canceledQueued, err)
	}
	pending, err := store.PendingMailbox(t.Context(), owner, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("queued cancellation mailbox = %#v, %v", pending, err)
	}
	cancelRequested, err := store.Cancel(t.Context(), claim.Task.ID, claim.Task.CancellationGeneration, "stop", time.Now())
	if err != nil || cancelRequested.State != subagent.TaskRunning || cancelRequested.CancellationGeneration != claim.Task.CancellationGeneration+1 {
		t.Fatalf("running cancel = %#v, %v", cancelRequested, err)
	}
	completed, mailbox, err := store.Complete(t.Context(), subagent.Completion{
		TaskID: claim.Task.ID, ConversationID: conversation.ID,
		CancellationGeneration: cancelRequested.CancellationGeneration,
		State:                  subagent.TaskAborted, Error: "stop",
	})
	if err != nil || completed.State != subagent.TaskAborted || mailbox == nil || mailbox.State != subagent.TaskAborted {
		t.Fatalf("aborted completion = %#v %#v %v", completed, mailbox, err)
	}

	_, third, err := store.Admit(t.Context(), testAdmission(owner, "third"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	conversation, err = store.Conversation(t.Context(), conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := store.Dismiss(t.Context(), conversation.ID, conversation.Generation, "reset", time.Now())
	if err != nil || len(changed) != 1 || changed[0].ID != third.ID || changed[0].State != subagent.TaskAborted {
		t.Fatalf("dismiss = %#v, %v", changed, err)
	}
	tombstone, err := store.Conversation(t.Context(), conversation.ID)
	if err != nil || tombstone.DismissedAt == nil || tombstone.Generation != conversation.Generation+1 {
		t.Fatalf("tombstone = %#v, %v", tombstone, err)
	}
}

func TestSubagentRecoveryInterruptsRunningAndPreservesQueued(t *testing.T) {
	store, owner := newSubagentStore(t)
	conversation, first, err := store.Admit(t.Context(), testAdmission(owner, "first"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := store.Admit(t.Context(), testAdmission(owner, "second"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
	if err != nil || claim.Task.ID != first.ID {
		t.Fatalf("claim = %#v, %v", claim, err)
	}
	recovery, err := store.RecoverRunning(t.Context(), time.Now())
	if err != nil || len(recovery.InterruptedTasks) != 1 || recovery.InterruptedTasks[0].ID != first.ID || recovery.QueuedTasks != 1 {
		t.Fatalf("recovery = %#v, %v", recovery, err)
	}
	queued, err := store.Task(t.Context(), second.ID)
	if err != nil || queued.State != subagent.TaskQueued {
		t.Fatalf("queued = %#v, %v", queued, err)
	}
	pending, err := store.PendingMailbox(t.Context(), owner, 10)
	if err != nil || len(pending) != 1 || pending[0].State != subagent.TaskInterrupted {
		t.Fatalf("mailbox = %#v, %v", pending, err)
	}
	next, err := store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
	if err != nil || next.Task.ID != second.ID || next.Conversation.ID != conversation.ID {
		t.Fatalf("next claim = %#v, %v", next, err)
	}
}

func TestSessionArchiveTransactionallyTombstonesOwnedSubagents(t *testing.T) {
	store, owner := newSubagentStore(t)
	conversation, first, err := store.Admit(t.Context(), testAdmission(owner, "running"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	_, queued, err := store.Admit(t.Context(), testAdmission(owner, "queued"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimNext(t.Context(), owner, subagent.DefaultLimits(), time.Now())
	if err != nil || claim.Task.ID != first.ID {
		t.Fatalf("claim = %#v, %v", claim, err)
	}
	if err := store.ArchiveSession(t.Context(), owner, time.Now()); err != nil {
		t.Fatal(err)
	}
	tombstone, err := store.Conversation(t.Context(), conversation.ID)
	if err != nil || tombstone.DismissedAt == nil || tombstone.State != subagent.ConversationAborted {
		t.Fatalf("conversation = %#v, %v", tombstone, err)
	}
	for _, id := range []subagent.TaskID{first.ID, queued.ID} {
		task, err := store.Task(t.Context(), id)
		if err != nil || task.State != subagent.TaskAborted {
			t.Fatalf("task %s = %#v, %v", id, task, err)
		}
	}
	pending, err := store.PendingMailbox(t.Context(), owner, 10)
	if err != nil || len(pending) != 1 || pending[0].TaskID != first.ID {
		t.Fatalf("mailbox = %#v, %v", pending, err)
	}
}

func TestSubagentStateCascadesOnPhysicalSessionDeletion(t *testing.T) {
	store, owner := newSubagentStore(t)
	conversation, _, err := store.Admit(t.Context(), testAdmission(owner, "work"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM sessions WHERE id = ?`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Conversation(t.Context(), conversation.ID); !errors.Is(err, subagent.ErrNotFound) {
		t.Fatalf("conversation after cascade error = %v", err)
	}
}

func newSubagentStore(t *testing.T) (*Store, string) {
	t.Helper()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	owner := "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err = store.CreateSession(t.Context(), session.NewSession{
		ID: owner, CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "model", ThinkingLevel: "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, owner
}

func testAdmission(owner, message string) subagent.Admission {
	return subagent.Admission{
		OwnerSessionID: owner,
		Definition: subagent.Definition{
			Name: "scout", Description: "finds things", Instructions: "Inspect carefully.",
			Source: subagent.Source{Kind: subagent.SourceUser, Path: "/tmp/scout.md"},
		},
		CWD: "/tmp", Model: "test/model", ThinkingLevel: "medium", Message: message,
		Now: time.Now(),
	}
}
