// Package contextbridge adapts context return shapes that Ard cannot import.
package contextbridge

import (
	"context"
	"time"
)

// Cancellation packages context.WithCancel's two non-error results.
type Cancellation struct {
	Context context.Context
	Cancel  func()
}

// WithCancel returns a cancelable child of parent.
func WithCancel(parent context.Context) Cancellation {
	ctx, cancel := context.WithCancel(parent)
	return Cancellation{Context: ctx, Cancel: cancel}
}

// WithTimeout returns a cancelable child that expires after milliseconds.
func WithTimeout(parent context.Context, milliseconds int64) Cancellation {
	ctx, cancel := context.WithTimeout(parent, time.Duration(milliseconds)*time.Millisecond)
	return Cancellation{Context: ctx, Cancel: cancel}
}

// Wait blocks for milliseconds and reports whether the delay elapsed before
// ctx ended.
func Wait(ctx context.Context, milliseconds int64) bool {
	timer := time.NewTimer(time.Duration(milliseconds) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
