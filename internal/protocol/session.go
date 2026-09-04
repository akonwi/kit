// Package protocol owns Kit's renderer-neutral session wire vocabulary.
package protocol

// CreateSessionInput requests a new persisted session.
type CreateSessionInput struct {
	CWD           string `json:"cwd"`
	Name          string `json:"name,omitempty"`
	Model         string `json:"model"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

// ReserveRunInput requests a durable generation-bound run handle.
type ReserveRunInput struct {
	RunID string `json:"runId"`
}

// RunReservation acknowledges a durable queued parent run.
type RunReservation struct {
	SessionID string `json:"sessionId"`
	TurnID    string `json:"turnId"`
	RunID     string `json:"runId"`
}

// PromptInput requests execution of one reserved parent run.
type PromptInput struct {
	RunID string `json:"runId"`
	Text  string `json:"text"`
}

// SessionInfo is the client-facing projection of persisted session metadata.
type SessionInfo struct {
	ID            string `json:"id"`
	CWD           string `json:"cwd"`
	Name          string `json:"name,omitempty"`
	Model         string `json:"model"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

// TranscriptMessage is one ordered persisted message projected for clients.
type TranscriptMessage struct {
	ID        string `json:"id"`
	TurnID    string `json:"turnId"`
	Sequence  int64  `json:"sequence"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	Thinking  string `json:"thinking,omitempty"`
	ToolName  string `json:"toolName,omitempty"`
	IsError   bool   `json:"isError,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// SessionSnapshot is an authoritative point-in-time session presentation.
type SessionSnapshot struct {
	Session       SessionInfo         `json:"session"`
	Messages      []TranscriptMessage `json:"messages"`
	ActiveRunID   string              `json:"activeRunId,omitempty"`
	ContextTokens int                 `json:"contextTokens,omitempty"`
	ContextWindow int                 `json:"contextWindow,omitempty"`
}

// RunStatus is a canonical parent-run terminal state on the wire.
type RunStatus string

const (
	RunStatusQueued      RunStatus = "queued"
	RunStatusRunning     RunStatus = "running"
	RunStatusCompleted   RunStatus = "completed"
	RunStatusFailed      RunStatus = "failed"
	RunStatusAborted     RunStatus = "aborted"
	RunStatusInterrupted RunStatus = "interrupted"
)

// ProviderErrorKind is the canonical recovery class for provider failures.
type ProviderErrorKind string

const (
	ProviderErrorAuthentication ProviderErrorKind = "authentication"
	ProviderErrorEntitlement    ProviderErrorKind = "entitlement"
	ProviderErrorUsageLimit     ProviderErrorKind = "usage_limit"
	ProviderErrorRateLimit      ProviderErrorKind = "rate_limit"
	ProviderErrorTransport      ProviderErrorKind = "transport"
	ProviderErrorProtocol       ProviderErrorKind = "protocol"
)

// RunInfo is the durable status of one parent-run generation.
type RunInfo struct {
	SessionID    string    `json:"sessionId"`
	TurnID       string    `json:"turnId"`
	RunID        string    `json:"runId"`
	Status       RunStatus `json:"status"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
}

// PromptOutcome is the terminal projection of one parent run.
type PromptOutcome struct {
	SessionID    string            `json:"sessionId"`
	TurnID       string            `json:"turnId"`
	RunID        string            `json:"runId"`
	Text         string            `json:"text"`
	StopReason   string            `json:"stopReason"`
	Status       RunStatus         `json:"status"`
	ErrorKind    ProviderErrorKind `json:"errorKind,omitempty"`
	ErrorMessage string            `json:"errorMessage,omitempty"`
}
