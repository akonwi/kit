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
