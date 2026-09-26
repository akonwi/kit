package session_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/scratchpad"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

type scratchpadFanoutGateRepository struct {
	*storage.Store
	armed   atomic.Bool
	started chan struct{}
	release chan struct{}
}

func (repository *scratchpadFanoutGateRepository) ListSessions(ctx context.Context, cwd string) ([]session.SessionRecord, error) {
	if repository.armed.CompareAndSwap(true, false) {
		close(repository.started)
		<-repository.release
	}
	return repository.Store.ListSessions(ctx, cwd)
}

type mismatchedScratchpadOwnerRepository struct {
	session.Repository
	corruptID      string
	failForkCreate bool
}

func (r *mismatchedScratchpadOwnerRepository) CreateSession(ctx context.Context, input session.NewSession) (session.SessionRecord, error) {
	record, err := r.Repository.CreateSession(ctx, input)
	if err == nil && r.failForkCreate && input.ParentSessionID != "" && input.ScratchpadOwnerID != input.ID {
		r.corruptID = record.ID
		return session.SessionRecord{}, errors.New("ambiguous fork publication")
	}
	return record, err
}

func (r *mismatchedScratchpadOwnerRepository) GetSession(ctx context.Context, id string) (session.SessionRecord, error) {
	record, err := r.Repository.GetSession(ctx, id)
	if err == nil && id == r.corruptID {
		record.ScratchpadOwnerID = id
	}
	return record, err
}

