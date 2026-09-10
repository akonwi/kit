package session_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
)

func TestManagerCreateIsIdempotentForClientSelectedSessionID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(
		store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	input := session.CreateInput{
		ID: "session_0123456789abcdef0123456789abcdef", CWD: root, Name: "Retry safe", Model: "test/echo",
	}
	first, err := manager.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := manager.List(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != input.ID || second.ID != first.ID || len(listed) != 1 {
		t.Fatalf("idempotent creates first=%q second=%q listed=%+v", first.ID, second.ID, listed)
	}
	conflict := input
	conflict.CWD = t.TempDir()
	if _, err := manager.Create(t.Context(), conflict); err == nil {
		t.Fatal("Create accepted a reused session id with different immutable metadata")
	}
	renamed, err := manager.Rename(t.Context(), first.ID, " Renamed session ")
	if err != nil || renamed.Name != "Renamed session" {
		t.Fatalf("Rename() = %+v, %v", renamed, err)
	}
	replayed, err := manager.Create(t.Context(), input)
	if err != nil || replayed.ID != first.ID || replayed.Name != "Renamed session" {
		t.Fatalf("Create() replay after rename = %+v, %v", replayed, err)
	}
	for _, invalid := range []string{"   ", "bad\nname", "bad\u202ename"} {
		if _, err := manager.Rename(t.Context(), first.ID, invalid); !errors.Is(err, session.ErrInvalidInput) {
			t.Fatalf("Rename(%q) error = %v", invalid, err)
		}
	}
}

func TestManagerCoordinatesTemporaryCreationAndDisposal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repository := &blockingGetRepository{
		Repository: store, started: make(chan struct{}), release: make(chan struct{}),
	}
	manager, err := session.NewManager(repository, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	input := session.CreateInput{
		ID: "session_0123456789abcdef0123456789abcdef", CWD: root, Model: "test/echo", Temporary: true,
	}
	created := make(chan error, 1)
	go func() {
		_, err := manager.Create(context.Background(), input)
		created <- err
	}()
	select {
	case <-repository.started:
	case <-time.After(5 * time.Second):
		t.Fatal("temporary create did not reach repository check")
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(repository.release)
	}()
	if err := manager.DisposeTemporary(context.Background(), input.ID); err != nil {
		t.Fatalf("DisposeTemporary() error = %v", err)
	}
	if err := <-created; !errors.Is(err, session.ErrDeleteBusy) {
		t.Fatalf("racing Create() error = %v, want disposal rejection", err)
	}
	if _, err := manager.Create(t.Context(), input); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("delayed Create() error = %v, want disposed-id rejection", err)
	}
	if _, err := manager.Snapshot(t.Context(), input.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("racing temporary Snapshot() error = %v", err)
	}
}

func TestManagerTemporarySessionUsesMemoryAndDisappearsOnDelete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	droidDirectory := filepath.Join(root, "droids")
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_0123456789abcdef0123456789abcdef", CWD: root, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Persistent {
		t.Fatalf("temporary session = %+v, want non-persistent", created)
	}
	renamed, err := manager.Rename(t.Context(), created.ID, " Temporary work ")
	if err != nil || renamed.Name != "Temporary work" || renamed.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatalf("Rename(temporary) = %+v, %v", renamed, err)
	}
	loaded, err := manager.Get(t.Context(), created.ID)
	if err != nil || loaded.ID != created.ID || loaded.CWD != root {
		t.Fatalf("Get(temporary) = %+v, %v", loaded, err)
	}
	listed, err := manager.List(t.Context(), "")
	if err != nil || len(listed) != 0 {
		t.Fatalf("List() = %+v, %v, want no temporary sessions", listed, err)
	}
	if _, err := store.GetSession(t.Context(), created.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("persisted temporary record error = %v, want not found", err)
	}
	result, err := manager.RunPrompt(t.Context(), created.ID, "temporary prompt")
	if err != nil || result.Status != session.RunStatusCompleted {
		t.Fatalf("RunPrompt() = %+v, %v", result, err)
	}
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Session.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("temporary session activity time = %v, want after %v", snapshot.Session.UpdatedAt, created.UpdatedAt)
	}
	entries, err := os.ReadDir(droidDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary droid files = %v, want none", entries)
	}
	if err := manager.Delete(t.Context(), created.ID); !errors.Is(err, session.ErrTemporary) {
		t.Fatalf("Delete(temporary) error = %v", err)
	}
	if err := manager.DisposeTemporary(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), created.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("disposed Snapshot() error = %v, want not found", err)
	}
	if err := manager.DisposeTemporary(t.Context(), created.ID); err != nil {
		t.Fatalf("second DisposeTemporary() error = %v", err)
	}
}

