package subagent

import (
	"context"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

// ParentToolFactory constructs the asynchronous delegation tool for one owner.
// Child runtime bundles omit this factory, preventing nested delegation.
type ParentToolFactory interface {
	Tool(ownerSessionID string, catalog Catalog) (droids.AnyTool, error)
}

// Owner is the authoritative persisted parent configuration sampled at task admission.
type Owner struct {
	SessionID     string
	CWD           string
	Model         string
	ThinkingLevel string
	Persistent    bool
}

// OwnerResolver loads current parent configuration without loading its runtime.
type OwnerResolver interface {
	SubagentOwner(context.Context, string) (Owner, error)
}

// Admission snapshots the owner configuration and definition used when a
// conversation is created, and contains one immutable task message.
type Admission struct {
	OwnerSessionID     string
	ConversationID     ConversationID
	ExpectedGeneration uint64
	Definition         Definition
	CWD                string
	Model              string
	ThinkingLevel      string
	Message            string
	RetryOf            TaskID
	Now                time.Time
}

// Claim binds one queued task to a scheduler worker.
type Claim struct {
	Conversation Conversation
	Task         Task
}

// Completion is one generation-guarded terminal worker transition.
type Completion struct {
	TaskID                 TaskID
	ConversationID         ConversationID
	CancellationGeneration uint64
	State                  TaskState
	ChildTurnID            string
	ResultSummary          string
	Error                  string
	FinishedAt             time.Time
}

// Recovery summarizes the startup interruption pass.
type Recovery struct {
	InterruptedTasks []Task
	QueuedTasks      int
}

// Repository is the durable authority for child ownership, queueing,
// scheduling, lifecycle, and parent mailbox records.
type Repository interface {
	Admit(context.Context, Admission, Limits) (Conversation, Task, error)
	QueuedSessions(context.Context) ([]string, error)
	ClaimNext(context.Context, string, Limits, time.Time) (Claim, error)
	MarkConversationInitialized(context.Context, ConversationID, time.Time) error
	BindChildTurn(context.Context, TaskID, uint64, string) (Task, error)
	Complete(context.Context, Completion) (Task, *MailboxItem, error)
	Cancel(context.Context, TaskID, uint64, string, time.Time) (Task, error)
	Dismiss(context.Context, ConversationID, uint64, string, time.Time) ([]Task, error)
	Conversation(context.Context, ConversationID) (Conversation, error)
	ConversationByAgent(context.Context, string, string) (Conversation, error)
	Task(context.Context, TaskID) (Task, error)
	ListConversations(context.Context, string) ([]Conversation, error)
	ListDismissedConversations(context.Context, string) ([]Conversation, error)
	AllDismissedConversations(context.Context) ([]Conversation, error)
	ListTasks(context.Context, ConversationID) ([]Task, error)
	RecoverRunning(context.Context, time.Time) (Recovery, error)
	PendingMailbox(context.Context, string, int) ([]MailboxItem, error)
	PendingMailboxOwners(context.Context, string, int) ([]string, error)
	MarkMailboxDelivered(context.Context, []string, uint64, time.Time) error
}

// ChildRuntimeFactory constructs an isolated child over its own conversation store.
type ChildRuntimeFactory interface {
	Open(context.Context, Conversation) (ChildRuntime, error)
	Delete(context.Context, Conversation) error
}

// ChildRuntime executes one task in an isolated conversation. Run, Steer, and
// Transcript may be called concurrently; implementations must synchronize
// access to shared runtime and store state without blocking cancellation.
type ChildRuntime interface {
	Run(context.Context, Task, func(childTurnID string) error, func(LiveEvent)) (ChildOutcome, error)
	Steer(context.Context, string) error
	Abort(context.Context) error
	Transcript(context.Context) (Transcript, error)
	Close(context.Context) error
}

// ChildOutcome is the bounded terminal projection returned by a child runtime.
type ChildOutcome struct {
	TurnID string
	State  TaskState
	Text   string
	Error  string
}

// EventSink receives absolute lifecycle updates after authoritative commits.
type EventSink interface {
	// SubagentChanged runs synchronously on scheduling paths and must return
	// promptly without performing network, disk, or other blocking I/O.
	SubagentChanged(context.Context, string, ConversationID, TaskID)
	// MailboxAdded runs only after the child execution slot is released and may
	// perform bounded delivery work.
	MailboxAdded(context.Context, MailboxItem)
}
