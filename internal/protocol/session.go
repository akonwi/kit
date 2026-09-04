// Package protocol owns Kit's renderer-neutral session wire vocabulary.
package protocol

import (
	"encoding/json"
	"strings"
)

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

// TranscriptContentKind identifies one renderer-neutral message content block.
type TranscriptContentKind string

const (
	TranscriptContentText     TranscriptContentKind = "text"
	TranscriptContentThinking TranscriptContentKind = "thinking"
	TranscriptContentToolCall TranscriptContentKind = "toolCall"
	TranscriptContentImage    TranscriptContentKind = "image"
	TranscriptContentFile     TranscriptContentKind = "file"
)

// TranscriptContent preserves the ordered presentation content of a message.
type TranscriptContent struct {
	Kind               TranscriptContentKind `json:"kind"`
	Text               string                `json:"text,omitempty"`
	ToolCallID         string                `json:"toolCallId,omitempty"`
	ToolName           string                `json:"toolName,omitempty"`
	Arguments          string                `json:"arguments,omitempty"`
	ArgumentsTruncated bool                  `json:"argumentsTruncated,omitempty"`
	Filename           string                `json:"filename,omitempty"`
	MediaType          string                `json:"mediaType,omitempty"`
}

// TranscriptMessage is one ordered persisted message projected for clients.
type TranscriptMessage struct {
	ID           string              `json:"id"`
	TurnID       string              `json:"turnId"`
	Sequence     int64               `json:"sequence"`
	Role         string              `json:"role"`
	Content      []TranscriptContent `json:"content"`
	StopReason   string              `json:"stopReason,omitempty"`
	ErrorMessage string              `json:"errorMessage,omitempty"`
	ToolCallID   string              `json:"toolCallId,omitempty"`
	ToolName     string              `json:"toolName,omitempty"`
	Details      json.RawMessage     `json:"details,omitempty"`
	IsError      bool                `json:"isError,omitempty"`
	CreatedAt    string              `json:"createdAt"`
}

// TextContent joins the message's text blocks in their original order.
func (message TranscriptMessage) TextContent() string {
	parts := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		if block.Kind == TranscriptContentText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
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
