// Package protocol owns Kit's renderer-neutral session wire vocabulary.
package protocol

import (
	"encoding/json"
	"strings"
)

// CreateSessionInput requests a new persisted or temporary session.
type CreateSessionInput struct {
	ID            string `json:"id,omitempty"`
	CWD           string `json:"cwd"`
	Name          string `json:"name,omitempty"`
	Model         string `json:"model"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	Temporary     bool   `json:"temporary,omitempty"`
}

// ForkSessionInput requests a linked child from a settled persistent session.
type ForkSessionInput struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// RenameSessionInput requests a new non-empty display name for a session.
type RenameSessionInput struct {
	Name string `json:"name"`
}

// ChangeCWDInput requests a new filesystem scope for a session. Relative paths
// resolve from the session's current cwd.
type ChangeCWDInput struct {
	MutationID string `json:"mutationId"`
	Path       string `json:"path"`
}

// ThinkingLevel is one canonical provider-neutral reasoning effort.
type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
	ThinkingMax     ThinkingLevel = "max"
)

// ModelInputKind identifies one supported model input modality.
type ModelInputKind string

const (
	ModelInputText  ModelInputKind = "text"
	ModelInputImage ModelInputKind = "image"
)

// ModelCapability is one selectable exact provider/model configuration.
type ModelCapability struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Provider        string           `json:"provider"`
	API             string           `json:"api"`
	ContextWindow   int              `json:"contextWindow"`
	MaxInputTokens  int              `json:"maxInputTokens,omitempty"`
	MaxOutputTokens int              `json:"maxOutputTokens,omitempty"`
	ThinkingLevels  []ThinkingLevel  `json:"thinkingLevels"`
	Inputs          []ModelInputKind `json:"inputs"`
	Available       bool             `json:"available"`
}

// ModelCatalog is the server-authoritative selectable model catalog.
type ModelCatalog struct {
	Models []ModelCapability `json:"models"`
}

// ConfigureSessionInput requests an atomic model/thinking transition.
type ConfigureSessionInput struct {
	ExpectedRevision uint64         `json:"expectedRevision"`
	Model            string         `json:"model"`
	ThinkingLevel    *ThinkingLevel `json:"thinkingLevel,omitempty"`
}

// ConfigureSessionResult reports the exact configuration and runtime stream applied.
type ConfigureSessionResult struct {
	Session       SessionInfo `json:"session"`
	EventStreamID string      `json:"eventStreamId"`
	Compacted     bool        `json:"compacted,omitempty"`
	CheckpointID  string      `json:"checkpointId,omitempty"`
	Warnings      []string    `json:"warnings,omitempty"`
}

// CompactSessionInput requests one idempotent explicit context compaction.
type CompactSessionInput struct {
	OperationID string `json:"operationId"`
}

// CompactSessionResult reports whether explicit compaction changed context.
type CompactSessionResult struct {
	OperationID   string `json:"operationId"`
	Compacted     bool   `json:"compacted"`
	CheckpointID  string `json:"checkpointId,omitempty"`
	EventStreamID string `json:"eventStreamId"`
}

// PromptSectionKind identifies one ordered source category in an assembled prompt.
type PromptSectionKind string

const (
	PromptSectionCore         PromptSectionKind = "core"
	PromptSectionFeature      PromptSectionKind = "feature"
	PromptSectionSkillCatalog PromptSectionKind = "skillCatalog"
	PromptSectionPlugin       PromptSectionKind = "plugin"
	PromptSectionContext      PromptSectionKind = "context"
)

// PromptSource identifies one source applied to a session prompt.
type PromptSource struct {
	SectionID string            `json:"sectionId"`
	ID        string            `json:"id"`
	Kind      PromptSectionKind `json:"kind"`
	Path      string            `json:"path,omitempty"`
}

// PromptDiagnostic reports non-fatal guidance omitted or altered during reload.
type PromptDiagnostic struct {
	Severity string       `json:"severity"`
	Code     string       `json:"code"`
	Message  string       `json:"message"`
	Source   PromptSource `json:"source"`
}

// ReloadSessionResult describes a newly applied prompt/tool bundle without
// exposing its server-local prompt text or implementation types.
type ReloadSessionResult struct {
	SessionID     string             `json:"sessionId"`
	EventStreamID string             `json:"eventStreamId"`
	Sources       []PromptSource     `json:"sources"`
	Diagnostics   []PromptDiagnostic `json:"diagnostics,omitempty"`
	Warnings      []string           `json:"warnings,omitempty"`
}

// RunReservation acknowledges a droid-owned turn admission.
type RunReservation struct {
	SessionID string `json:"sessionId"`
	TurnID    string `json:"turnId"`
	RunID     string `json:"runId"`
}

// PromptInput requests admission of one droid-owned turn.
type PromptInput struct {
	Text          string   `json:"text"`
	AttachmentIDs []string `json:"attachmentIds,omitempty"`
	AnnotationIDs []uint64 `json:"annotationIds,omitempty"`
}

// FollowUpQueue is the renderer-safe projection of deferred session prompts.
type FollowUpQueue struct {
	Count    int      `json:"count"`
	Previews []string `json:"previews,omitempty"`
	// AnnotationIDs identifies annotations already assigned to queued messages.
	AnnotationIDs []uint64 `json:"annotationIds,omitempty"`
}

// PromptSubmission reports whether a prompt started or became a follow-up.
type PromptSubmission struct {
	Reservation *RunReservation `json:"reservation,omitempty"`
	Queued      bool            `json:"queued"`
	Queue       FollowUpQueue   `json:"queue"`
}

// RestoreFollowUpsResult returns every atomically drained follow-up.
type RestoreFollowUpsResult struct {
	Messages []PromptInput `json:"messages"`
	Queue    FollowUpQueue `json:"queue"`
}

// PromoteFollowUpsResult reports steering accepted from the follow-up queue.
type PromoteFollowUpsResult struct {
	Promoted int           `json:"promoted"`
	Queue    FollowUpQueue `json:"queue"`
}

// PromptCommandInput requests execution of one discovered prompt template.
type PromptCommandInput struct {
	Name string `json:"name"`
	Args string `json:"args,omitempty"`
}

// PromptCommand is renderer-safe metadata for one discovered prompt template.
type PromptCommand struct {
	ArgumentHint string `json:"argumentHint,omitempty"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Source       string `json:"source"`
	Location     string `json:"location"`
}

