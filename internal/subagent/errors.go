package subagent

import "errors"

var (
	ErrNotFound             = errors.New("subagent record not found")
	ErrQueueFull            = errors.New("SUBAGENT_QUEUE_FULL")
	ErrConflict             = errors.New("subagent state conflict")
	ErrDismissed            = errors.New("subagent conversation dismissed")
	ErrNotCancelable        = errors.New("subagent task is not cancelable")
	ErrTemporaryUnavailable = errors.New("SUBAGENT_UNAVAILABLE_TEMPORARY_SESSION")
	ErrClosed               = errors.New("subagent supervisor is closed")
	ErrInvalidInput         = errors.New("invalid subagent input")
	// ErrTranscriptCursorUnavailable means an older-history cursor does not
	// identify a complete-turn boundary that still exists.
	ErrTranscriptCursorUnavailable = errors.New("subagent transcript cursor is unavailable")
)
