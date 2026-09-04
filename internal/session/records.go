package session

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound identifies a missing persisted session record.
var ErrNotFound = errors.New("session record not found")

// SessionRecord is Kit's persisted session metadata projection.
type SessionRecord struct {
	ID              string
	CWD             string
	Name            string
	Persistent      bool
	ParentSessionID string
	ModelProvider   string
	ModelID         string
	ThinkingLevel   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ArchivedAt      *time.Time
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

// TurnRecord identifies one user-initiated parent run in a session.
type TurnRecord struct {
	ID        string
	SessionID string
	Sequence  int64
	Status    RunStatus
	CreatedAt time.Time
	StartedAt *time.Time
	EndedAt   *time.Time
}

// ParentRunRecord is one execution attempt associated with a Kit turn.
type ParentRunRecord struct {
	ID        string
	SessionID string
	TurnID    string
	Status    RunStatus
	Error     string
	CreatedAt time.Time
	StartedAt *time.Time
	EndedAt   *time.Time
}

// RunStatus is a durable parent or subagent execution state.
type RunStatus string

const (
	RunStatusPending     RunStatus = "pending"
	RunStatusQueued      RunStatus = "queued"
	RunStatusRunning     RunStatus = "running"
	RunStatusCompleted   RunStatus = "completed"
	RunStatusFailed      RunStatus = "failed"
	RunStatusAborted     RunStatus = "aborted"
	RunStatusInterrupted RunStatus = "interrupted"
)

// NewMessageRecord is an encoded runtime message awaiting persistence. ID may
// be supplied when a live message identity was allocated before persistence.
type NewMessageRecord struct {
	ID          string
	Role        string
	PayloadJSON []byte
	CreatedAt   time.Time
}

// MessageRecord is one ordered persisted message.
type MessageRecord struct {
	ID          string
	SessionID   string
	TurnID      string
	Sequence    int64
	Role        string
	PayloadJSON []byte
	CreatedAt   time.Time
}

// Repository is the persistence port required by parent-session orchestration.
type Repository interface {
	CreateSession(context.Context, NewSession) (SessionRecord, error)
	GetSession(context.Context, string) (SessionRecord, error)
	ListSessions(context.Context, string) ([]SessionRecord, error)
	GetParentRun(context.Context, string, string) (ParentRunRecord, error)
	GetActiveParentRun(context.Context, string) (ParentRunRecord, error)
	ReserveParentRun(context.Context, string, string, string) (TurnRecord, ParentRunRecord, error)
	StartReservedParentRun(context.Context, string, string) (string, RunStatus, error)
	AbortReservedParentRun(context.Context, string, string, string) (RunStatus, error)
	FinishParentRun(context.Context, string, string, string, RunStatus, string) error
	RecoverParentRun(context.Context, string, string, string, string) (RunStatus, error)
	AppendMessages(context.Context, string, string, []NewMessageRecord) ([]MessageRecord, error)
	ListMessages(context.Context, string) ([]MessageRecord, error)
	ListReplayMessages(context.Context, string) ([]MessageRecord, error)
	AppendSessionEvents(context.Context, []NewEvent) ([]Event, error)
	ListSessionEvents(context.Context, string, int64, int) (EventPage, error)
}