// BashExecutionInput requests one runtime-idempotent direct composer shell
// execution. Included terminal executions remain protected by their durable
// droid boundary receipt.
type BashExecutionInput struct {
	ExecutionID        string `json:"executionId"`
	Command            string `json:"command"`
	ExcludeFromContext bool   `json:"excludeFromContext,omitempty"`
}

// BashExecutionStatus is the canonical lifecycle state of direct shell work.
type BashExecutionStatus string

const (
	BashExecutionRunning     BashExecutionStatus = "running"
	BashExecutionCompleted   BashExecutionStatus = "completed"
	BashExecutionFailed      BashExecutionStatus = "failed"
	BashExecutionAborted     BashExecutionStatus = "aborted"
	BashExecutionInterrupted BashExecutionStatus = "interrupted"
)

// BashExecution is one renderer-neutral direct shell execution projection.
type BashExecution struct {
	ID                 string              `json:"id"`
	SessionID          string              `json:"sessionId"`
	Sequence           int64               `json:"sequence"`
	Command            string              `json:"command"`
	Status             BashExecutionStatus `json:"status"`
	Output             string              `json:"output,omitempty"`
	ExitCode           *int                `json:"exitCode,omitempty"`
	ExcludeFromContext bool                `json:"excludeFromContext,omitempty"`
	Truncated          bool                `json:"truncated,omitempty"`
	TimedOut           bool                `json:"timedOut,omitempty"`
	ErrorMessage       string              `json:"errorMessage,omitempty"`
	StartedAt          string              `json:"startedAt"`
	CompletedAt        string              `json:"completedAt,omitempty"`
}

// SessionInfo is the client-facing projection of authoritative session metadata.
type SessionInfo struct {
	ID                    string `json:"id"`
	CWD                   string `json:"cwd"`
	Name                  string `json:"name,omitempty"`
	ParentSessionID       string `json:"parentSessionId,omitempty"`
	ParentSessionName     string `json:"parentSessionName,omitempty"`
	Temporary             bool   `json:"temporary,omitempty"`
	Model                 string `json:"model"`
	ThinkingLevel         string `json:"thinkingLevel"`
	ConfigurationRevision uint64 `json:"configurationRevision"`
	CreatedAt             string `json:"createdAt"`
	UpdatedAt             string `json:"updatedAt"`
}

// VCSHeadKind identifies the checked-out repository head shape.
type VCSHeadKind string

const (
	VCSHeadBranch   VCSHeadKind = "branch"
	VCSHeadDetached VCSHeadKind = "detached"
	VCSHeadUnborn   VCSHeadKind = "unborn"
)