func TestAcceptedPromptAdvancesPersistentSessionActivity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(
		store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	older, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_11111111111111111111111111111111", CWD: root, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_22222222222222222222222222222222", CWD: root, Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), older.ID, "make this session recent"); err != nil {
		t.Fatal(err)
	}
	listed, err := manager.List(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].ID != older.ID || !listed[0].UpdatedAt.After(newer.UpdatedAt) {
		t.Fatalf("sessions after prompt = %+v, want prompted session first", listed)
	}
	activityAt := listed[0].UpdatedAt
	if _, err := manager.StartPrompt(t.Context(), older.ID, "   "); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("invalid StartPrompt() error = %v", err)
	}
	afterInvalid, err := store.GetSession(t.Context(), older.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !afterInvalid.UpdatedAt.Equal(activityAt) {
		t.Fatalf("invalid prompt changed activity time from %v to %v", activityAt, afterInvalid.UpdatedAt)
	}
	if err := manager.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	reopened, err := session.NewManager(
		store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	afterRestart, err := reopened.List(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterRestart) != 2 || afterRestart[0].ID != older.ID || !afterRestart[0].UpdatedAt.Equal(activityAt) {
		t.Fatalf("sessions after restart = %+v, want prompted session first", afterRestart)
	}
}

var errSessionActivityWrite = errors.New("simulated session activity persistence failure")

type failingTouchRepository struct {
	session.Repository
}

func (*failingTouchRepository) TouchSession(context.Context, string, time.Time) error {
	return errSessionActivityWrite
}

func TestPromptActivityFailurePreventsDroidAdmission(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(
		&failingTouchRepository{Repository: store}, providers, staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartPrompt(t.Context(), created.ID, "do not admit this"); !errors.Is(err, errSessionActivityWrite) {
		t.Fatalf("StartPrompt() error = %v", err)
	}
	providers.mu.Lock()
	calls := providers.calls
	providers.mu.Unlock()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveRunID != "" || len(snapshot.Messages) != 0 {
		t.Fatalf("snapshot after failed activity write = %+v", snapshot)
	}
}

func TestManagerDeletesIdleSessionAndRetainsArchivedDroidStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	droidDirectory := filepath.Join(root, "droids")
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	droidPath := filepath.Join(droidDirectory, created.ID+".db")
	if err := manager.Delete(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(droidPath); err != nil {
		t.Fatalf("archived droid store was not retained: %v", err)
	}
	if _, err := manager.Snapshot(t.Context(), created.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("deleted Snapshot() error = %v", err)
	}
	listed, err := manager.List(t.Context(), "")
	if err != nil || len(listed) != 0 {
		t.Fatalf("List() = %+v, %v", listed, err)
	}
	if _, err := manager.Create(t.Context(), session.CreateInput{ID: created.ID, CWD: root, Model: "test/echo"}); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("Create() with deleted id error = %v", err)
	}
}

func TestManagerProjectsCanonicalDroidHistoryAcrossRestart(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.RunPrompt(t.Context(), created.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.RunID != result.TurnID || result.Status != session.RunStatusCompleted {
		t.Fatalf("result = %+v", result)
	}
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Messages) != 2 || snapshot.Messages[0].ID == "" || snapshot.Messages[0].TurnID != result.TurnID {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if err := manager.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	reopened, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	afterRestart, err := reopened.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterRestart.Messages) != 2 || afterRestart.Messages[0].ID != snapshot.Messages[0].ID {
		t.Fatalf("reopened snapshot = %+v", afterRestart)
	}
}

func TestInitializedSessionFailsClosedWhenDroidStoreIsMissing(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	droidDirectory := filepath.Join(root, "droids")
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(droidDirectory, created.ID+".db")); err != nil {
		t.Fatal(err)
	}
	reopened, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if _, err := reopened.Snapshot(t.Context(), created.ID); !errors.Is(err, session.ErrDroidStoreMissing) {
		t.Fatalf("Snapshot error = %v, want ErrDroidStoreMissing", err)
	}
}