func TestManagerAssignsScratchpadOwnershipOnlyToSemanticForks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(
		store,
		&authorityProviders{},
		staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)

	owner, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if owner.ScratchpadOwnerID != owner.ID {
		t.Fatalf("root scratchpad owner = %q, want %q", owner.ScratchpadOwnerID, owner.ID)
	}

	peer, err := manager.Create(t.Context(), session.CreateInput{
		CWD: root, Model: "test/echo", ParentSessionID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if peer.ParentSessionID != owner.ID || peer.ScratchpadOwnerID != peer.ID {
		t.Fatalf("independent peer = %+v", peer)
	}

	childResult, err := manager.Fork(t.Context(), owner.ID, session.ForkInput{})
	if err != nil {
		t.Fatal(err)
	}
	child := childResult.Session
	if child.ScratchpadOwnerID != owner.ID {
		t.Fatalf("fork scratchpad owner = %q, want %q", child.ScratchpadOwnerID, owner.ID)
	}
	grandchildResult, err := manager.Fork(t.Context(), child.ID, session.ForkInput{})
	if err != nil {
		t.Fatal(err)
	}
	grandchild := grandchildResult.Session
	if grandchild.ScratchpadOwnerID != owner.ID {
		t.Fatalf("nested fork scratchpad owner = %q, want %q", grandchild.ScratchpadOwnerID, owner.ID)
	}

	family := []session.SessionRecord{owner, child, grandchild}
	before := make(map[string]session.Snapshot, len(family))
	for _, record := range append(family, peer) {
		snapshot, err := manager.Snapshot(t.Context(), record.ID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Scratchpad == nil || snapshot.Scratchpad.OwnerSessionID != record.ScratchpadOwnerID || snapshot.Scratchpad.Revision != 1 {
			t.Fatalf("initial %q scratchpad = %+v", record.ID, snapshot.Scratchpad)
		}
		before[record.ID] = snapshot
	}
	updated, err := manager.UpdateScratchpad(t.Context(), child.ID, 1, "shared family notes")
	if err != nil || updated.OwnerSessionID != owner.ID || updated.Revision != 2 {
		t.Fatalf("UpdateScratchpad() = %+v, %v", updated, err)
	}
	for _, record := range family {
		page, err := manager.Events(t.Context(), record.ID, before[record.ID].EventStreamID, before[record.ID].EventCursor)
		if err != nil || len(page.Events) != 1 || page.Events[0].Kind != session.EventScratchpadChanged || page.Events[0].Scratchpad == nil || *page.Events[0].Scratchpad != updated {
			t.Fatalf("family events for %q = %+v, %v", record.ID, page, err)
		}
		snapshot, err := manager.Snapshot(t.Context(), record.ID)
		if err != nil || snapshot.Scratchpad == nil || *snapshot.Scratchpad != updated {
			t.Fatalf("updated %q snapshot = %+v, %v", record.ID, snapshot.Scratchpad, err)
		}
	}
	peerPage, err := manager.Events(t.Context(), peer.ID, before[peer.ID].EventStreamID, before[peer.ID].EventCursor)
	if err != nil || len(peerPage.Events) != 0 {
		t.Fatalf("independent peer events = %+v, %v", peerPage, err)
	}
	if unchanged, err := manager.UpdateScratchpad(t.Context(), owner.ID, updated.Revision, updated.Content); err != nil || unchanged != updated {
		t.Fatalf("no-op UpdateScratchpad() = %+v, %v", unchanged, err)
	}
	beforeEdit := make(map[string]session.Snapshot, len(family))
	for _, record := range family {
		snapshot, err := manager.Snapshot(t.Context(), record.ID)
		if err != nil {
			t.Fatal(err)
		}
		beforeEdit[record.ID] = snapshot
	}
	edited, applied, err := manager.EditScratchpad(t.Context(), grandchild.ID, []scratchpad.Edit{{OldText: "family", NewText: "root family"}})
	if err != nil || applied != 1 || edited.Revision != 3 || edited.Content != "shared root family notes" {
		t.Fatalf("EditScratchpad() = %+v, %d, %v", edited, applied, err)
	}
	for _, record := range family {
		page, err := manager.Events(t.Context(), record.ID, beforeEdit[record.ID].EventStreamID, beforeEdit[record.ID].EventCursor)
		if err != nil || len(page.Events) != 1 || page.Events[0].Scratchpad == nil || *page.Events[0].Scratchpad != edited {
			t.Fatalf("edit family events for %q = %+v, %v", record.ID, page, err)
		}
	}

	beforeTransfer := make(map[string]session.Snapshot)
	for _, record := range []session.SessionRecord{child, grandchild} {
		snapshot, err := manager.Snapshot(t.Context(), record.ID)
		if err != nil {
			t.Fatal(err)
		}
		beforeTransfer[record.ID] = snapshot
	}
	if err := manager.Delete(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	transferred, err := manager.UpdateScratchpad(t.Context(), child.ID, edited.Revision, "notes after owner deletion")
	if err != nil || transferred.OwnerSessionID != child.ID {
		t.Fatalf("UpdateScratchpad() after owner deletion = %+v, %v", transferred, err)
	}
	for _, record := range []session.SessionRecord{child, grandchild} {
		page, err := manager.Events(t.Context(), record.ID, beforeTransfer[record.ID].EventStreamID, beforeTransfer[record.ID].EventCursor)
		if err != nil || len(page.Events) != 1 || page.Events[0].Kind != session.EventScratchpadChanged || page.Events[0].Scratchpad == nil || *page.Events[0].Scratchpad != transferred {
			t.Fatalf("post-transfer events for %q = %+v, %v", record.ID, page, err)
		}
	}
}

func TestScratchpadFanoutCompletesBeforeOwnerTransfer(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repository := &scratchpadFanoutGateRepository{Store: store, started: make(chan struct{}), release: make(chan struct{})}
	manager, err := session.NewManager(
		repository,
		&authorityProviders{},
		staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	owner, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	fork, err := manager.Fork(t.Context(), owner.ID, session.ForkInput{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := manager.Snapshot(t.Context(), fork.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			close(repository.release)
		}
	}()
	repository.armed.Store(true)
	updateDone := make(chan error, 1)
	go func() {
		_, err := manager.UpdateScratchpad(context.Background(), fork.Session.ID, 1, "serialized update")
		updateDone <- err
	}()
	select {
	case <-repository.started:
	case err := <-updateDone:
		t.Fatalf("scratchpad update returned before fanout gate: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("scratchpad fanout did not reach repository gate")
	}
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- manager.Delete(context.Background(), owner.ID) }()
	select {
	case err := <-deleteDone:
		t.Fatalf("Delete() completed before scratchpad fanout: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if _, err := store.GetSession(t.Context(), owner.ID); err != nil {
		t.Fatalf("owner disappeared before scratchpad fanout completed: %v", err)
	}
	close(repository.release)
	released = true
	if err := <-updateDone; err != nil {
		t.Fatal(err)
	}
	if err := <-deleteDone; err != nil {
		t.Fatal(err)
	}
	page, err := manager.Events(t.Context(), fork.Session.ID, before.EventStreamID, before.EventCursor)
	if err != nil || len(page.Events) != 1 || page.Events[0].Kind != session.EventScratchpadChanged {
		t.Fatalf("event page after serialized deletion = %+v, %v", page, err)
	}
}

func TestForkReconciliationRejectsMismatchedScratchpadOwner(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repository := &mismatchedScratchpadOwnerRepository{Repository: store}
	manager, err := session.NewManager(
		repository,
		&authorityProviders{},
		staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	parent, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := manager.Fork(t.Context(), parent.ID, session.ForkInput{})
	if err != nil {
		t.Fatal(err)
	}
	repository.corruptID = child.Session.ID
	if _, err := manager.Fork(t.Context(), parent.ID, session.ForkInput{ID: child.Session.ID}); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("retry with mismatched owner error = %v", err)
	}
}

func TestAmbiguousForkPublicationRejectsMismatchedScratchpadOwner(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repository := &mismatchedScratchpadOwnerRepository{Repository: store, failForkCreate: true}
	manager, err := session.NewManager(
		repository,
		&authorityProviders{},
		staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	parent, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Fork(t.Context(), parent.ID, session.ForkInput{}); err == nil {
		t.Fatal("ambiguous fork with mismatched owner reconciled successfully")
	}
}
