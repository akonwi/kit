package session_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
)

func TestPendingParentRequestReplyStartsReactionAfterManagerReconstruction(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	droidDirectory := filepath.Join(root, "droids")
	first, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Shutdown(context.Background()) })
	record, err := first.Create(t.Context(), session.CreateInput{
		ID: "session_abcdefabcdefabcdefabcdefabcdefab", CWD: root, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	definition := subagent.Definition{
		Name: "researcher", Description: "answers requests", Instructions: "Reply explicitly.",
		Source: subagent.Source{Kind: subagent.SourceUser, Path: "/tmp/researcher.md"},
	}
	request, err := store.CreateSubagentRequest(t.Context(), subagent.RequestAdmission{
		OwnerSessionID: record.ID, RecipientName: definition.Name, NewRecipient: &definition,
		CWD: root, Model: "test/echo", DeliveryMode: "send", CallIdentity: "turn_parent/call_send",
		Message: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplySubagentRequest(t.Context(), request.RecipientConversationID, request.ID, "turn_child/call_reply", "answer from researcher"); err != nil {
		t.Fatal(err)
	}
	second, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Shutdown(context.Background()) })
	if err := second.StartSubagentMailbox(); err != nil {
		t.Fatal(err)
	}
	boundaryID := "mail_" + strings.TrimPrefix(request.ID, "subrequest_")
	deadline := time.Now().Add(3 * time.Second)
	for {
		pending, pendingErr := store.PendingMailbox(t.Context(), record.ID, 8)
		snapshot, snapshotErr := second.Snapshot(t.Context(), record.ID)
		providers.mu.Lock()
		calls := providers.calls
		providers.mu.Unlock()
		if pendingErr == nil && snapshotErr == nil && len(pending) == 0 && snapshot.ActiveRunID == "" && calls == 1 {
			var found bool
			for _, message := range snapshot.Messages {
				found = found || message.Role == "context" && message.BoundaryID == boundaryID &&
					message.BoundaryKind == "subagent_request_result" &&
					len(message.Content) == 2 && message.Content[1].Text ==
					"Subagent researcher replied to request "+request.ID+" with state completed.\nResult: answer from researcher"
			}
			if found {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("parent request result not delivered: pending=%#v snapshot=%#v calls=%d errors=%v/%v", pending, snapshot, calls, pendingErr, snapshotErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
	oldMailbox, err := store.PendingMailbox(t.Context(), record.ID, 8)
	if err != nil || len(oldMailbox) != 0 {
		t.Fatalf("task completion mailbox = %#v, %v", oldMailbox, err)
	}
}
