package droids

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Input is role-safe caller input.
type Input struct {
	Content []InputContent
}

// InputContent is content accepted from callers.
type InputContent interface{ isInputContent() }

// TextInput is caller-provided text.
type TextInput struct {
	Text         string
	AttachmentID string
	Filename     string
	MediaType    string
}

func (TextInput) isInputContent() {}

// FileInput is caller-provided file or image content. Providers select their
// representation from MediaType.
type FileInput struct {
	Filename     string
	MediaType    string
	URL          string
	AttachmentID string
}

func (FileInput) isInputContent() {}

// NewFileInputURL validates a caller-provided file or image URL.
func NewFileInputURL(filename, mediaType, rawURL string) (FileInput, error) {
	parsed, err := validateMediaType(mediaType)
	if err != nil {
		return FileInput{}, err
	}
	if filename == "" && !strings.HasPrefix(strings.ToLower(parsed), "image/") {
		return FileInput{}, fmt.Errorf("filename is required for non-image input")
	}
	if err := validateContentSource(rawURL, mediaType); err != nil {
		return FileInput{}, err
	}
	return FileInput{Filename: filename, MediaType: mediaType, URL: rawURL}, nil
}

// NewFileInputData constructs caller-provided inline file or image data.
func NewFileInputData(filename, mediaType string, data []byte) FileInput {
	return FileInput{Filename: filename, MediaType: mediaType, URL: dataURL(mediaType, data)}
}

// MessageEnvelope gives a canonical message durable identity and ordering
// metadata.
type MessageEnvelope struct {
	ID             MessageID
	Sequence       uint64
	ConversationID ConversationID
	TurnID         TurnID
	CreatedAt      time.Time
	Message        Message
}

// BoundaryMessage carries non-user agent-domain information into context.
type BoundaryMessage struct {
	// ID makes this materialized boundary identifiable when non-empty.
	ID string
	// ReceiptIDs atomically acknowledge source records represented by this
	// boundary. When empty, a non-empty ID is also its sole receipt.
	ReceiptIDs []string
	Kind       string
	Source     string
	Content    []InputContent
	Details    json.RawMessage
}

// BoundaryStatus reports durable receipt and turn-consumption state.
type BoundaryStatus struct {
	Received bool
	Pending  bool
	TurnID   TurnID
}

// PromptOptions controls prompt admission behavior.
type PromptOptions struct {
	// Steer requires an active turn and durably adds the input at its next model
	// boundary. It returns ErrConflict rather than starting a new turn when idle.
	Steer bool
	// AdmissionKey makes immediate prompt admission idempotent for an embedding
	// application crossing its own durable boundary. Reuse with different input
	// returns ErrConflict. Steering does not accept admission keys.
	AdmissionKey string
}

// RetryPolicy configures droid-owned provider retries.
type RetryPolicy struct {
	Enabled          bool
	MaxRetries       int
	BaseDelay        time.Duration
	MaxDelay         time.Duration
	UseProviderDelay bool
}

// DefaultRetryPolicy returns the SDK retry defaults.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		Enabled: true, MaxRetries: 3, BaseDelay: 2 * time.Second,
		MaxDelay: 60 * time.Second, UseProviderDelay: true,
	}
}

// ExecutionBudget optionally bounds model cycles in one attempt. Zero is
// unbounded.
type ExecutionBudget struct {
	MaxModelCycles uint64
}

// ExecutionPolicy configures local execution bounds.
type ExecutionPolicy struct {
	Budget           ExecutionBudget
	ToolExecution    ExecutionMode
	MaxParallelTools int
}

// CompactionConfig contains the only public automatic-compaction overrides.
type CompactionConfig struct {
	Prompt string
	Model  string
}

// ToolContext identifies the durable call being processed.
type ToolContext struct {
	ConversationID ConversationID
	TurnID         TurnID
	AttemptID      AttemptID
	ToolCallID     ToolCallID
}