func TestCompletedBashBecomesPendingThenConsumedDroidBoundary(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	bash, err := manager.StartBash(t.Context(), created.ID, "bash_0123456789abcdef0123456789abcdef", "printf boundary", false)
	if err != nil {
		t.Fatal(err)
	}
	activeRecord, err := store.GetSession(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !activeRecord.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("bash activity time = %v, want after %v", activeRecord.UpdatedAt, created.UpdatedAt)
	}
	deadline := time.Now().Add(5 * time.Second)
	for bash.Status == session.BashExecutionRunning {
		bash, err = manager.GetBash(t.Context(), created.ID, bash.ID)
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("bash did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	pending, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending.Boundaries) != 1 || pending.Boundaries[0].ID != bash.ID || len(pending.Boundaries[0].Details) == 0 {
		t.Fatalf("pending boundaries = %+v", pending.Boundaries)
	}
	if _, err := manager.RunPrompt(t.Context(), created.ID, "consume boundary"); err != nil {
		t.Fatal(err)
	}
	consumed, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(consumed.Boundaries) != 0 {
		t.Fatalf("pending after prompt = %+v", consumed.Boundaries)
	}
	found := false
	for _, message := range consumed.Messages {
		if message.Role == "context" && message.BoundaryID == bash.ID && message.BoundaryKind == "bash" {
			found = len(message.Details) > 0
		}
	}
	if !found {
		t.Fatalf("consumed boundary not projected: %+v", consumed.Messages)
	}
}

func TestExcludedBashRemainsTransient(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	bash, err := manager.StartBash(t.Context(), created.ID, "bash_abcdef0123456789abcdef0123456789", "printf private", true)
	if err != nil {
		t.Fatal(err)
	}
	for bash.Status == session.BashExecutionRunning {
		bash, err = manager.GetBash(t.Context(), created.ID, bash.ID)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Boundaries) != 0 {
		t.Fatalf("excluded bash reached droid: %+v", snapshot.Boundaries)
	}
	if err := manager.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	reopened, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if _, err := reopened.GetBash(t.Context(), created.ID, bash.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("reopened excluded bash = %v, want transient not found", err)
	}
}

func TestWaitCancellationDetachesWithoutAbortingDroid(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	waitContext, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := manager.RunPrompt(waitContext, created.ID, "keep running")
		result <- err
	}()
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("RunPrompt error = %v, want detached cancellation", err)
	}
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveRunID == "" {
		t.Fatal("wait cancellation aborted the droid")
	}
	if err := manager.Abort(t.Context(), created.ID, snapshot.ActiveRunID); err != nil {
		t.Fatal(err)
	}
	close(providers.block)
}

func TestManagerDisposeTemporaryCancelsActiveRun(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_0123456789abcdef0123456789abcdef", CWD: root, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartPrompt(t.Context(), created.ID, "keep running"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	if err := manager.DisposeTemporary(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	close(providers.block)
	if _, err := manager.Snapshot(t.Context(), created.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("disposed active temporary Snapshot() error = %v", err)
	}
}

func TestManagerDisposeTemporaryCancelsActiveBash(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_0123456789abcdef0123456789abcdef", CWD: root, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartBash(t.Context(), created.ID, "bash_0123456789abcdef0123456789abcdef", "sleep 30", false); err != nil {
		t.Fatal(err)
	}
	disposeContext, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := manager.DisposeTemporary(disposeContext, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), created.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("disposed bash temporary Snapshot() error = %v", err)
	}
}

