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

// RunStatus is a canonical parent-run terminal state on the wire.
type RunStatus string

const (
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
