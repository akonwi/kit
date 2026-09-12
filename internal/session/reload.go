package session

import (
	"context"
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
	if !loaded.transitionMu.TryLock() {
		return ReloadResult{}, ErrReloadBusy
	}
	defer loaded.transitionMu.Unlock()
	if m.sessionDeleting(sessionID) {
		return ReloadResult{}, ErrDeleteBusy
	}
	workspaceCWD, workspaceGeneration := loaded.workspace.snapshot()
	record, err := m.sessionRecord(ctx, sessionID)
	if err != nil {
		return ReloadResult{}, err
	}
	if record.CWD != workspaceCWD {
		return ReloadResult{}, fmt.Errorf("%w: session cwd changed while reload was starting", ErrReloadBusy)
	}
	replacement, errnth := m.bundleBuilder.Build(ctx, record, loaded.workspace.CWD)
	if errnth != nil {
		return ReloadResult{}, fmt.Errorf("build replacement runtime bundle for session %q: %w", sessionID, errnth)
	}
	replacement.Prompt.Prompt += interactionPromptGuidance
	replacement.Tools = append(replacement.Tools, interactionTools(sessionID, loaded.interactions)...)
	replacement.Tools = append(replacement.Tools, m.changeCWDTool(sessionID, loaded.workspace))
	if m.isClosed() {
		return ReloadResult{}, ErrClosed
	}
	currentCWD, currentGeneration := loaded.workspace.snapshot()
	if currentGeneration != workspaceGeneration || currentCWD != workspaceCWD {
		return ReloadResult{}, fmt.Errorf("%w: session cwd changed during reload", ErrReloadBusy)
	}

	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	loaded.workspace.mutationMu.Lock()
	defer loaded.workspace.mutationMu.Unlock()
	if m.sessionDeleting(sessionID) {
		return ReloadResult{}, ErrDeleteBusy
	}
	currentCWD, currentGeneration = loaded.workspace.snapshot()
	if currentGeneration != workspaceGeneration || currentCWD != workspaceCWD {
		return ReloadResult{}, fmt.Errorf("%w: session cwd changed during reload", ErrReloadBusy)
	}
	if err := loaded.droid.Reconfigure(droids.RequestConfiguration{
		SystemPrompt: replacement.Prompt.Prompt, Reasoning: record.ThinkingLevel, Tools: replacement.Tools,
	}); err != nil {
		return ReloadResult{}, fmt.Errorf("apply replacement runtime bundle for session %q: %w", sessionID, err)
	}
	loaded.bundle = cloneRuntimeBundle(replacement)
	return ReloadResult{
		Sources:       append([]systemprompt.Source(nil), loaded.bundle.Prompt.Sources...),
		Diagnostics:   append([]systemprompt.Diagnostic(nil), loaded.bundle.Prompt.Diagnostics...),
		EventStreamID: loaded.events.streamID,
	}, nil
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
		Sources:     append([]systemprompt.Source(nil), loaded.bundle.Prompt.Sources...),
		Diagnostics: append([]systemprompt.Diagnostic(nil), loaded.bundle.Prompt.Diagnostics...),
	}
}
