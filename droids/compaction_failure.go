package droids

import (
	"context"
	"errors"
)

// CompactionFailureReason classifies why a context compaction failed. The
// vocabulary is closed and carries no provider text, credentials, or
// conversation content, so it may be persisted and shown to clients.
type CompactionFailureReason string

const (
	// CompactionFailureSummaryTruncated reports a summary that stopped at the
	// compaction model's output limit.
	CompactionFailureSummaryTruncated CompactionFailureReason = "summary_truncated"
	// CompactionFailureSummaryInvalid reports a summary response that was
	// empty, called tools, or otherwise was not complete text.
	CompactionFailureSummaryInvalid CompactionFailureReason = "summary_invalid"
	// CompactionFailurePolicyStop reports a provider policy stop on a summary
	// request.
	CompactionFailurePolicyStop CompactionFailureReason = "policy_stop"
	// CompactionFailureProviderError reports a failed provider request: a
	// summary request or a context measurement.
	CompactionFailureProviderError CompactionFailureReason = "provider_error"
	// CompactionFailureNotAdaptable reports context that cannot be reduced to
	// fit the target and current models, or a summary model that cannot
	// accept the material.
	CompactionFailureNotAdaptable CompactionFailureReason = "not_adaptable"
	// CompactionFailureCanceled reports a compaction that was canceled, timed
	// out, or stopped because the droid closed.
	CompactionFailureCanceled CompactionFailureReason = "canceled"
	// CompactionFailureConflict reports a compaction refused as busy or
	// invalidated because the context or its configuration changed.
	CompactionFailureConflict CompactionFailureReason = "conflict"
	// CompactionFailureInternal reports any other failure, such as
	// persistence.
	CompactionFailureInternal CompactionFailureReason = "internal_error"
)

// Valid reports whether reason is one of the declared reasons.
func (reason CompactionFailureReason) Valid() bool {
	switch reason {
	case CompactionFailureSummaryTruncated, CompactionFailureSummaryInvalid, CompactionFailurePolicyStop,
		CompactionFailureProviderError, CompactionFailureNotAdaptable, CompactionFailureCanceled,
		CompactionFailureConflict, CompactionFailureInternal:
		return true
	}
	return false
}

// CompactionError is a failed compaction with its classified reason.
// CompactContext returns one for every failure. Cause is diagnostic: it may
// carry provider text and must not be shown to users or cross a process
// boundary.
type CompactionError struct {
	Reason CompactionFailureReason
	Cause  error
}

func (e *CompactionError) Error() string {
	if e.Cause == nil {
		return "droids: context compaction failed: " + string(e.Reason)
	}
	return e.Cause.Error()
}

// Unwrap returns the diagnostic cause.
func (e *CompactionError) Unwrap() error { return e.Cause }

// CompactionFailureOf reports the classified reason of a compaction failure.
// It is false when err is not a classified compaction failure.
func CompactionFailureOf(err error) (CompactionFailureReason, bool) {
	var failure *CompactionError
	if errors.As(err, &failure) && failure.Reason.Valid() {
		return failure.Reason, true
	}
	return "", false
}

// compactionFailed marks err, at the point it occurs, with its reason.
func compactionFailed(reason CompactionFailureReason, err error) error {
	return &CompactionError{Reason: reason, Cause: err}
}

// classifyCompactionFailure returns err as a classified compaction failure.
// ctx is the compaction's own context: a failure after it ended is a
// cancellation, whatever a provider reported. Otherwise the reason marked
// where the failure occurred wins, then the runtime's own sentinels.
func classifyCompactionFailure(ctx context.Context, err error) *CompactionError {
	if err == nil {
		return nil
	}
	var marked *CompactionError
	hasMark := errors.As(err, &marked) && marked.Reason.Valid()
	reason := CompactionFailureInternal
	switch {
	case ctx != nil && ctx.Err() != nil, errors.Is(err, ErrClosed):
		reason = CompactionFailureCanceled
	case hasMark:
		reason = marked.Reason
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		reason = CompactionFailureCanceled
	case errors.Is(err, ErrBusy), errors.Is(err, ErrConflict):
		reason = CompactionFailureConflict
	case errors.Is(err, ErrContextNotAdaptable):
		reason = CompactionFailureNotAdaptable
	}
	if top, ok := err.(*CompactionError); ok && top.Reason == reason {
		return top
	}
	return &CompactionError{Reason: reason, Cause: err}
}
