package session

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound identifies missing persisted session or transient harness state.
var ErrNotFound = errors.New("session record not found")

// SessionRecord is Kit's persisted session registry entry.
type SessionRecord struct {
	ID                 string
	CWD                string
	Name               string
	Persistent         bool
	ParentSessionID    string
	ModelProvider      string
	ModelID            string
	ThinkingLevel      string
	DroidInitializedAt *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ArchivedAt         *time.Time
}

// NewSession contains metadata required to create a persisted session.
type NewSession struct {
	ID              string
	CWD             string
	Name            string
	Persistent      bool
	ParentSessionID string
	ModelProvider   string
	ModelID         string
	ThinkingLevel   string
}

// RunStatus is the renderer-neutral state of an in-memory droid turn handle.
type RunStatus string

const (
	RunStatusQueued      RunStatus = "queued"
	RunStatusRunning     RunStatus = "running"
	RunStatusCompleted   RunStatus = "completed"
	RunStatusFailed      RunStatus = "failed"
	RunStatusAborted     RunStatus = "aborted"
	RunStatusInterrupted RunStatus = "interrupted"
)

// RunProjection is a transient projection of one droid turn.
type RunProjection struct {
	ID        string
	SessionID string
	TurnID    string
	Status    RunStatus
	Error     string
}

// Repository is the persistence port for Kit's session registry. Conversation
// turns, executions, messages, and events belong to each session's droid Store.
type Repository interface {
	CreateSession(context.Context, NewSession) (SessionRecord, error)
	RenameSession(context.Context, string, string) (SessionRecord, error)
	ArchiveSession(context.Context, string, time.Time) error
	GetSession(context.Context, string) (SessionRecord, error)
	ListSessions(context.Context, string) ([]SessionRecord, error)
	MarkDroidInitialized(context.Context, string, time.Time) error
}