// VCSHead is renderer-neutral checked-out head metadata.
type VCSHead struct {
	Kind VCSHeadKind `json:"kind"`
	Name string      `json:"name,omitempty"`
	OID  string      `json:"oid,omitempty"`
}

// GitHubPullRequest is optional, cached metadata for the current named branch.
type GitHubPullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// VCSStatus is volatile repository status for a session workspace.
type VCSStatus struct {
	PullRequest *GitHubPullRequest `json:"pullRequest,omitempty"`
	Root        string             `json:"root"`
	Head        VCSHead            `json:"head"`
	Dirty       bool               `json:"dirty"`
}

// SessionVCSStatus binds volatile VCS status to the session and cwd that were
// inspected. Status is nil outside a usable Git worktree.
type SessionVCSStatus struct {
	SessionID string     `json:"sessionId"`
	CWD       string     `json:"cwd"`
	Status    *VCSStatus `json:"status,omitempty"`
}

// TranscriptContentKind identifies one renderer-neutral message content block.
type TranscriptContentKind string

const (
	TranscriptContentText        TranscriptContentKind = "text"
	TranscriptContentThinking    TranscriptContentKind = "thinking"
	TranscriptContentToolCall    TranscriptContentKind = "toolCall"
	TranscriptContentImage       TranscriptContentKind = "image"
	TranscriptContentFile        TranscriptContentKind = "file"
	TranscriptContentAnnotations TranscriptContentKind = "annotations"
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
	AttachmentID       string                `json:"attachmentId,omitempty"`
	Annotations        []SubmittedAnnotation `json:"annotations,omitempty"`
}

// TranscriptMessage is one ordered persisted message projected for clients.
type TranscriptMessage struct {
	ID             string              `json:"id"`
	TurnID         string              `json:"turnId"`
	Sequence       int64               `json:"sequence"`
	Role           string              `json:"role"`
	Content        []TranscriptContent `json:"content"`
	Bash           *BashExecution      `json:"bash,omitempty"`
	StopReason     string              `json:"stopReason,omitempty"`
	ErrorMessage   string              `json:"errorMessage,omitempty"`
	ToolCallID     string              `json:"toolCallId,omitempty"`
	ToolName       string              `json:"toolName,omitempty"`
	BoundaryID     string              `json:"boundaryId,omitempty"`
	BoundaryKind   string              `json:"boundaryKind,omitempty"`
	BoundarySource string              `json:"boundarySource,omitempty"`
	Details        json.RawMessage     `json:"details,omitempty"`
	IsError        bool                `json:"isError,omitempty"`
	CreatedAt      string              `json:"createdAt"`
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

// PendingBoundary is durable external context awaiting materialization.
type PendingBoundary struct {
	ID         string              `json:"id"`
	Kind       string              `json:"kind"`
	Source     string              `json:"source,omitempty"`
	Content    []TranscriptContent `json:"content"`
	Details    json.RawMessage     `json:"details,omitempty"`
	AcceptedAt string              `json:"acceptedAt"`
}

// SessionUsage is the authoritative cumulative provider usage for one session.
type SessionUsage struct {
	Input       int              `json:"input"`
	Output      int              `json:"output"`
	CacheRead   int              `json:"cacheRead"`
	CacheWrite  int              `json:"cacheWrite"`
	Reasoning   int              `json:"reasoning"`
	TotalTokens int              `json:"totalTokens"`
	Cost        SessionUsageCost `json:"cost"`
}

// SessionUsageCost is cumulative provider cost in US dollars.
type SessionUsageCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// SubagentSource is renderer-safe definition provenance.
type SubagentSource struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	PluginID string `json:"pluginId,omitempty"`
}

// SubagentDefinition is model- and renderer-safe catalog metadata.
type SubagentDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Model       string         `json:"model,omitempty"`
	Source      SubagentSource `json:"source"`
}

// SubagentDiagnostic reports a non-fatal definition discovery problem.
type SubagentDiagnostic struct {
	Severity string         `json:"severity"`
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Source   SubagentSource `json:"source"`
}

// SubagentTask is one durable renderer-safe task projection.
type SubagentTask struct {
	ID                     string `json:"id"`
	Sequence               uint64 `json:"sequence"`
	State                  string `json:"state"`
	CancellationGeneration uint64 `json:"cancellationGeneration"`
	QueuedAt               string `json:"queuedAt"`
	StartedAt              string `json:"startedAt,omitempty"`
	FinishedAt             string `json:"finishedAt,omitempty"`
	ResultSummary          string `json:"resultSummary,omitempty"`
	Error                  string `json:"error,omitempty"`
}

