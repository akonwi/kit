package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

// IsIncompatibleDaemon reports a terminal compatibility failure across the
// renderer-neutral client boundary. Ordinary transport errors remain retryable.
func IsIncompatibleDaemon(err error) bool {
	if errors.Is(err, kit.ErrIncompatibleServer) {
		return true
	}
	var mismatch interface{ IncompatibleDaemon() bool }
	return errors.As(err, &mismatch) && mismatch.IncompatibleDaemon()
}

// Server discovers, creates, and binds authoritative sessions.
type Server interface {
	CreateSession(context.Context, protocol.CreateSessionInput) (protocol.SessionInfo, error)
	ForkSession(context.Context, string, protocol.ForkSessionInput) (protocol.ForkSessionResult, error)
	RenameSession(context.Context, string, string) (protocol.SessionInfo, error)
	DeleteSession(context.Context, string) error
	DisposeTemporarySession(context.Context, string) error
	ListSessions(context.Context, string) ([]protocol.SessionInfo, error)
	Models(context.Context) (protocol.ModelCatalog, error)
	Attach(context.Context, string) (Session, error)
}

// CompatibilityProber is an optional read-only server capability. It verifies
// the currently registered daemon without starting or replacing it.
// Reattachment uses it before binding a new session client.
type CompatibilityProber interface {
	ProbeCompatibility(context.Context) error
}

// ModelCatalogRefresher is the optional server capability for refreshing models.dev.
type ModelCatalogRefresher interface {
	RefreshModels(context.Context) (protocol.ModelCatalog, error)
}