func TestManagerFollowUpsQueueRestorePromoteAndAutoStart(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartPrompt(t.Context(), created.ID, "first"); err != nil {
		t.Fatal(err)
	}
	<-providers.started
	for _, text := range []string{"second", "third"} {
		result, err := manager.SubmitPrompt(t.Context(), created.ID, text)
		if err != nil || !result.Queued {
			t.Fatalf("SubmitPrompt(%q) = %+v, %v", text, result, err)
		}
	}
	restored, err := manager.RestoreFollowUps(t.Context(), created.ID)
	if err != nil || !reflect.DeepEqual(restored.Messages, []string{"second", "third"}) || restored.Queue.Count != 0 {
		t.Fatalf("RestoreFollowUps() = %+v, %v", restored, err)
	}
	for _, text := range restored.Messages {
		if _, err := manager.SubmitPrompt(t.Context(), created.ID, text); err != nil {
			t.Fatal(err)
		}
	}
	promoted, err := manager.PromoteFollowUps(t.Context(), created.ID)
	if err != nil || promoted.Promoted != 2 || promoted.Queue.Count != 0 {
		t.Fatalf("PromoteFollowUps() = %+v, %v", promoted, err)
	}
	if _, err := manager.SubmitPrompt(t.Context(), created.ID, "automatic"); err != nil {
		t.Fatal(err)
	}
	close(providers.block)
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, snapshotErr := manager.Snapshot(t.Context(), created.ID)
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		if snapshot.FollowUps.Count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued follow-up did not start automatically: %+v", snapshot.FollowUps)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestManagerStartsQueuedFollowUpsAsDistinctTurns(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartPrompt(t.Context(), created.ID, "first"); err != nil {
		t.Fatal(err)
	}
	<-providers.started
	for _, text := range []string{"second", "third"} {
		result, err := manager.SubmitPrompt(t.Context(), created.ID, text)
		if err != nil || !result.Queued {
			t.Fatalf("SubmitPrompt(%q) = %+v, %v", text, result, err)
		}
	}
	close(providers.block)
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, snapshotErr := manager.Snapshot(t.Context(), created.ID)
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		if snapshot.ActiveRunID == "" && snapshot.FollowUps.Count == 0 {
			var users []string
			for _, message := range snapshot.Messages {
				if message.Role != "user" {
					continue
				}
				for _, content := range message.Content {
					if content.Kind == session.TranscriptContentText {
						users = append(users, content.Text)
					}
				}
			}
			if !reflect.DeepEqual(users, []string{"first", "second", "third"}) {
				t.Fatalf("user turns = %#v", users)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued turns did not settle: active=%q queue=%+v", snapshot.ActiveRunID, snapshot.FollowUps)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestManagerRejectsDeleteWhileSessionRunIsActive(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := manager.StartPrompt(t.Context(), created.ID, "keep running")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	if err := manager.Delete(t.Context(), created.ID); !errors.Is(err, session.ErrDeleteBusy) {
		t.Fatalf("Delete() active error = %v", err)
	}
	if err := manager.Abort(t.Context(), created.ID, reservation.RunID); err != nil {
		t.Fatal(err)
	}
	close(providers.block)
}