// BeforeToolCallHook runs from a durable pre-execution hook phase.
type BeforeToolCallHook func(context.Context, ToolContext, ToolCall) (BeforeToolResult, error)

// AfterToolCallHook runs after the raw tool result is durable.
type AfterToolCallHook func(context.Context, ToolContext, ToolResult) (*ToolResult, error)

// Config configures one autonomous droid.
type Config struct {
	Store        Store
	Providers    Providers
	Model        string
	SystemPrompt string
	Reasoning    string
	Tools        []AnyTool

	Retry      *RetryPolicy
	Execution  *ExecutionPolicy
	Compaction CompactionConfig

	BeforeToolCall BeforeToolCallHook
	AfterToolCall  AfterToolCallHook
}

// ForkOptions configures the independent Store used by a forked conversation.
type ForkOptions struct {
	Store Store
}

// ForkPoint identifies the exact settled source revision inherited by a fork.
type ForkPoint struct {
	ConversationID ConversationID `json:"conversation_id"`
	Revision       uint64         `json:"revision"`
	LastEvent      EventSequence  `json:"last_event"`
}

// ForkResult contains a newly attached child droid and its durable lineage.
type ForkResult struct {
	Droid *Droid
	Point ForkPoint
}

// ExecutionHandle observes one admitted execution without exposing its internal
// identity.
type ExecutionHandle interface {
	TurnID() TurnID
	Wait(context.Context) (Outcome, error)
	Snapshot(context.Context) (ExecutionSnapshot, error)
}

// ExecutionStatus describes current or terminal execution state.
type ExecutionStatus string

const (
	ExecutionReady       ExecutionStatus = "ready"
	ExecutionRunning     ExecutionStatus = "running"
	ExecutionRetrying    ExecutionStatus = "retrying"
	ExecutionPausing     ExecutionStatus = "pausing"
	ExecutionPaused      ExecutionStatus = "paused"
	ExecutionAborting    ExecutionStatus = "aborting"
	ExecutionCompleted   ExecutionStatus = "completed"
	ExecutionFailed      ExecutionStatus = "failed"
	ExecutionAborted     ExecutionStatus = "aborted"
	ExecutionInterrupted ExecutionStatus = "interrupted"
)

// DroidErrorKind classifies stable runtime outcomes.
type DroidErrorKind string

const (
	DroidErrorProvider    DroidErrorKind = "provider"
	DroidErrorTool        DroidErrorKind = "tool"
	DroidErrorPersistence DroidErrorKind = "persistence"
	DroidErrorCompaction  DroidErrorKind = "compaction"
	DroidErrorUnsafe      DroidErrorKind = "unsafe_continuation"
	DroidErrorLimit       DroidErrorKind = "limit"
	DroidErrorInternal    DroidErrorKind = "internal"
)

// DroidError is safe, bounded terminal diagnostic information.
type DroidError struct {
	Kind      DroidErrorKind
	Message   string
	Retryable bool
	Cause     error `json:"-"`
}

// ProviderRetry is the authoritative retry delay for an execution.
type ProviderRetry struct {
	Count   int
	RetryAt time.Time
}

// CompactionSnapshot is one active automatic context compaction.
type CompactionSnapshot struct {
	ID     string
	TurnID TurnID
}

// ExecutionSnapshot is the current execution projection.
type ExecutionSnapshot struct {
	TurnID           TurnID
	Status           ExecutionStatus
	BoundaryReaction bool
	Reason           string
	Error            *DroidError
	Retry            *ProviderRetry
	Compaction       *CompactionSnapshot
}

// Outcome is one execution's terminal or paused result.
type Outcome struct {
	ConversationID ConversationID
	TurnID         TurnID
	Status         ExecutionStatus
	FinalMessage   *MessageEnvelope
	Error          *DroidError
	CheckpointID   CheckpointID
	Usage          Usage
}

// QuiescentKind distinguishes ready, paused, and recoverable droids.
type QuiescentKind string

