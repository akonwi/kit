package annotation

import (
	"testing"
	"time"
)

func TestSessionLockLeaseRetainsIdentityWhileWaitersExist(t *testing.T) {
	service, err := NewService(&memoryRepository{records: make(map[uint64]Record)}, &staticReader{content: "one"})
	if err != nil {
		t.Fatal(err)
	}
	first := service.sessionLock("session_lock")
	first.Lock()
	entry := first.entry
	second := service.sessionLock("session_lock")
	if second.entry != entry || entry.refs != 2 {
		t.Fatalf("second lease entry/refcount = %p/%d, want %p/2", second.entry, entry.refs, entry)
	}
	secondAcquired := make(chan struct{})
	secondRelease := make(chan struct{})
	go func() {
		second.Lock()
		close(secondAcquired)
		<-secondRelease
		second.Unlock()
	}()
	first.Unlock()
	select {
	case <-secondAcquired:
	case <-time.After(3 * time.Second):
		t.Fatal("second lock lease did not acquire")
	}
	third := service.sessionLock("session_lock")
	if third.entry != entry {
		t.Fatal("new caller received a different lock while a waiter held the original")
	}
	close(secondRelease)
	third.Lock()
	third.Unlock()
	service.locksMu.Lock()
	_, retained := service.locks["session_lock"]
	service.locksMu.Unlock()
	if retained {
		t.Fatal("session lock entry remained after the last lease was released")
	}
}

func TestTemporarySessionUsesVolatileMonotonicIDs(t *testing.T) {
	durable := &memoryRepository{records: make(map[uint64]Record)}
	service, err := NewService(durable, &staticReader{content: "one\ntwo\nthree"})
	if err != nil {
		t.Fatal(err)
	}
	service.SetTemporary("session_temp")
	first, err := service.Create(t.Context(), "session_temp", "/repo", testAnchor(), "First")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(t.Context(), "session_temp", first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(t.Context(), "session_temp", "/repo", testAnchor(), "Second")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || second.ID != 2 || len(durable.records) != 0 {
		t.Fatalf("temporary ids = %d, %d; durable = %+v", first.ID, second.ID, durable.records)
	}
	service.ForgetSession("session_temp")
	service.SetTemporary("session_temp")
	third, err := service.Create(t.Context(), "session_temp", "/repo", testAnchor(), "Third")
	if err != nil || third.ID != 1 {
		t.Fatalf("recreated process-local session = %+v, %v", third, err)
	}
}
