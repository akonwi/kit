package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/systemprompt"
)

const runtimeTransitionTimeout = 10 * time.Second

// ReloadResult describes the newly applied runtime bundle.
type ReloadResult struct {
	Sources       []systemprompt.Source
	Diagnostics   []systemprompt.Diagnostic
	Warnings      []string
	EventStreamID string
}

// ReloadSession rebuilds and atomically applies a session's prompt and tools.
// The authoritative droid Store and conversation history are preserved.
func (m *Manager) ReloadSession(ctx context.Context, sessionID string) (ReloadResult, error) {
	if err := m.beginOperation(); err != nil {
		return ReloadResult{}, err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return ReloadResult{}, err
	}
	if !loaded.admissionMu.TryLock() {
		return ReloadResult{}, ErrReloadBusy
	}
	defer loaded.admissionMu.Unlock()
	if !loaded.controlMu.TryLock() {
		return ReloadResult{}, ErrReloadBusy
	}
	defer loaded.controlMu.Unlock()
	if m.sessionDeleting(sessionID) {
		return ReloadResult{}, ErrDeleteBusy
	}
	loaded.stateMu.Lock()
	active := loaded.activeRun != ""
	loaded.stateMu.Unlock()
	if active {
		return ReloadResult{}, ErrReloadBusy
	}
	m.bashMu.Lock()
	bashActive := m.bashActive[sessionID] != nil
	m.bashMu.Unlock()
	if bashActive {
		return ReloadResult{}, ErrReloadBusy
	}
	if _, err := loaded.droid.WaitQuiescent(ctx); err != nil {
		if errors.Is(err, droids.ErrClosed) {
			return ReloadResult{}, err
		}
		return ReloadResult{}, fmt.Errorf("wait for session %q to become quiescent: %w", sessionID, err)
	}
	workspaceCWD, workspaceGeneration := loaded.workspace.snapshot()
	record, err := m.sessionRecord(ctx, sessionID)
	if err != nil {
		return ReloadResult{}, err
	}
	if record.CWD != workspaceCWD {
		return ReloadResult{}, fmt.Errorf("%w: session cwd changed while reload was starting", ErrReloadBusy)
	}
	replacement, err := m.bundleBuilder.Build(ctx, record, loaded.workspace.CWD)
	if err != nil {
		return ReloadResult{}, fmt.Errorf("build replacement runtime bundle for session %q: %w", sessionID, err)
	}
	replacement.Tools = append(replacement.Tools, m.changeCWDTool(sessionID, loaded.workspace))
	nextEvents, err := newEventLog()
	if err != nil {
		return ReloadResult{}, fmt.Errorf("prepare replacement event stream for session %q: %w", sessionID, err)
	}
	if m.isClosed() {
		return ReloadResult{}, ErrClosed
	}
	if m.sessionDeleting(sessionID) {
		return ReloadResult{}, ErrDeleteBusy
	}

	transitionContext, cancelTransition := context.WithTimeout(context.WithoutCancel(ctx), runtimeTransitionTimeout)
	defer cancelTransition()
	// Open the replacement against the quiescent Store before closing the current
	// droid. Neither runtime can mutate while admission and control are held, so
	// a failed open leaves the previous valid bundle untouched.
	replacementDroid, snapshot, err := m.openDroid(transitionContext, record, loaded.store, replacement)
	if err != nil {
		return ReloadResult{}, fmt.Errorf("open replacement droid: %w", err)
	}
	if m.isClosed() {
		_ = replacementDroid.Close()
		return ReloadResult{}, ErrClosed
	}
	loaded.workspace.mu.Lock()
	defer loaded.workspace.mu.Unlock()
	if loaded.workspace.generation != workspaceGeneration || loaded.workspace.cwd != workspaceCWD {
		_ = replacementDroid.Close()
		return ReloadResult{}, fmt.Errorf("%w: session cwd changed during reload", ErrReloadBusy)
	}
	closeErr := loaded.droid.Shutdown(transitionContext)
	loaded.droid = replacementDroid
	loaded.bundle = cloneRuntimeBundle(replacement)
	loaded.eventCursor = snapshot.LastEvent
	loaded.promptSources = append([]systemprompt.Source(nil), replacement.Prompt.Sources...)
	loaded.promptDiagnostics = append([]systemprompt.Diagnostic(nil), replacement.Prompt.Diagnostics...)
	loaded.events.replace(nextEvents)
	result := ReloadResult{
		Sources:       append([]systemprompt.Source(nil), loaded.promptSources...),
		Diagnostics:   append([]systemprompt.Diagnostic(nil), loaded.promptDiagnostics...),
		EventStreamID: nextEvents.streamID,
	}
	if closeErr != nil {
		result.Warnings = []string{boundedReloadWarning("Previous runtime shutdown reported: " + closeErr.Error())}
	}
	return result, nil
}

func boundedReloadWarning(message string) string {
	const maximum = 4096
	message = strings.ReplaceAll(strings.ToValidUTF8(message, "�"), "\x00", "�")
	if len(message) <= maximum {
		return message
	}
	message = message[:maximum]
	for !utf8.ValidString(message) {
		message = message[:len(message)-1]
	}
	return message
}

func promptMetadata(loaded *runtime) PromptMetadata {
	return PromptMetadata{
		Sources:     append([]systemprompt.Source(nil), loaded.promptSources...),
		Diagnostics: append([]systemprompt.Diagnostic(nil), loaded.promptDiagnostics...),
	}
}