const (
	QuiescentSettled     QuiescentKind = "settled"
	QuiescentPaused      QuiescentKind = "paused"
	QuiescentRecoverable QuiescentKind = "recoverable"
)

// QuiescentState describes an atomic non-running droid state.
type QuiescentState struct {
	Kind      QuiescentKind
	TurnID    TurnID
	Execution *ExecutionSnapshot
	LastEvent EventSequence
}

// ConversationSnapshot is the conversation-level snapshot header.
type ConversationSnapshot struct {
	ID         ConversationID
	Revision   uint64
	ForkedFrom *ForkPoint
}

// PendingBoundarySnapshot is one durable external boundary awaiting a safe
// model-context boundary.
type PendingBoundarySnapshot struct {
	Message    BoundaryMessage
	AcceptedAt time.Time
}

// PendingInputSnapshot reports durable pending steering and boundary state.
type PendingInputSnapshot struct {
	Steering   int
	Boundary   int
	Boundaries []PendingBoundarySnapshot
}

// ContextSnapshot reports the active provider context checkpoint.
type ContextSnapshot struct {
	CheckpointID CheckpointID
	Messages     int
	Usage        ContextUsage
}

// ContextTarget identifies a model configuration against which active context
// should be measured or compacted.
type ContextTarget struct {
	Model     string
	Reasoning string
}

// ContextAssessment reports whether settled active context is replayable and
// fits one target model configuration.
type ContextAssessment struct {
	Target             ContextTarget
	Usage              ContextUsage
	ReplayCompatible   bool
	RequiresCompaction bool
}

// CompactContextOptions configures one idempotent, quiescent context
// adaptation. OperationID, Target, and Force must remain stable across
// ambiguous retries.
type CompactContextOptions struct {
	OperationID string
	Target      ContextTarget
	// Force requests compaction even when context is replayable and below the
	// automatic compaction threshold. Empty context remains a no-op.
	Force bool
}

// CompactContextResult describes the durable outcome of one context-adaptation
// operation.
type CompactContextResult struct {
	OperationID  string
	Target       ContextTarget
	Forced       bool
	Compacted    bool
	CheckpointID CheckpointID
	Before       ContextUsage
	After        ContextUsage
}

// SnapshotOptions bounds recent diagnostic history.
type SnapshotOptions struct {
	RecentMessageLimit int
}

// Snapshot is a bounded, atomic droid projection.
type Snapshot struct {
	Conversation ConversationSnapshot
	Recent       MessagePage
	Active       *ExecutionSnapshot
	Pending      PendingInputSnapshot
	Context      ContextSnapshot
	Usage        SessionUsage
	LastEvent    EventSequence
}

// TurnSnapshot is the durable terminal state of one settled turn.
type TurnSnapshot struct {
	ID     TurnID
	Status ExecutionStatus
	Error  *DroidError
	Usage  Usage
}

// HistoryQuery pages canonical messages.
type HistoryQuery struct {
	After      uint64
	Before     uint64
	Limit      int
	Descending bool
}

// MessagePage is one page of canonical diagnostic messages.
type MessagePage struct {
	Messages []MessageEnvelope
	Next     uint64
	HasMore  bool
}

// SubscribeOptions configures durable replay and transient delivery.
type SubscribeOptions struct {
	After            EventSequence
	IncludeTransient bool
	Buffer           int
}

// EventEnvelope anchors one event to durable droid identities.
type EventEnvelope struct {
	Sequence       EventSequence
	Durable        bool
	OccurredAt     time.Time
	ConversationID ConversationID
	TurnID         TurnID
	AttemptID      AttemptID
	Event          Event
}

// LifecycleEvent is the generic, versioned event payload used by the core
// state machine. Provider stream events retain their existing concrete types.
type LifecycleEvent struct {
	Kind string
	Data json.RawMessage
}

func (LifecycleEvent) isEvent() {}

// Subscription is a bounded droid event stream.
type Subscription interface {
	Events() <-chan EventEnvelope
	Err() error
	Close()
}
