// Package sessionclient owns renderer-neutral server and bound-session contracts.
package sessionclient

import (
	"context"
	"errors"
	"io"

	"github.com/akonwi/kit/internal/protocol"
)

// IsIncompatibleDaemon reports a terminal compatibility failure across the
// renderer-neutral client boundary. Ordinary transport errors remain retryable.
func IsIncompatibleDaemon(err error) bool {
	var mismatch interface{ IncompatibleDaemon() bool }
	return errors.As(err, &mismatch) && mismatch.IncompatibleDaemon()
}

// Server discovers, creates, and binds authoritative sessions.
type Server interface {
	CreateSession(context.Context, protocol.CreateSessionInput) (protocol.SessionInfo, error)
	ForkSession(context.Context, string, protocol.ForkSessionInput) (protocol.SessionInfo, error)
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
	// Terminal failures are wrapped in *VCSWatchTerminalError.
	WatchVCS(context.Context, func(protocol.SessionVCSStatus)) error
	FileIndex(context.Context) (protocol.SessionFileIndex, error)
	ChangeCWD(context.Context, string) (protocol.SessionInfo, error)
	Reload(context.Context) (protocol.ReloadSessionResult, error)
	Configure(context.Context, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	Compact(context.Context, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
	Run(context.Context, string) (protocol.RunInfo, error)
	Stream(context.Context, string) (EventStream, error)
	StartPrompt(context.Context, string) (Run, error)
	StartPromptCommand(context.Context, string, string) (Run, error)
	Bash(context.Context, string) (BashExecution, error)
	StartBash(context.Context, string, string, bool) (BashExecution, error)
	AbortBash(context.Context, string) error
	Abort(context.Context, string) error
	Subagent(context.Context, protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error)
	SubagentTranscript(context.Context, string) (protocol.SubagentTranscript, error)
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

// VCSWatchTerminalError marks repository-stream failures that must stop the
// watcher instead of reconnecting: authentication, missing sessions, and
// protocol violations such as oversized or malformed frames.
type VCSWatchTerminalError struct{ Err error }

func (e *VCSWatchTerminalError) Error() string { return e.Err.Error() }
func (e *VCSWatchTerminalError) Unwrap() error { return e.Err }

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

// SessionEventWatcher is the optional attachment-scoped event surface. It
// carries all session events so an idle client can discover externally admitted
// runs as well as session-level invalidations.
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
	StartPromptInput(context.Context, protocol.PromptInput) (Run, error)
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
	Run    Run
	Queued bool
	Queue  protocol.FollowUpQueue
}

// Run is one droid turn handle. Waiting may be detached or canceled without
// aborting; Abort explicitly targets only this droid turn identity.
type Run interface {
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

// EventStream delivers ordered batches for one run. Err is available after
// Updates closes; cancellation of the stream does not abort the run.
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
	WatchPluginToasts(context.Context) (PluginToastStream, error)
}

// PluginToastStream is owned by the watch context and closes on detach. Err is
// available after Updates closes. No cursor, history, or offline replay exists.
type PluginToastStream interface {
	Updates() <-chan protocol.PluginToast
	Err() error
}
