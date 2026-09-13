// Package sessionclient owns renderer-neutral server and bound-session contracts.
package sessionclient

import (
	"context"
	"errors"
	"io"

	"github.com/akonwi/kit/internal/protocol"
)

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

// Session is immutably bound to one authoritative session for its lifetime.
type Session interface {
	ID() string
	Snapshot(context.Context) (protocol.SessionSnapshot, error)
	VCSStatus(context.Context) (protocol.SessionVCSStatus, error)
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

// ErrTranscriptCursorUnavailable indicates that older history must be restarted from a fresh snapshot.
var ErrTranscriptCursorUnavailable = errors.New("transcript cursor is unavailable")

// TranscriptPager is the optional older-history pagination surface.
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
