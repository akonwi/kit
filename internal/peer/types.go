// Package peer owns durable, symmetric queries between persistent top-level sessions.
package peer

import (
	"context"
	"errors"
	"time"
)

type State string

const (
	StateQueued               State = "queued"
	StateProcessing           State = "processing"
	StateCompleted            State = "completed"
	StateFailed               State = "failed"
	StateAborted              State = "aborted"
	StateInterrupted          State = "interrupted"
	StateRecipientArchived    State = "recipient_archived"
	StateRecipientUnavailable State = "recipient_unavailable"
)

func (s State) Terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateAborted, StateInterrupted, StateRecipientArchived, StateRecipientUnavailable:
		return true
	default:
		return false
	}
}

var (
	ErrNotFound    = errors.New("peer query not found")
	ErrInvalid     = errors.New("invalid peer query")
	ErrConflict    = errors.New("peer query generation conflict")
	ErrQueueFull   = errors.New("peer query queue capacity reached")
	ErrUnavailable = errors.New("peer session unavailable")
)

type Request struct {
	ID                 string
	IdempotencyKey     string
	SenderSessionID    string
	RecipientSessionID string
	Message            string
	ThreadID           string
	PrecedingRequestID string
	Route              []string
	HopCount           int
	State              State
	RecipientTurnID    string
	Result             string
	Error              string
	Generation         uint64
	CreatedAt          time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
}

type Admission struct {
	ID                 string
	IdempotencyKey     string
	SenderSessionID    string
	RecipientSessionID string
	Message            string
	ThreadID           string
	PrecedingRequestID string
	Route              []string
	HopCount           int
	Now                time.Time
}

type Completion struct {
	ID              string
	RecipientTurnID string
	Generation      uint64
	State           State
	Result          string
	Error           string
	FinishedAt      time.Time
}

type Limits struct {
	MaxMessageBytes       int
	MaxResultBytes        int
	MaxQueuedGlobal       int
	MaxQueuedPerSender    int
	MaxQueuedPerRecipient int
	MaxHopCount           int
	MaxThreadDepth        int
	MaxTerminalRetained   int
}

func DefaultLimits() Limits {
	return Limits{
		MaxMessageBytes: 16 << 10, MaxResultBytes: 64 << 10,
		MaxQueuedGlobal: 256, MaxQueuedPerSender: 32, MaxQueuedPerRecipient: 32,
		MaxHopCount: 8, MaxThreadDepth: 16, MaxTerminalRetained: 2048,
	}
}

type Repository interface {
	AdmitPeerQuery(context.Context, Admission, Limits) (Request, bool, error)
	PeerQuery(context.Context, string) (Request, error)
	ClaimNextPeerQuery(context.Context, string, time.Time) (Request, error)
	BindPeerRecipientTurn(context.Context, string, uint64, string) (Request, error)
	CompletePeerQuery(context.Context, Completion, Limits) (Request, error)
	PendingPeerRecipients(context.Context, string, int) ([]string, error)
	RecoverPeerQueries(context.Context, time.Time, Limits) ([]Request, error)
	PeerQueryByRecipientTurn(context.Context, string, string) (Request, error)
	TerminalPeerResults(context.Context, int) ([]Request, error)
}

type Session struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	CWD             string `json:"cwd"`
	ParentSessionID string `json:"parentSessionId,omitempty"`
	Availability    string `json:"availability"`
}
