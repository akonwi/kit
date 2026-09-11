// Package subagent owns Kit's definitions and supervised child-execution domain.
package subagent

import (
	"encoding/json"
	"time"
)

// ConversationID is the stable identity of one parent's conversation with an agent.
type ConversationID string

// TaskID is the stable identity of one submitted unit of child work.
type TaskID string

// SourceKind identifies the owner of an agent definition.
type SourceKind string

const (
	SourceUser    SourceKind = "user"
	SourceProject SourceKind = "project"
	SourcePlugin  SourceKind = "plugin"
)

// Source describes where an agent definition originated. PluginID is reserved
// for plugin-contributed definitions in a later delivery.
type Source struct {
	Kind     SourceKind
	Path     string
	PluginID string
}

// Definition is one immutable child-agent configuration.
type Definition struct {
	Name         string
	Description  string
	Model        string
	Instructions string
	Source       Source
}

// DiagnosticSeverity classifies a non-fatal discovery condition.
type DiagnosticSeverity string

const (
	DiagnosticWarning DiagnosticSeverity = "warning"
)

// Diagnostic describes one malformed, duplicate, or unreadable definition.
type Diagnostic struct {
	Severity DiagnosticSeverity
	Code     string
	Message  string
	Source   Source
}

// ConversationState is the lifecycle state of one non-dismissed conversation.
type ConversationState string

const (
	ConversationIdle        ConversationState = "idle"
	ConversationRunning     ConversationState = "running"
	ConversationFailed      ConversationState = "failed"
	ConversationAborted     ConversationState = "aborted"
	ConversationInterrupted ConversationState = "interrupted"
)

// TaskState is the durable lifecycle state of one submitted task.
type TaskState string

const (
	TaskQueued      TaskState = "queued"
	TaskRunning     TaskState = "running"
	TaskCompleted   TaskState = "completed"
	TaskFailed      TaskState = "failed"
	TaskAborted     TaskState = "aborted"
	TaskInterrupted TaskState = "interrupted"
)

// Conversation is the Kit-owned durable projection of a child conversation.
type Conversation struct {
	ID                  ConversationID
	OwnerSessionID      string
	Agent               Definition
	CWD                 string
	Model               string
	ThinkingLevel       string
	State               ConversationState
	Generation          uint64
	ActiveTaskID        TaskID
	QueuedTasks         int
	LastCompletedTaskID TaskID
	LastResultSummary   string
	DroidInitializedAt  *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DismissedAt         *time.Time
}

// Task is the Kit-owned durable projection of one child unit of work.
type Task struct {
	ID                     TaskID
	ConversationID         ConversationID
	OwnerSessionID         string
	Sequence               uint64
	Message                string
	State                  TaskState
	RetryOf                TaskID
	ChildTurnID            string
	CancellationGeneration uint64
	QueuedAt               time.Time
	StartedAt              *time.Time
	FinishedAt             *time.Time
	ResultSummary          string
	Error                  string
}

// LiveEvent is one bounded transient or durable child activity update.
type LiveEvent struct {
	Sequence     int64
	Kind         string
	TurnID       string
	MessageID    string
	ContentIndex int
	Delta        string
	Text         string
	ToolCallID   string
	ToolName     string
	IsError      bool
}

// LiveEventPage is a runtime-local child stream page.
type LiveEventPage struct {
	StreamID       string
	FirstSequence  int64
	LastSequence   int64
	ResyncRequired bool
	Events         []LiveEvent
}

// TranscriptContent is one ordered renderer-neutral child message block.
type TranscriptContent struct {
	Kind               string
	Text               string
	ToolCallID         string
	ToolName           string
	Arguments          string
	ArgumentsTruncated bool
	Filename           string
	MediaType          string
}

// TranscriptMessage is one durable child conversation message.
type TranscriptMessage struct {
	ID             string
	TurnID         string
	Sequence       int64
	Role           string
	Content        []TranscriptContent
	StopReason     string
	ErrorMessage   string
	ToolCallID     string
	ToolName       string
	BoundaryID     string
	BoundaryKind   string
	BoundarySource string
	Details        json.RawMessage
	IsError        bool
	CreatedAt      time.Time
}

// Transcript is one bounded/paginated child transcript snapshot. The first
// delivery returns all current messages within droids' bounded page contract.
type Transcript struct {
	ConversationID ConversationID
	Messages       []TranscriptMessage
}

// MailboxItem is the bounded result delivered to a parent at a safe boundary.
type MailboxItem struct {
	ID             string
	OwnerSessionID string
	ConversationID ConversationID
	TaskID         TaskID
	AgentName      string
	State          TaskState
	Summary        string
	Error          string
	CreatedAt      time.Time
	DeliveredAt    *time.Time
	Generation     uint64
}
