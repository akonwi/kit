// Package sessionclient owns renderer-neutral server and bound-session contracts.
package sessionclient

import (
	"context"

	"github.com/akonwi/kit/internal/protocol"
)

// Server discovers, creates, and binds authoritative sessions.
type Server interface {
	CreateSession(context.Context, protocol.CreateSessionInput) (protocol.SessionInfo, error)
	ListSessions(context.Context, string) ([]protocol.SessionInfo, error)
	Attach(context.Context, string) (Session, error)
}

// Session is immutably bound to one authoritative session for its lifetime.
type Session interface {
	ID() string
	StartPrompt(context.Context, string) (Run, error)
}

// Run is one generation-bound parent execution. Waiting may be detached or
// canceled without aborting; Abort explicitly targets only this run id.
type Run interface {
	ID() string
	Wait(context.Context) (protocol.PromptOutcome, error)
	Abort(context.Context) error
}
