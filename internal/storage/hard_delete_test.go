package storage

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/subagent"
)

func TestDeleteSessionCascadesAndTransfersSharedScratchpad(t *testing.T) {
	t.Parallel()

	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	root, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_root", ScratchpadOwnerID: "session_root", CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_child", ParentSessionID: root.ID, ScratchpadOwnerID: root.ID,
		CWD: root.CWD, Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_grandchild", ParentSessionID: child.ID, ScratchpadOwnerID: root.ID,
		CWD: root.CWD, Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `
		UPDATE scratchpads SET content = 'shared notes', revision = 9 WHERE owner_session_id = ?
	`, root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `
		INSERT INTO session_cwd_mutations(session_id, mutation_id, target_path, previous_cwd, cwd, changed, created_at)
		VALUES (?, 'mutation_delete', ?, ?, ?, 1, ?)
	`, root.ID, root.CWD, root.CWD, root.CWD, formatTimestamp(root.CreatedAt)); err != nil {
		t.Fatal(err)
	}

	if err := store.ArchiveSession(t.Context(), root.ID, root.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSession(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSession(t.Context(), root.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted session GetSession() error = %v", err)
	}
	if _, err := store.CreateSession(t.Context(), session.NewSession{
		ID: root.ID, ScratchpadOwnerID: root.ID, CWD: root.CWD, Persistent: true,
		ModelProvider: "test", ModelID: "model",
	}); err == nil {
		t.Fatal("recreating a permanently deleted session ID succeeded")
	}
	var cleanupPending int
	if err := store.db.QueryRowContext(t.Context(), `SELECT cleanup_pending FROM deleted_session_ids WHERE id = ?`, root.ID).Scan(&cleanupPending); err != nil || cleanupPending != 1 {
		t.Fatalf("cleanup_pending = %d, %v; want 1", cleanupPending, err)
	}
	if err := store.DeleteSession(t.Context(), root.ID); err != nil {
		t.Fatalf("repeated DeleteSession() error = %v", err)
	}
	if err := store.CompleteSessionDeletion(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(t.Context(), `SELECT cleanup_pending FROM deleted_session_ids WHERE id = ?`, root.ID).Scan(&cleanupPending); err != nil || cleanupPending != 0 {
		t.Fatalf("cleanup_pending after cleanup = %d, %v; want 0", cleanupPending, err)
	}
	for _, id := range []string{child.ID, grandchild.ID} {
		record, err := store.GetSession(t.Context(), id)
		if err != nil {
			t.Fatalf("surviving session %q: %v", id, err)
		}
		if record.ScratchpadOwnerID != child.ID {
			t.Errorf("session %q scratchpad owner = %q, want %q", id, record.ScratchpadOwnerID, child.ID)
		}
	}
	var content string
	var revision int64
	if err := store.db.QueryRowContext(t.Context(), `
		SELECT content, revision FROM scratchpads WHERE owner_session_id = ?
	`, child.ID).Scan(&content, &revision); err != nil {
		t.Fatal(err)
	}
	if content != "shared notes" || revision != 9 {
		t.Fatalf("transferred scratchpad = (%q, %d), want (%q, 9)", content, revision, "shared notes")
	}
	var mutations int
	if err := store.db.QueryRowContext(t.Context(), `
		SELECT COUNT(*) FROM session_cwd_mutations WHERE session_id = ?
	`, root.ID).Scan(&mutations); err != nil {
		t.Fatal(err)
	}
	if mutations != 0 {
		t.Fatalf("deleted session retained %d cwd mutations", mutations)
	}
	var violations int
	rows, err := store.db.QueryContext(t.Context(), `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		violations++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if violations != 0 {
		t.Fatalf("foreign key check found %d violations", violations)
	}
}

func TestDeleteSessionPersistsExternalArtifactInventory(t *testing.T) {
	store, owner := newSubagentStore(t)
	conversation, _, err := store.Admit(t.Context(), testAdmission(owner, "inventory"), subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSession(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	ids, err := store.ListSessionDeletionArtifacts(t.Context(), owner)
	if err != nil || len(ids) != 1 || ids[0] != string(conversation.ID) {
		t.Fatalf("deletion artifact inventory = %v, %v", ids, err)
	}
	if err := store.CompleteSessionDeletion(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	ids, err = store.ListSessionDeletionArtifacts(t.Context(), owner)
	if err != nil || len(ids) != 0 {
		t.Fatalf("completed deletion artifact inventory = %v, %v", ids, err)
	}
}

func TestDeleteSessionInventoriesActiveAndDismissedChildStores(t *testing.T) {
	store, owner := newSubagentStore(t)
	activeAdmission := testAdmission(owner, "active")
	activeAdmission.Definition.Name = "active-scout"
	active, _, err := store.Admit(t.Context(), activeAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	dismissedAdmission := testAdmission(owner, "dismissed")
	dismissedAdmission.Definition.Name = "dismissed-scout"
	dismissed, _, err := store.Admit(t.Context(), dismissedAdmission, subagent.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Dismiss(t.Context(), dismissed.ID, dismissed.Generation, "test", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSession(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	ids, err := store.ListSessionDeletionArtifacts(t.Context(), owner)
	if err != nil || len(ids) != 2 || ids[0] != string(active.ID) && ids[1] != string(active.ID) || ids[0] != string(dismissed.ID) && ids[1] != string(dismissed.ID) {
		t.Fatalf("active and dismissed deletion artifacts = %v, %v", ids, err)
	}
}

func TestDeleteSuccessorPreservesScratchpadForSiblingForks(t *testing.T) {
	t.Parallel()

	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_family_root", ScratchpadOwnerID: "session_family_root", CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	childA, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_family_a", ParentSessionID: root.ID, ScratchpadOwnerID: root.ID,
		CWD: root.CWD, Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	childB, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_family_b", ParentSessionID: root.ID, ScratchpadOwnerID: root.ID,
		CWD: root.CWD, Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `
		UPDATE scratchpads SET content = 'family notes' WHERE owner_session_id = ?
	`, root.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSession(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSession(t.Context(), childA.ID); err != nil {
		t.Fatalf("delete first successor: %v", err)
	}
	record, err := store.GetSession(t.Context(), childB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ScratchpadOwnerID != childB.ID {
		t.Fatalf("remaining sibling owner = %q, want %q", record.ScratchpadOwnerID, childB.ID)
	}
	var content string
	if err := store.db.QueryRowContext(t.Context(), `
		SELECT content FROM scratchpads WHERE owner_session_id = ?
	`, childB.ID).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "family notes" {
		t.Fatalf("remaining scratchpad = %q, want %q", content, "family notes")
	}
}