func TestDelayedAbortCannotCancelSuccessorTurn(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.RunPrompt(t.Context(), created.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	providers.block = make(chan struct{})
	block := providers.block
	providers.mu.Unlock()
	second, err := manager.StartPrompt(t.Context(), created.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Abort(t.Context(), created.ID, first.TurnID); !errors.Is(err, session.ErrRunNotAbortable) {
		t.Fatalf("delayed Abort = %v", err)
	}
	run, err := manager.GetRun(t.Context(), created.ID, second.TurnID)
	if err != nil || run.Status != session.RunStatusRunning {
		t.Fatalf("successor = %+v, %v", run, err)
	}
	if err := manager.Abort(t.Context(), created.ID, second.TurnID); err != nil {
		t.Fatal(err)
	}
	close(block)
}

func TestAbortChecksDroidTurnGeneration(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{})}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := manager.StartPrompt(t.Context(), created.ID, "block")
	if err != nil {
		t.Fatal(err)
	}
	activeSnapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !activeSnapshot.EventReplayAvailable || activeSnapshot.EventStreamID == "" || activeSnapshot.ActiveRunID != reservation.TurnID {
		t.Fatalf("active synchronization metadata = %+v", activeSnapshot)
	}
	if err := manager.Abort(t.Context(), created.ID, "turn_wrong"); !errors.Is(err, session.ErrRunNotAbortable) {
		t.Fatalf("wrong-generation Abort = %v", err)
	}
	if err := manager.Abort(t.Context(), created.ID, reservation.TurnID); err != nil {
		t.Fatal(err)
	}
	close(providers.block)
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := manager.GetRun(t.Context(), created.ID, reservation.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == session.RunStatusAborted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not abort: %+v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type blockingGetRepository struct {
	session.Repository
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingGetRepository) GetSession(ctx context.Context, sessionID string) (session.SessionRecord, error) {
	r.once.Do(func() { close(r.started) })
	select {
	case <-r.release:
	case <-ctx.Done():
		return session.SessionRecord{}, ctx.Err()
	}
	return r.Repository.GetSession(ctx, sessionID)
}

func TestManagerIncludesCurrentSkillCatalogAndActivationTool(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(
		store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_abcdef0123456789abcdef0123456789", CWD: root, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "hello"); err != nil {
		t.Fatal(err)
	}

	providers.mu.Lock()
	if len(providers.requests) != 1 {
		providers.mu.Unlock()
		t.Fatalf("provider requests = %d, want 1", len(providers.requests))
	}
	request := providers.requests[0]
	providers.mu.Unlock()
	for _, expected := range []string{
		"system\n\nThe following skills provide specialized instructions",
		"<name>kit-customization</name>",
		"<source>built-in</source>",
	} {
		if !strings.Contains(request.SystemPrompt, expected) {
			t.Fatalf("system prompt does not contain %q:\n%s", expected, request.SystemPrompt)
		}
	}
	if strings.Contains(request.SystemPrompt, "<location>") {
		t.Fatalf("embedded skill was given a fake location:\n%s", request.SystemPrompt)
	}
	toolNames := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	wantTools := []string{"bash", "read", "write", "edit", "ls", "grep", "find", skills.ActivateToolName, session.ChangeCWDToolName}
	if !reflect.DeepEqual(toolNames, wantTools) {
		t.Fatalf("provider tools = %#v, want %#v", toolNames, wantTools)
	}
}

func staticRuntimeBundleBuilder(core string) session.RuntimeBundleBuilder {
	registry, err := skills.NewRegistry()
	if err != nil {
		panic(err)
	}
	builder, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{Core: core, Registry: registry})
	if err != nil {
		panic(err)
	}
	return builder
}

type authorityProviders struct {
	mu       sync.Mutex
	calls    int
	block    chan struct{}
	started  chan struct{}
	requests []droids.Request
}

func (p *authorityProviders) ID() string             { return "test" }
func (p *authorityProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *authorityProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *authorityProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *authorityProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "echo" || id == "test/echo"
}
func (p *authorityProviders) RefreshModels(context.Context) error { return nil }
func (p *authorityProviders) Stream(ctx context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.calls++
	p.requests = append(p.requests, request)
	call := p.calls
	block := p.block
	p.mu.Unlock()
	if block != nil {
		if p.started != nil {
			select {
			case <-p.started:
			default:
				close(p.started)
			}
		}
		select {
		case <-block:
		case <-ctx.Done():
		}
	}
	return &authorityStream{message: droids.AssistantMessage{
		Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: fmt.Sprintf("reply %d", call)}},
	}}
}
func (*authorityProviders) model() droids.Model {
	return droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
}

type authorityStream struct{ message droids.AssistantMessage }

func (s *authorityStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 2)
	events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: "test", Model: "echo"}}
	events <- droids.StreamDone{Message: s.message}
	close(events)
	return events
}
func (s *authorityStream) Result() droids.AssistantMessage { return s.message }
