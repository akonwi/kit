package session_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func TestDeleteRejectsWhileSessionOperationIsHeld(t *testing.T) {
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
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	release, err := manager.BeginSessionOperation(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond, err := manager.BeginSessionOperation(t.Context(), record.ID)
	if err != nil {
		t.Fatalf("second concurrent session operation = %v", err)
	}
	if err := manager.Delete(t.Context(), record.ID); !errors.Is(err, session.ErrDeleteBusy) {
		t.Fatalf("Delete() with an active session operation = %v, want ErrDeleteBusy", err)
	}
	release()
	releaseSecond()
	if err := manager.Delete(t.Context(), record.ID); err != nil {
		t.Fatalf("Delete() after releasing operation = %v", err)
	}
}

func TestRuntimeDeleteDoesNotSweepUnrelatedOrphanStores(t *testing.T) {
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
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	inProgressFork := filepath.Join(droidDirectory, "session_00000000000000000000000000000000.db")
	if err := os.WriteFile(inProgressFork, []byte("in-progress fork"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Delete(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(inProgressFork); err != nil {
		t.Fatalf("unrelated in-progress store was removed: %v", err)
	}
}

func TestRepeatedDeleteResumesPendingExternalCleanup(t *testing.T) {
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
	if err := store.DeleteSession(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(droidPath); err != nil {
		t.Fatalf("precondition: droid store missing before retry: %v", err)
	}
	if err := manager.Delete(t.Context(), created.ID); err != nil {
		t.Fatalf("repeated Delete() did not resume cleanup: %v", err)
	}
	if _, err := os.Stat(droidPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retried droid store still exists (stat error %v)", err)
	}
}
