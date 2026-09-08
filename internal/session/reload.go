package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/systemprompt"
)

const runtimeTransitionTimeout = 10 * time.Second

// ReloadSession rebuilds and atomically applies a session's prompt and tools.
// The authoritative droid Store and conversation history are preserved.
func (m *Manager) ReloadSession(ctx context.Context, sessionID string) (PromptMetadata, error) {
	if err := m.beginOperation(); err != nil {
		return PromptMetadata{}, err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return PromptMetadata{}, err
	}
	if !loaded.admissionMu.TryLock() {
		return PromptMetadata{}, ErrReloadBusy
	}
	defer loaded.admissionMu.Unlock()
	if !loaded.controlMu.TryLock() {
		return PromptMetadata{}, ErrReloadBusy
	}
	defer loaded.controlMu.Unlock()
	if m.sessionDeleting(sessionID) {
		return PromptMetadata{}, ErrDeleteBusy
	}
	loaded.stateMu.Lock()
	active := loaded.activeRun != ""
	loaded.stateMu.Unlock()
	if active {
		return PromptMetadata{}, ErrReloadBusy
	}
	m.bashMu.Lock()
	bashActive := m.bashActive[sessionID] != nil
	m.bashMu.Unlock()
	if bashActive {
		return PromptMetadata{}, ErrReloadBusy
	}
	if _, err := loaded.droid.WaitQuiescent(ctx); err != nil {
		if errors.Is(err, droids.ErrClosed) {
			return PromptMetadata{}, err
		}
		return PromptMetadata{}, fmt.Errorf("wait for session %q to become quiescent: %w", sessionID, err)
	}
	record, err := m.sessionRecord(ctx, sessionID)
	if err != nil {
		return PromptMetadata{}, err
	}
	replacement, err := m.bundleBuilder.Build(ctx, record)
	if err != nil {
		return PromptMetadata{}, fmt.Errorf("build replacement runtime bundle for session %q: %w", sessionID, err)
	}
	nextEvents, err := newEventLog()
	if err != nil {
		return PromptMetadata{}, fmt.Errorf("prepare replacement event stream for session %q: %w", sessionID, err)
	}
	if m.isClosed() {
		return PromptMetadata{}, ErrClosed
	}
	if m.sessionDeleting(sessionID) {
		return PromptMetadata{}, ErrDeleteBusy
	}

	transitionContext, cancelTransition := context.WithTimeout(context.WithoutCancel(ctx), runtimeTransitionTimeout)
	defer cancelTransition()
	// Open the replacement against the quiescent Store before closing the current
	// droid. Neither runtime can mutate while admission and control are held, so
	// a failed open leaves the previous valid bundle untouched.
	replacementDroid, snapshot, err := m.openDroid(transitionContext, record, loaded.store, replacement)
	if err != nil {
		return PromptMetadata{}, fmt.Errorf("open replacement droid: %w", err)
	}
	if m.isClosed() {
		_ = replacementDroid.Close()
		return PromptMetadata{}, ErrClosed
	}
	closeErr := loaded.droid.Shutdown(transitionContext)
	loaded.droid = replacementDroid
	loaded.bundle = cloneRuntimeBundle(replacement)
	loaded.eventCursor = snapshot.LastEvent
	loaded.promptSources = append([]systemprompt.Source(nil), replacement.Prompt.Sources...)
	loaded.promptDiagnostics = append([]systemprompt.Diagnostic(nil), replacement.Prompt.Diagnostics...)
	loaded.events.replace(nextEvents)
	metadata := promptMetadata(loaded)
	if closeErr != nil {
		return metadata, fmt.Errorf("replacement applied after current droid shutdown failed: %w", closeErr)
	}
	return metadata, nil
}

func promptMetadata(loaded *runtime) PromptMetadata {
	return PromptMetadata{
		Sources:     append([]systemprompt.Source(nil), loaded.promptSources...),
		Diagnostics: append([]systemprompt.Diagnostic(nil), loaded.promptDiagnostics...),
	}
}