// Session is immutably bound to one authoritative session for its lifetime.
type Session interface {
	ID() string
	Snapshot(context.Context) (protocol.SessionSnapshot, error)
	VCSStatus(context.Context) (protocol.SessionVCSStatus, error)
	// WatchVCS blocks on the server-pushed repository-status stream, invoking
	// receive for every validated update until the context is canceled or the
	// stream fails. Reconnection starts fresh; there is no cursor or replay.
	// Terminal failures are wrapped in *StreamWatchTerminalError.
	WatchVCS(context.Context, func(protocol.SessionVCSStatus)) error
	FileIndex(context.Context) (protocol.SessionFileIndex, error)
	ChangeCWD(context.Context, string) (protocol.SessionInfo, error)
	Reload(context.Context) (protocol.ReloadSessionResult, error)
	Configure(context.Context, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	Compact(context.Context, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
	Turn(context.Context, string) (protocol.TurnInfo, error)
	Stream(context.Context, string) (EventStream, error)
	StartPrompt(context.Context, string) (Turn, error)
	StartPromptCommand(context.Context, string, string) (Turn, error)
	Bash(context.Context, string) (BashExecution, error)
	StartBash(context.Context, string, string, bool) (BashExecution, error)
	AbortBash(context.Context, string) error
	Abort(context.Context, string) error
	Subagent(context.Context, protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error)
	// SubagentTranscript loads one complete-turn child page. An empty before
	// selects the newest page.
	SubagentTranscript(context.Context, string, string) (protocol.SubagentTranscript, error)
}

// BashHistorySession is the optional bound-session durable direct-bash history
// capability. It is distinct from the transcript projection: recall must read
// persisted session history, not the loaded messages.
type BashHistorySession interface {
	BashHistory(context.Context, uint64, int) (protocol.BashHistoryPage, error)
}

// ScratchpadSession is the optional bound-session shared scratchpad capability.
type ScratchpadSession interface {
	Scratchpad(context.Context) (protocol.Scratchpad, error)
	UpdateScratchpad(context.Context, protocol.UpdateScratchpadInput) (protocol.Scratchpad, error)
}

// StreamWatchTerminalError marks server-push stream failures that must stop the
// watcher instead of reconnecting (ADR 0035): authentication, missing sessions,
// incompatible daemons, and protocol violations such as oversized or malformed
// records and pre-stream responses outside the operation's declared errors.
// Every other failure, including a clean end, is transient.
type StreamWatchTerminalError struct{ Err error }

func (e *StreamWatchTerminalError) Error() string { return e.Err.Error() }
func (e *StreamWatchTerminalError) Unwrap() error { return e.Err }

// FileIndexRefreshSession is the optional bound-session forced index-refresh facet.
type FileIndexRefreshSession interface {
	RefreshFileIndex(context.Context) (protocol.SessionFileIndex, error)
}

// WorkspaceFilesSession is the optional bounded workspace exploration surface.
type WorkspaceFilesSession interface {
	WorkspaceLimits() protocol.WorkspaceLimits
	Workspace(context.Context) (protocol.WorkspaceRef, error)
	ListDirectory(context.Context, protocol.ListDirectoryInput) (protocol.DirectoryPage, error)
	ReadWorkspaceFile(context.Context, protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error)
}

// DiffSession is the optional server-authoritative generalized diff surface.
type DiffSession interface {
	ListDiffTargets(context.Context, protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error)
	ObserveDiff(context.Context, protocol.ObserveDiffInput) (protocol.DiffPage, error)
	ReadFileDiff(context.Context, protocol.ReadFileDiffInput) (protocol.FileDiffPage, error)
}

// WorkingTreeDiffSession is the compatibility surface for clients that have
// not yet adopted the generalized target catalog.
type WorkingTreeDiffSession interface {
	ObserveWorkingTree(context.Context, protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error)
	ReadFileDiff(context.Context, protocol.ReadFileDiffInput) (protocol.FileDiffPage, error)
}

// AnnotationSession is the optional server-owned draft annotation surface.
type AnnotationSession interface {
	ListAnnotations(context.Context, protocol.ListAnnotationsInput) (protocol.AnnotationPage, error)
	CreateAnnotation(context.Context, protocol.CreateAnnotationInput) (protocol.Annotation, error)
	UpdateAnnotation(context.Context, protocol.UpdateAnnotationInput) (protocol.Annotation, error)
	DeleteAnnotation(context.Context, protocol.DeleteAnnotationInput) error
}

// ErrTranscriptCursorUnavailable indicates that older history must be restarted from a fresh snapshot.
var ErrTranscriptCursorUnavailable = errors.New("transcript cursor is unavailable")

// MessagePager is the optional generic durable-message history surface.
type MessagePager interface {
	MessagePage(context.Context, protocol.MessagePageQuery) (protocol.MessagePage, error)
}

// TranscriptPager is the optional complete-turn transcript history surface.
type TranscriptPager interface {
	TranscriptPage(context.Context, string) (protocol.TranscriptPage, error)
}

// SubagentEventReader is the optional child live-event synchronization surface.
// InteractionSession is the optional structured model-user response surface.
type InteractionSession interface {
	RespondInteraction(context.Context, protocol.InteractionResponse) error
}

type SubagentEventReader interface {
	SubagentEvents(context.Context, string, string, int64) (protocol.SubagentLiveEventPage, error)
}

// SubagentConfigurationSession changes one durable child conversation's
// server-authoritative model or thinking setting.
type SubagentConfigurationSession interface {
	ConfigureSubagent(context.Context, string, protocol.ConfigureSubagentInput) (protocol.ConfigureSubagentResult, error)
}

// SessionEventWatcher is the optional attachment-scoped event surface. It
// carries all session events so an idle client can discover externally admitted
// turns as well as session-level invalidations.
type SessionEventWatcher interface {
	Watch(context.Context) (protocol.SessionSnapshot, EventStream, error)
}

// StructuredPromptSession admits prompts and follow-ups with durable attachment IDs.
// AttachmentSession streams validated files to and from session-owned storage.
type AttachmentSession interface {
	UploadAttachment(context.Context, string, io.Reader) (protocol.AttachmentInfo, error)
	OpenAttachment(context.Context, string) (protocol.AttachmentInfo, io.ReadCloser, error)
}

// AttachmentMetadataSession resolves attachment identities without transferring bytes.
type AttachmentMetadataSession interface {
	ResolveAttachments(context.Context, []string) (protocol.AttachmentResolution, error)
}

type StructuredPromptSession interface {
	StartPromptInput(context.Context, protocol.PromptInput) (Turn, error)
	SubmitPromptInput(context.Context, protocol.PromptInput) (PromptSubmission, error)
}

// FollowUpSession is the optional queue-aware prompt surface.
type FollowUpSession interface {
	SubmitPrompt(context.Context, string) (PromptSubmission, error)
	RestoreFollowUps(context.Context) (protocol.RestoreFollowUpsResult, error)
	PromoteFollowUps(context.Context) (protocol.PromoteFollowUpsResult, error)
}

// PromptSubmission reports whether a prompt started or became a follow-up.
type PromptSubmission struct {
	Turn   Turn
	Queued bool
	Queue  protocol.FollowUpQueue
}

// Turn is one droid turn handle. Waiting may be detached or canceled without
// aborting; Abort explicitly targets only this droid turn identity.
type Turn interface {
	ID() string
	Wait(context.Context) (protocol.PromptOutcome, error)
	Abort(context.Context) error
}

// BashExecution is one generation-bound direct shell execution. Waiting may
// detach without aborting; Abort explicitly targets only this execution id.
type BashExecution interface {
	ID() string
	State() protocol.BashExecution
	Wait(context.Context) (protocol.BashExecution, error)
	Abort(context.Context) error
}

// EventStream delivers ordered batches for one turn. Err is available after
// Updates closes; cancellation of the stream does not abort the turn.
type EventStream interface {
	Updates() <-chan []protocol.SessionEvent
	Err() error
}

// PluginCommandSession is the optional executable-plugin contribution capability.
// Callers retain the catalog instance token and must never replay failed commands.
type PluginCommandSession interface {
	ExecutePluginCommand(context.Context, protocol.PluginCommandInput) error
}

// PluginToastSession observes live notifications only. Reconnection starts fresh;
// persistent plugin notices are also represented by session diagnostics.
type PluginToastSession interface {
	// WatchPluginToasts opens one fresh live stream. Terminal open failures
	// are wrapped in *StreamWatchTerminalError.
	WatchPluginToasts(context.Context) (PluginToastStream, error)
}

// PluginToastStream is owned by the watch context and closes on detach. Err is
// available after Updates closes: nil for a clean end, and terminal failures
// are wrapped in *StreamWatchTerminalError. No cursor, history, or offline
// replay exists.
type PluginToastStream interface {
	Updates() <-chan protocol.PluginToast
	Err() error
}

var (
	// ErrSessionNotFound indicates that no saved session matches a selector.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionAmbiguous indicates that a short selector matches several sessions.
	ErrSessionAmbiguous = errors.New("session selector is ambiguous")
)

// ResolveSession finds an exact or uniquely prefixed saved session. Exact IDs
// win over short-ID matching. Short selectors may omit the session_ prefix.
func ResolveSession(ctx context.Context, server Server, selector string) (protocol.SessionInfo, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return protocol.SessionInfo{}, fmt.Errorf("%w: selector is empty", ErrSessionNotFound)
	}
	sessions, err := server.ListSessions(ctx, "")
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	for _, candidate := range sessions {
		if candidate.ID == selector {
			return candidate, nil
		}
	}
	canonicalPrefix := selector
	if !strings.HasPrefix(canonicalPrefix, "session_") {
		canonicalPrefix = "session_" + canonicalPrefix
	}
	var match protocol.SessionInfo
	matches := 0
	for _, candidate := range sessions {
		if strings.HasPrefix(candidate.ID, canonicalPrefix) {
			match = candidate
			matches++
		}
	}
	switch matches {
	case 0:
		return protocol.SessionInfo{}, fmt.Errorf("%w: %s", ErrSessionNotFound, selector)
	case 1:
		return match, nil
	default:
		return protocol.SessionInfo{}, fmt.Errorf("%w: %s matches %d sessions", ErrSessionAmbiguous, selector, matches)
	}
}

// ResolveAvailableModel selects an authoritative available model. An explicit
// unavailable selection fails; an implicit stale preference falls back within
// its provider before using the first available catalog entry.
func ResolveAvailableModel(catalog protocol.ModelCatalog, preferred string, explicit bool) (string, error) {
	for _, model := range catalog.Models {
		if model.ID == preferred && model.Available {
			return model.ID, nil
		}
	}
	if explicit {
		return "", fmt.Errorf("model %q is not available", preferred)
	}
	preferredProvider, _, _ := strings.Cut(preferred, "/")
	for _, model := range catalog.Models {
		if model.Provider == preferredProvider && model.Available {
			return model.ID, nil
		}
	}
	for _, model := range catalog.Models {
		if model.Available {
			return model.ID, nil
		}
	}
	return "", fmt.Errorf("no authenticated model is available")
}
