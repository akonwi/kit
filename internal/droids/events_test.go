package droids

import (
	"testing"
	"time"
)

func newIdleDroid(t *testing.T) *Droid {
	t.Helper()
	d, err := New(Options{Providers: toolThenStop(t, "noop"), Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Events returns the same channel on every call.
func TestEventsReturnsSameChannel(t *testing.T) {
	d := newIdleDroid(t)
	defer d.Close()
	if d.Events() != d.Events() {
		t.Fatal("Events returned different channels")
	}
}

// The channel obtained before Close is the same one, and it closes on Close.
func TestEventsChannelClosesOnClose(t *testing.T) {
	d := newIdleDroid(t)
	ch := d.Events()
	d.Close()

	if got := d.Events(); got != ch {
		t.Fatal("Events returned a different channel after Close")
	}
	assertClosed(t, ch)
}

// Calling Events for the first time after Close returns an already-closed
// channel (not a fresh one that never closes).
func TestEventsAfterCloseReturnsClosedChannel(t *testing.T) {
	d := newIdleDroid(t)
	d.Close()

	ch := d.Events()
	if ch != d.Events() {
		t.Fatal("post-close Events returned different channels")
	}
	assertClosed(t, ch)
}

func assertClosed(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel, received a value")
		}
	case <-time.After(time.Second):
		t.Fatal("event channel was not closed")
	}
}
