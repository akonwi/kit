package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
)

func TestInitialSchemaContainsSessionRegistryAndDurableMailboxState(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rows, err := store.db.QueryContext(t.Context(), `
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	want := []string{"annotation_sequences", "annotations", "direct_bash_history", "parent_mailbox", "peer_session_queries", "schema_migrations", "scratchpads", "session_cwd_mutations", "sessions", "subagent_conversations", "subagent_events", "subagent_tasks"}
	if fmt.Sprint(tables) != fmt.Sprint(want) {
		t.Fatalf("tables = %v, want %v", tables, want)
	}
	for _, obsolete := range []string{"turns", "parent_runs", "messages", "session_streams", "session_events"} {
		var name string
		err := store.db.QueryRowContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, obsolete).Scan(&name)
		if err != sql.ErrNoRows {
			t.Fatalf("table %q exists: name=%q err=%v", obsolete, name, err)
		}
	}
}

func TestSessionRegistryChangesCWD(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	created, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_cwd", ScratchpadOwnerID: "session_cwd", CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	mutation := session.CWDMutation{
		ID: "cwd_1", SessionID: created.ID, TargetPath: "../target",
		PreviousCWD: created.CWD, CWD: target, Changed: true,
	}
	changed, applied, err := store.ApplySessionCWDMutation(t.Context(), mutation)
	if err != nil {
		t.Fatal(err)
	}
	if changed.CWD != target || changed.UpdatedAt.Before(created.UpdatedAt) || applied != mutation {
		t.Fatalf("changed session = %+v, mutation = %+v", changed, applied)
	}
	replayed, receipt, err := store.ApplySessionCWDMutation(t.Context(), mutation)
	if err != nil || replayed.CWD != target || receipt != mutation {
		t.Fatalf("replayed cwd mutation = session:%+v receipt:%+v error:%v", replayed, receipt, err)
	}
	time.Sleep(time.Millisecond)
	noChange := session.CWDMutation{
		ID: "cwd_no_change", SessionID: created.ID, TargetPath: ".",
		PreviousCWD: target, CWD: target, Changed: false,
	}
	unchanged, _, err := store.ApplySessionCWDMutation(t.Context(), noChange)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.UpdatedAt.After(replayed.UpdatedAt) {
		t.Fatalf("accepted no-op cwd activity time = %v, want after %v", unchanged.UpdatedAt, replayed.UpdatedAt)
	}
	time.Sleep(time.Millisecond)
	replayedNoChange, _, err := store.ApplySessionCWDMutation(t.Context(), noChange)
	if err != nil {
		t.Fatal(err)
	}
	if !replayedNoChange.UpdatedAt.Equal(unchanged.UpdatedAt) {
		t.Fatalf("replayed no-op cwd changed activity time from %v to %v", unchanged.UpdatedAt, replayedNoChange.UpdatedAt)
	}
	missing := mutation
	missing.ID, missing.SessionID = "cwd_missing", "session_missing"
	if _, _, err := store.ApplySessionCWDMutation(t.Context(), missing); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("missing cwd change error = %v", err)
	}
}

func TestSessionRegistryRenamesNonArchivedSession(t *testing.T) {
	t.Parallel()

	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	created, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_rename", ScratchpadOwnerID: "session_rename", CWD: t.TempDir(), Name: "Before", Persistent: true,
		ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := store.RenameSession(t.Context(), created.ID, "After")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "After" || renamed.ID != created.ID || renamed.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatalf("renamed session = %+v, created = %+v", renamed, created)
	}
	if _, err := store.RenameSession(t.Context(), "session_missing", "After"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("missing rename error = %v", err)
	}
}

func TestSessionRegistryTouchesActivityMonotonicallyAndSorts(t *testing.T) {
	t.Parallel()

	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cwd := t.TempDir()
	first, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_first", ScratchpadOwnerID: "session_first", CWD: cwd, Persistent: true,
		ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_second", ScratchpadOwnerID: "session_second", CWD: cwd, Persistent: true,
		ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	activityAt := second.UpdatedAt.Add(time.Hour)
	if err := store.TouchSession(t.Context(), first.ID, activityAt); err != nil {
		t.Fatal(err)
	}
	if err := store.TouchSession(t.Context(), first.ID, activityAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	touched, err := store.GetSession(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !touched.UpdatedAt.Equal(activityAt) {
		t.Fatalf("updated at = %v, want %v", touched.UpdatedAt, activityAt)
	}
	renamed, err := store.RenameSession(t.Context(), first.ID, "Still recent")
	if err != nil {
		t.Fatal(err)
	}
	if !renamed.UpdatedAt.Equal(activityAt) {
		t.Fatalf("rename regressed activity time to %v, want %v", renamed.UpdatedAt, activityAt)
	}
	moved, _, err := store.ApplySessionCWDMutation(t.Context(), session.CWDMutation{
		ID: "cwd_monotonic", SessionID: first.ID, TargetPath: "../elsewhere",
		PreviousCWD: cwd, CWD: t.TempDir(), Changed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !moved.UpdatedAt.Equal(activityAt) {
		t.Fatalf("cwd change regressed activity time to %v, want %v", moved.UpdatedAt, activityAt)
	}
	listed, err := store.ListSessions(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].ID != first.ID {
		t.Fatalf("sessions = %+v, want touched session first", listed)
	}
	if err := store.TouchSession(t.Context(), "session_missing", activityAt); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("missing TouchSession() error = %v", err)
	}
}

func TestSessionRegistrySerializesConcurrentActivityAndCWDWrites(t *testing.T) {
	t.Parallel()

	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cwd := t.TempDir()
	record, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_concurrent", ScratchpadOwnerID: "session_concurrent", CWD: cwd, Persistent: true,
		ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	const writes = 16
	start := make(chan struct{})
	errorsFound := make(chan error, writes*2)
	var wait sync.WaitGroup
	for index := 0; index < writes; index++ {
		index := index
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			errorsFound <- store.TouchSession(t.Context(), record.ID, record.UpdatedAt.Add(time.Duration(index+1)*time.Minute))
		}()
		go func() {
			defer wait.Done()
			<-start
			_, _, err := store.ApplySessionCWDMutation(t.Context(), session.CWDMutation{
				ID: fmt.Sprintf("cwd_concurrent_%d", index), SessionID: record.ID,
				TargetPath: fmt.Sprintf("target-%d", index), PreviousCWD: cwd,
				CWD: cwd, Changed: true,
			})
			errorsFound <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("concurrent registry write failed: %v", err)
		}
	}
	loaded, err := store.GetSession(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantActivity := record.UpdatedAt.Add(writes * time.Minute)
	if !loaded.UpdatedAt.Equal(wantActivity) {
		t.Fatalf("activity time = %v, want %v", loaded.UpdatedAt, wantActivity)
	}
}

func TestSessionRegistryArchivesSession(t *testing.T) {
	t.Parallel()

	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	created, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_delete", ScratchpadOwnerID: "session_delete", CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	futureActivity := created.UpdatedAt.Add(time.Hour)
	if err := store.TouchSession(t.Context(), created.ID, futureActivity); err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveSession(t.Context(), created.ID, created.UpdatedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	archived, err := store.GetSession(t.Context(), created.ID)
	if err != nil || archived.ArchivedAt == nil {
		t.Fatalf("archived GetSession() = %+v, %v", archived, err)
	}
	if !archived.UpdatedAt.Equal(futureActivity) {
		t.Fatalf("archive regressed activity time to %v, want %v", archived.UpdatedAt, futureActivity)
	}
	listed, err := store.ListSessions(t.Context(), "")
	if err != nil || len(listed) != 0 {
		t.Fatalf("ListSessions() = %+v, %v", listed, err)
	}
	if err := store.ArchiveSession(t.Context(), created.ID, time.Now().UTC()); err != nil {
		t.Fatalf("repeated ArchiveSession() error = %v", err)
	}
}

func TestSessionRegistryTracksDroidInitialization(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	record, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_test", ScratchpadOwnerID: "session_test", CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.DroidInitializedAt != nil {
		t.Fatalf("new session initialized at %v", record.DroidInitializedAt)
	}
	now := time.Now().UTC()
	if err := store.MarkDroidInitialized(t.Context(), record.ID, now); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetSession(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DroidInitializedAt == nil {
		t.Fatal("droid initialization marker was not persisted")
	}
	if !loaded.UpdatedAt.Equal(record.UpdatedAt) {
		t.Fatalf("droid initialization changed activity time from %v to %v", record.UpdatedAt, loaded.UpdatedAt)
	}
}
