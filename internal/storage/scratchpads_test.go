package storage

import (
	"errors"
	"math"
	"path/filepath"
	"sync"
	"testing"

	"github.com/akonwi/kit/internal/scratchpad"
	"github.com/akonwi/kit/internal/session"
)

func TestScratchpadsAreSharedByRootSessionFamily(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cwd := t.TempDir()
	root := createScratchpadSession(t, store, session.NewSession{
		ID: "session_root", ScratchpadOwnerID: "session_root", CWD: cwd, Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	child := createScratchpadSession(t, store, session.NewSession{
		ID: "session_child", CWD: cwd, Persistent: true, ParentSessionID: root.ID, ScratchpadOwnerID: root.ID,
		ModelProvider: "test", ModelID: "model",
	})
	grandchild := createScratchpadSession(t, store, session.NewSession{
		ID: "session_grandchild", CWD: cwd, Persistent: true, ParentSessionID: child.ID, ScratchpadOwnerID: root.ID,
		ModelProvider: "test", ModelID: "model",
	})
	if root.ScratchpadOwnerID != root.ID || child.ScratchpadOwnerID != root.ID || grandchild.ScratchpadOwnerID != root.ID {
		t.Fatalf("scratchpad owners = root:%q child:%q grandchild:%q", root.ScratchpadOwnerID, child.ScratchpadOwnerID, grandchild.ScratchpadOwnerID)
	}

	initial, err := store.Get(t.Context(), grandchild.ID)
	if err != nil {
		t.Fatal(err)
	}
	if initial.OwnerSessionID != root.ID || initial.Content != "" || initial.Revision != 1 {
		t.Fatalf("initial scratchpad = %+v", initial)
	}
	updated, err := store.Update(t.Context(), child.ID, initial.Revision, "# Shared notes\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated.OwnerSessionID != root.ID || updated.Revision != 2 {
		t.Fatalf("updated scratchpad = %+v", updated)
	}
	for _, sessionID := range []string{root.ID, child.ID, grandchild.ID} {
		got, err := store.Get(t.Context(), sessionID)
		if err != nil || got != updated {
			t.Fatalf("Get(%q) = %+v, %v; want %+v", sessionID, got, err, updated)
		}
	}

	unchanged, err := store.Update(t.Context(), root.ID, updated.Revision, updated.Content)
	if err != nil || unchanged != updated {
		t.Fatalf("no-op update = %+v, %v; want %+v", unchanged, err, updated)
	}
	if _, err := store.Update(t.Context(), grandchild.ID, 1, "stale"); !errors.Is(err, scratchpad.ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	} else {
		var conflict *scratchpad.ConflictError
		if !errors.As(err, &conflict) || conflict.Current != updated {
			t.Fatalf("conflict = %+v", conflict)
		}
	}

	if err := store.ArchiveSession(t.Context(), root.ID, updated.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(t.Context(), root.ID); !errors.Is(err, scratchpad.ErrNotFound) {
		t.Fatalf("archived root Get error = %v", err)
	}
	if got, err := store.Get(t.Context(), grandchild.ID); err != nil || got != updated {
		t.Fatalf("grandchild after root archive = %+v, %v", got, err)
	}
	if err := store.ArchiveSession(t.Context(), child.ID, updated.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(t.Context(), child.ID); !errors.Is(err, scratchpad.ErrNotFound) {
		t.Fatalf("archived child Get error = %v", err)
	}
	if got, err := store.Get(t.Context(), grandchild.ID); err != nil || got != updated {
		t.Fatalf("grandchild after child archive = %+v, %v", got, err)
	}
}

func TestScratchpadOwnershipDoesNotFollowParentProvenanceImplicitly(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cwd := t.TempDir()
	parent := createScratchpadSession(t, store, session.NewSession{
		ID: "session_provenance_parent", ScratchpadOwnerID: "session_provenance_parent", CWD: cwd, Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	peer := createScratchpadSession(t, store, session.NewSession{
		ID: "session_independent_peer", ScratchpadOwnerID: "session_independent_peer", CWD: cwd, Persistent: true, ParentSessionID: parent.ID,
		ModelProvider: "test", ModelID: "model",
	})
	if peer.ScratchpadOwnerID != peer.ID {
		t.Fatalf("peer scratchpad owner = %q, want self", peer.ScratchpadOwnerID)
	}
	if _, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_invalid_owner", CWD: cwd, Persistent: true, ParentSessionID: peer.ID,
		ScratchpadOwnerID: parent.ID, ModelProvider: "test", ModelID: "model",
	}); err == nil {
		t.Fatal("session inherited a scratchpad outside its parent family")
	}
}

func TestScratchpadExactEditsUseLatestContentAtomically(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := createScratchpadSession(t, store, session.NewSession{
		ID: "session_exact_edit", ScratchpadOwnerID: "session_exact_edit", CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	initialized, applied, changed, err := store.Edit(t.Context(), root.ID, []scratchpad.Edit{{OldText: "", NewText: "alpha beta gamma"}})
	if err != nil || applied != 1 || !changed || initialized.Revision != 2 {
		t.Fatalf("initial Edit() = %+v, %d, %t, %v", initialized, applied, changed, err)
	}
	updated, applied, changed, err := store.Edit(t.Context(), root.ID, []scratchpad.Edit{
		{OldText: "alpha", NewText: "A"}, {OldText: "gamma", NewText: "G"},
	})
	if err != nil || applied != 2 || !changed || updated.Content != "A beta G" || updated.Revision != 3 {
		t.Fatalf("Edit() = %+v, %d, %t, %v", updated, applied, changed, err)
	}
	if _, _, _, err := store.Edit(t.Context(), root.ID, []scratchpad.Edit{
		{OldText: "A", NewText: "changed"}, {OldText: "missing", NewText: "x"},
	}); !errors.Is(err, scratchpad.ErrInvalidEdit) {
		t.Fatalf("invalid Edit() error = %v", err)
	}
	if got, err := store.Get(t.Context(), root.ID); err != nil || got != updated {
		t.Fatalf("scratchpad after rejected edit = %+v, %v", got, err)
	}
	unchanged, applied, changed, err := store.Edit(t.Context(), root.ID, []scratchpad.Edit{{OldText: "beta", NewText: "beta"}})
	if err != nil || applied != 1 || changed || unchanged != updated {
		t.Fatalf("no-op Edit() = %+v, %d, %t, %v", unchanged, applied, changed, err)
	}
}

func TestScratchpadExactEditRacingUserCASDoesNotLoseUpdates(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := createScratchpadSession(t, store, session.NewSession{
		ID: "session_edit_race", ScratchpadOwnerID: "session_edit_race", CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	if _, err := store.Update(t.Context(), root.ID, 1, "alpha"); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	userResult := make(chan error, 1)
	agentResult := make(chan error, 1)
	go func() {
		<-start
		_, updateErr := store.Update(t.Context(), root.ID, 2, "user alpha")
		userResult <- updateErr
	}()
	go func() {
		<-start
		_, _, _, editErr := store.Edit(t.Context(), root.ID, []scratchpad.Edit{{OldText: "alpha", NewText: "agent"}})
		agentResult <- editErr
	}()
	close(start)
	userErr, agentErr := <-userResult, <-agentResult
	if agentErr != nil {
		t.Fatalf("agent edit error = %v", agentErr)
	}
	got, err := store.Get(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if userErr == nil {
		if got.Content != "user agent" || got.Revision != 4 {
			t.Fatalf("user-first race result = %+v", got)
		}
	} else {
		if !errors.Is(userErr, scratchpad.ErrConflict) || got.Content != "agent" || got.Revision != 3 {
			t.Fatalf("agent-first race = %+v user error=%v", got, userErr)
		}
	}
}

func TestScratchpadConcurrentRevisionGuardAllowsOneWriter(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := createScratchpadSession(t, store, session.NewSession{
		ID: "session_concurrent_scratchpad", ScratchpadOwnerID: "session_concurrent_scratchpad", CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	})

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, content := range []string{"first", "second"} {
		content := content
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, updateErr := store.Update(t.Context(), root.ID, 1, content)
			results <- updateErr
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, scratchpad.ErrConflict):
			conflicted++
		default:
			t.Fatalf("unexpected update error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("results = success:%d conflict:%d", succeeded, conflicted)
	}
	got, err := store.Get(t.Context(), root.ID)
	if err != nil || got.Revision != 2 || got.Content != "first" && got.Content != "second" {
		t.Fatalf("persisted scratchpad = %+v, %v", got, err)
	}
}

func TestScratchpadPersistsAndRejectsMigrationOrRevisionTerminalStates(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "kit.db")
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	root := createScratchpadSession(t, store, session.NewSession{
		ID: "session_persisted_scratchpad", ScratchpadOwnerID: "session_persisted_scratchpad", CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	updated, err := store.Update(t.Context(), root.ID, 1, "persisted")
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
	t.Cleanup(func() { _ = store.Close() })
	if got, err := store.Get(t.Context(), root.ID); err != nil || got != updated {
		t.Fatalf("reopened scratchpad = %+v, %v; want %+v", got, err, updated)
	}
	if _, err := store.db.ExecContext(t.Context(), `UPDATE scratchpads SET revision = ? WHERE owner_session_id = ?`, int64(math.MaxInt64), root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(t.Context(), root.ID, math.MaxInt64, "cannot advance"); !errors.Is(err, scratchpad.ErrRevisionExhausted) {
		t.Fatalf("exhausted update error = %v", err)
	}
	if _, err := store.db.ExecContext(t.Context(), `UPDATE scratchpads SET migration_required = 1 WHERE owner_session_id = ?`, root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(t.Context(), root.ID); !errors.Is(err, scratchpad.ErrMigrationRequired) {
		t.Fatalf("migration-required read error = %v", err)
	}
	if _, err := store.Update(t.Context(), root.ID, math.MaxInt64, "blocked"); !errors.Is(err, scratchpad.ErrMigrationRequired) {
		t.Fatalf("migration-required update error = %v", err)
	}
}

func TestSessionCreationRollsBackWhenScratchpadCreationFails(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.db.ExecContext(t.Context(), `
		CREATE TRIGGER reject_test_scratchpad BEFORE INSERT ON scratchpads
		BEGIN SELECT RAISE(ABORT, 'reject test scratchpad'); END
	`); err != nil {
		t.Fatal(err)
	}
	const sessionID = "session_rollback_scratchpad"
	if _, err := store.CreateSession(t.Context(), session.NewSession{
		ID: sessionID, ScratchpadOwnerID: sessionID, CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	}); err == nil {
		t.Fatal("session creation succeeded")
	}
	if _, err := store.GetSession(t.Context(), sessionID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("rolled-back session error = %v", err)
	}
}

func TestSessionCreationRequiresDurableExplicitScratchpadOwnership(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	base := session.NewSession{
		ID: "session_explicit_owner", CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	}
	if _, err := store.CreateSession(t.Context(), base); err == nil {
		t.Fatal("session without explicit scratchpad owner succeeded")
	}
	base.ScratchpadOwnerID = base.ID
	base.Persistent = false
	if _, err := store.CreateSession(t.Context(), base); err == nil {
		t.Fatal("temporary session entered durable repository")
	}
}

func TestScratchpadCannotBeDeletedWhileItsFamilyIsAddressable(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	const sessionID = "session_guarded_scratchpad"
	createScratchpadSession(t, store, session.NewSession{
		ID: sessionID, ScratchpadOwnerID: sessionID, CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	const otherID = "session_other_scratchpad"
	createScratchpadSession(t, store, session.NewSession{
		ID: otherID, ScratchpadOwnerID: otherID, CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	if _, err := store.db.ExecContext(t.Context(), `UPDATE sessions SET scratchpad_owner_id = ? WHERE id = ?`, otherID, sessionID); err == nil {
		t.Fatal("scratchpad owner reassignment succeeded")
	}
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM scratchpads WHERE owner_session_id = ?`, sessionID); err == nil {
		t.Fatal("referenced scratchpad deletion succeeded")
	}
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
		t.Fatalf("delete unreferenced family root: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM scratchpads WHERE owner_session_id = ?`, sessionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("scratchpad rows after family deletion = %d", count)
	}
}

func TestScratchpadRejectsInvalidContentBeforeMutation(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := createScratchpadSession(t, store, session.NewSession{
		ID: "session_invalid_scratchpad", ScratchpadOwnerID: "session_invalid_scratchpad", CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	if _, err := store.Update(t.Context(), root.ID, 1, "bad\r\ncontent"); err == nil {
		t.Fatal("invalid scratchpad update succeeded")
	}
	got, err := store.Get(t.Context(), root.ID)
	if err != nil || got.Revision != 1 || got.Content != "" {
		t.Fatalf("scratchpad after invalid update = %+v, %v", got, err)
	}
}

func createScratchpadSession(t *testing.T, store *Store, input session.NewSession) session.SessionRecord {
	t.Helper()
	record, err := store.CreateSession(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	return record
}
