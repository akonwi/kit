package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTryLockWithinGivesUpOnAHeldLock(t *testing.T) {
	var mu sync.Mutex
	if !tryLockWithin(t.Context(), &mu, time.Second) {
		t.Fatal("free lock was not acquired")
	}
	if tryLockWithin(t.Context(), &mu, 20*time.Millisecond) {
		t.Fatal("held lock was acquired")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if tryLockWithin(ctx, &mu, time.Hour) {
		t.Fatal("held lock was acquired after cancellation")
	}
	released := make(chan bool, 1)
	go func() { released <- tryLockWithin(t.Context(), &mu, 5*time.Second) }()
	time.Sleep(10 * time.Millisecond)
	mu.Unlock()
	if !<-released {
		t.Fatal("lock released while waiting was not acquired")
	}
}
