package session

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound identifies missing persisted session or transient harness state.
var ErrNotFound = errors.New("session record not found")

// ErrConfigurationConflict identifies a stale expected configuration revision.
var ErrConfigurationConflict = errors.New("session configuration revision conflict")

// ConfigurationConflictError reports the expected and authoritative revisions.
type ConfigurationConflictError struct {
	Expected uint64
	Actual   uint64
}

func (err *ConfigurationConflictError) Error() string {
	return fmt.Sprintf("%v: expected %d, actual %d", ErrConfigurationConflict, err.Expected, err.Actual)
}

func (*ConfigurationConflictError) Unwrap() error { return ErrConfigurationConflict }

// SessionRecord is Kit's persisted session registry entry.
type SessionRecord struct {
	ID                    string
	CWD                   string
	Name                  string
	Persistent            bool
	ParentSessionID       string
	ParentSessionName     string // projected from the parent registry row; not persisted on the child
	ModelProvider         string
	ModelID               string
	ThinkingLevel         string
	ConfigurationRevision uint64
	DroidInitializedAt    *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ArchivedAt            *time.Time
}

// NewSession contains metadata required to create a persisted session.
type NewSession struct {
	ID                 string
	CWD                string
	Name               string
	Persistent         bool
	ParentSessionID    string
	ModelProvider      string
	ModelID            string
	ThinkingLevel      string
	DroidInitializedAt *time.Time
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

// ConfigurationUpdate is one atomic exact model and thinking change guarded by
// the configuration revision observed by the client.
type ConfigurationUpdate struct {
	SessionID        string
	ExpectedRevision uint64
	ModelProvider    string
	ModelID          string
	ThinkingLevel    string
}

// CWDMutation is an idempotent durable session workspace change.
type CWDMutation struct {
	ID          string
	SessionID   string
	TargetPath  string
	PreviousCWD string
	CWD         string
	Changed     bool
}

// Repository is the persistence port for Kit's session registry. Conversation
// turns, executions, messages, and events belong to each session's droid Store.
type Repository interface {
	CreateSession(context.Context, NewSession) (SessionRecord, error)
	GetSessionCWDMutation(context.Context, string, string) (CWDMutation, error)
	ApplySessionCWDMutation(context.Context, CWDMutation) (SessionRecord, CWDMutation, error)
	UpdateSessionConfiguration(context.Context, ConfigurationUpdate) (SessionRecord, error)
	RenameSession(context.Context, string, string) (SessionRecord, error)
	TouchSession(context.Context, string, time.Time) error
	ArchiveSession(context.Context, string, time.Time) error
	GetSession(context.Context, string) (SessionRecord, error)
	ListSessions(context.Context, string) ([]SessionRecord, error)
	MarkDroidInitialized(context.Context, string, time.Time) error
}