// SubagentConversation is one retained roster entry with bounded recent tasks.
type SubagentConversation struct {
	ID                  string         `json:"id"`
	AgentName           string         `json:"agentName"`
	Model               string         `json:"model"`
	ThinkingLevel       string         `json:"thinkingLevel"`
	State               string         `json:"state"`
	Generation          uint64         `json:"generation"`
	ActiveTaskID        string         `json:"activeTaskId,omitempty"`
	QueuedTasks         int            `json:"queuedTasks"`
	LastCompletedTaskID string         `json:"lastCompletedTaskId,omitempty"`
	LastResultSummary   string         `json:"lastResultSummary,omitempty"`
	UpdatedAt           string         `json:"updatedAt"`
	Tasks               []SubagentTask `json:"tasks,omitempty"`
}

// SubagentMailboxItem is one pending bounded parent notification.
type SubagentMailboxItem struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationId"`
	TaskID         string `json:"taskId"`
	AgentName      string `json:"agentName"`
	State          string `json:"state"`
	Summary        string `json:"summary,omitempty"`
	Error          string `json:"error,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

// ProviderRetry is an authoritative provider retry delay.
type ProviderRetry struct {
	Count   int    `json:"count"`
	RetryAt string `json:"retryAt,omitempty"`
}

// ActiveCompaction is an authoritative automatic compaction in progress.
type ActiveCompaction struct {
	ID    string `json:"id"`
	RunID string `json:"runId"`
}

// SessionSnapshot is an authoritative point-in-time session presentation.
type SessionSnapshot struct {
	Session               SessionInfo            `json:"session"`
	Workspace             *WorkspaceRef          `json:"workspace,omitempty"`
	Messages              []TranscriptMessage    `json:"messages"`
	PreviousMessageCursor string                 `json:"previousMessageCursor,omitempty"`
	HasMoreMessages       bool                   `json:"hasMoreMessages,omitempty"`
	PendingBoundaries     []PendingBoundary      `json:"pendingBoundaries,omitempty"`
	ActiveRunID           string                 `json:"activeRunId,omitempty"`
	ProviderRetry         *ProviderRetry         `json:"providerRetry,omitempty"`
	ActiveCompaction      *ActiveCompaction      `json:"activeCompaction,omitempty"`
	ActiveBashExecutionID string                 `json:"activeBashExecutionId,omitempty"`
	EventStreamID         string                 `json:"eventStreamId,omitempty"`
	EventCursor           int64                  `json:"eventCursor,omitempty"`
	EventReplayFrom       int64                  `json:"eventReplayFrom,omitempty"`
	EventReplayAvailable  bool                   `json:"eventReplayAvailable,omitempty"`
	ContextTokens         int                    `json:"contextTokens,omitempty"`
	ContextWindow         int                    `json:"contextWindow,omitempty"`
	Usage                 SessionUsage           `json:"usage"`
	PluginFooter          *PluginFooter          `json:"pluginFooter,omitempty"`
	PluginCommands        []PluginCommand        `json:"pluginCommands,omitempty"`
	PromptCommands        []PromptCommand        `json:"promptCommands,omitempty"`
	FollowUps             FollowUpQueue          `json:"followUps"`
	Warnings              []string               `json:"warnings,omitempty"`
	SubagentDefinitions   []SubagentDefinition   `json:"subagentDefinitions,omitempty"`
	SubagentDiagnostics   []SubagentDiagnostic   `json:"subagentDiagnostics,omitempty"`
	SubagentConversations []SubagentConversation `json:"subagentConversations,omitempty"`
	SubagentMailbox       []SubagentMailboxItem  `json:"subagentMailbox,omitempty"`
	PendingInteractions   []InteractionRequest   `json:"pendingInteractions,omitempty"`
	Annotations           []AnnotationSummary    `json:"annotations,omitempty"`
	Scratchpad            *Scratchpad            `json:"scratchpad,omitempty"`
}

// TranscriptPage is one complete-turn-bounded page preceding a cursor.
type TranscriptPage struct {
	SessionID             string              `json:"sessionId"`
	Messages              []TranscriptMessage `json:"messages"`
	PreviousMessageCursor string              `json:"previousMessageCursor,omitempty"`
	HasMoreMessages       bool                `json:"hasMoreMessages,omitempty"`
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

// RunInfo is the canonical status of one droid turn.
type RunInfo struct {
	SessionID    string    `json:"sessionId"`
	TurnID       string    `json:"turnId"`
	RunID        string    `json:"runId"`
	Status       RunStatus `json:"status"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
}

// PromptOutcome is the terminal projection of one droid turn.
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
