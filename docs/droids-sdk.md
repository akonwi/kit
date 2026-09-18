# Droids API and SDK specification

## Status

Accepted

## Purpose

This document specifies the public Go API and behavioral contracts for droids.
It implements the architecture established by
[ADR 0004](./adrs/0004-droids-agent-runtime-boundary.md).

A droid is an autonomous agent unit. An application configures it, gives it
instructions, supervises it, and observes it. The droid owns its conversation
state, turns, execution, tools, steering, retries, compaction, persistence
coordination, and settlement.

The API is application-neutral. Kit is one consumer.

## Initial scope

The initial SDK includes:

- the core `droids` package;
- provider-neutral messages and content;
- provider and model interfaces;
- typed tools;
- autonomous prompt admission, context-only boundary reaction, steering, retry, compaction, abort, and settled-conversation fork behavior;
- durable state and event contracts;
- an in-memory Store;
- a CGO-free SQLite Store;
- Store conformance tests reusable by adapter implementations.

Only these Store adapters are built initially:

1. **In-memory** — ephemeral use, unit tests, and simple embedding.
2. **SQLite** — durable local use and Kit's daemon.

Postgres, file, Redis, and remote Store adapters are not part of the initial
implementation. The Store interface remains suitable for external adapters
without requiring changes to the droid state machine.

## Package layout

The target package shape is:

```text
droids
  autonomous runtime, domain types, providers, tools, MemoryStore

droids/sqlitestore
  CGO-free SQLite Store implementation and migrations

droids/droidstest
  reusable Store and provider conformance suites
```

While droids remains private to Kit, these packages live below
`internal/droids`. Their APIs must not depend on Kit packages so they can be
extracted later without semantic redesign.

The root package never imports `sqlitestore`. Applications choose and construct
the adapter at composition time.

## Design principles

### Autonomous after admission

Once `Prompt` acknowledges admission, the droid owns execution through a
terminal or paused state. The caller does not invoke individual model cycles,
tool batches, retries, compaction, persistence writes, or settlement steps.

### Explicit prompt behavior

Every input enters through `Prompt`. Its mode declares whether an occupied
droid rejects the input or steers the current turn. Resume and abort remain
separate lifecycle operations. Callers own queues for future turns.

### Durable acknowledgement

An operation that returns an accepted receipt has crossed the configured Store
boundary. A persistent Store makes that receipt crash-durable. The in-memory
Store provides the same transition semantics for its process lifetime.

### One state machine

The Store records droid state; it does not implement agent policy. Embedding
applications and renderers project droid state; they do not reconstruct or
advance it independently.

### Safe side-effect boundaries

A tool call must be durably admitted before hooks or execution begin. The Store
uses its stable ID and conversation revision to prevent duplicate admission.
Pending hooks are restartable. A call becomes an ambiguous side-effect boundary
only after tool execution may have begun and before its raw result is durable.

### Unbounded productive progress

Model cycles and tool calls have no cumulative default limit. Configured
concurrency limits and bounded pending steering and boundary messages constrain
simultaneous work and retained memory.

### Automatic context management

Compaction is built in and automatic. Configuration may override only the
compaction prompt and model selection.

## Core construction API

### Opening a conversation

```go
func Open(
    ctx context.Context,
    id ConversationID,
    config Config,
) (*Droid, error)
```

`Open` atomically finds or creates `id`. It loads an existing conversation or
creates one when no conversation has that ID. The ID is required and is
normally allocated by the embedding application.

It returns only after the conversation state is acknowledged by the Store.
When loading existing state, `Open` validates it, reconstructs active context,
and durably classifies unfinished work before returning:

- a running, retrying, or aborting execution becomes interrupted and
  recoverable;
- an intentionally paused execution remains paused;
- a settled conversation remains ready.

`Open` does not resume model or tool work automatically. A caller may invoke
`Resume` unconditionally after opening; it continues resumable work and is a
no-op when no continuation is needed. `Snapshot` and `WaitQuiescent` remain
available when the caller wants to inspect the state before deciding.

The embedding application must not open multiple live droids with the same
`ConversationID`. Droids does not provide distributed ownership or process
coordination for this.

### Forking a settled conversation

```go
type ForkOptions struct {
    Store Store
}

type ForkPoint struct {
    ConversationID ConversationID
    Revision       uint64
    LastEvent      EventSequence
}

type ForkResult struct {
    Droid *Droid
    Point ForkPoint
}

func (d *Droid) Fork(
    ctx context.Context,
    id ConversationID,
    options ForkOptions,
) (ForkResult, error)
```

`Fork` creates an independent ready conversation from a settled source. It
copies immutable record history, active provider context, checkpoint identity,
boundary receipts, pending idle boundaries, and the cumulative usage total at
the exact fork point into a new destination Store. Later parent and child usage
advance independently from that inherited total.
Running, paused, interrupted, retrying, pausing, and aborting sources return
`ErrBusy`; forking never duplicates active execution or ambiguous tool state.
Fork does not invoke provider replay validation. The ordinary request path
validates inherited context before the child's next provider request and reports
any incompatibility through that prompt's normal outcome.

Only the `ConversationID` changes. Ancestral turn, attempt, message, tool-call,
and checkpoint IDs remain stable and are scoped by conversation. Embedded
conversation fields are rewritten through droids-owned versioned codecs, while
provider IDs and signatures remain unchanged. New work in each branch receives
fresh IDs.

The child has a fresh Store revision and outbox containing
`conversation.created` and `conversation.forked`; source outbox events are not
copied. Its bounded snapshot exposes the immediate source `ForkPoint`. An
already initialized destination is detected without attaching another live
droid. See [ADR 0005](./adrs/0005-droids-semantic-forking.md) for the complete
identity, lineage, atomicity, and recovery contract.

### Configuration

```go
type Config struct {
    Store        Store
    Model        Model // resolved, provider-bound active model
    SystemPrompt string
    Reasoning    string
    Tools        []AnyTool

    Retry      *RetryPolicy
    Execution *ExecutionPolicy
    Compaction CompactionConfig

    BeforeToolCall BeforeToolCallHook
    AfterToolCall  AfterToolCallHook
}
```

Required fields:

- `Model`

Callers resolve active and alternate models before passing them to Droids:

```go
model, err := providers.Resolve("openai-codex/gpt-5.6-sol")
if err != nil {
    return err
}
model = model.WithContextWindow(1_000_000)
```

Resolved models retain an immutable provider/catalog snapshot. Provider
compatibility is authoritative at request time; `WithContextWindow` does not
compare an override with catalog limits.

Defaults:

- `Store`: a new `MemoryStore`
- `Retry == nil`: `DefaultRetryPolicy()`
- `Execution == nil`: unbounded model cycles, parallel tools, four simultaneous
  tools
- `Compaction`: built-in prompt and active conversation model

Pointer policies distinguish omission from an intentional zero value such as
`RetryPolicy{Enabled: false}`. Configuration is copied during construction.
Callers must use explicit droid methods to make supported runtime changes.

The provider-facing prompt, reasoning level, and tools may be replaced while a
droid is active:

```go
type RequestConfiguration struct {
    SystemPrompt string
    Reasoning    string
    Tools        []AnyTool
}

func (d *Droid) Reconfigure(config RequestConfiguration) error
```

`Reconfigure` validates one complete immutable replacement before publishing it.
A provider request already in flight keeps the prompt, reasoning level, and tool
schemas it captured. The next provider request samples the replacement. Tool
calls returned by the earlier request are admitted and executed against the
configuration current when that phase begins; removing or changing a tool may
therefore produce the ordinary missing-tool result or invoke its replacement.
Model, provider, retry, execution, compaction, and hook configuration remain
fixed for the lifetime of the droid.

### Lifecycle

```go
func (d *Droid) Shutdown(ctx context.Context) error
func (d *Droid) Close() error
```

`Shutdown` rejects new operations and requests cancellation of active
provider/tool work. It completes only after all workers quiesce and the
resulting interruption or terminal state is durable. If `ctx` expires,
`Shutdown` returns its error while the droid continues safe cleanup; it does not
advertise settlement around a running tool.

`Close` performs the same shutdown without a deadline and returns persistence
failures. Both operations are idempotent after successful closure.

A dropped client, canceled wait, or timed-out shutdown waiter does not
implicitly settle an admitted execution.

## Identity types

The embedding application supplies the stable `ConversationID`; droids
generates the remaining opaque identifiers:

```go
type ConversationID string
type TurnID         string
type AttemptID      string
type MessageID      string
type ToolCallID     string
type CheckpointID   string
type EventSequence  uint64
```

Identifiers are unique within their semantic scope and remain stable across
process restart and Store reload. A fork gives the child a new conversation ID
while preserving ancestral nested IDs; callers therefore qualify turn, attempt,
message, tool-call, and checkpoint IDs by conversation. Callers treat textual
encodings as opaque.

## Message and content API

### Input versus stored messages

Callers submit role-safe `Input`. Input cannot contain thinking, tool calls, or
provider signatures.

```go
type Input struct {
    Content []InputContent
}

type InputContent interface { /* sealed by droids */ }

type TextInput struct {
    Text string
}

type FileInput struct {
    Filename  string
    MediaType string
    URL       string
}
```

Droids assigns message and turn identities when input is admitted.
Persisted and emitted messages share a common envelope:

```go
type MessageEnvelope struct {
    ID             MessageID
    ConversationID ConversationID
    TurnID         TurnID
    CreatedAt      time.Time
    Message        Message
}
```

### Canonical messages

Message content is constrained by role:

```go
type Message interface { /* sealed by droids */ }

type UserMessage struct {
    Content []InputContent
}

type AssistantMessage struct {
    Content       []AssistantContent
    Provider      string
    Model         string
    ResponseModel string
    ResponseID    string
    ProviderScope string
    Usage         Usage
    StopReason    StopReason
    Error         *ProviderError
}

type ToolResultMessage struct {
    ToolCallID ToolCallID
    ToolName   string
    Content    []ResultContent
    Details    json.RawMessage
    IsError    bool
    Terminate  bool
}

type ContextMessage struct {
    BoundaryID string // empty for built-in summaries
    Kind       string // summary or an external boundary kind
    Source     string
    Content    []InputContent
    Details    json.RawMessage
}
```

### Provider usage

```go
type Usage struct {
    Input       int
    Output      int
    CacheRead   int
    CacheWrite  int
    Reasoning   int
    TotalTokens int
    Cost        UsageCost
}

type SessionUsage = Usage
```

`Usage` on an assistant message or settled turn describes that canonical unit.
`SessionUsage` is the authoritative cumulative total for the conversation. It
includes every observed canonical provider terminal response, including retry,
provider-error, context-overflow, and provider-aborted responses that report
usage, plus automatic and explicit compaction summary requests. A local
cancellation that ends before a provider terminal response is observed has no
verified usage to add.

Each non-zero contribution has a stable immutable record and is added to the
bounded cumulative aggregate in the same Store Commit. Once a canonical
terminal response is observed, droids uses a bounded cancellation-independent
persistence context so a racing abort cannot erase known usage. Integer overflow,
negative token categories, and negative or non-finite costs are rejected before
aggregation. Malformed provider usage is replaced with verified zero usage and
the response becomes a protocol failure; untrusted values never enter totals.
Historical costs remain the amounts computed with the model used for each
request and are not repriced from the current catalog.

`ContextMessage` represents built-in compaction summaries and admitted boundary
messages explicitly. Provider adapters map it to a provider-supported user or
assistant form without presenting it as primary user input in diagnostic
history.

`Details` is bounded canonical JSON persisted for observers but not sent to the
model. Invalid or oversized details are omitted with a diagnostic event without
preventing the terminal tool result from completing the admitted call.

### Assistant and result content

```go
type AssistantContent interface { /* sealed by droids */ }
type ResultContent interface    { /* sealed by droids */ }

type TextContent struct {
    Text      string
    Signature string
}

type ThinkingContent struct {
    Thinking  string
    Signature string
    Redacted  bool
}

type FileContent struct {
    Filename  string
    MediaType string
    URL       string
}

type ToolCall struct {
    ID        ToolCallID
    Name      string
    Arguments json.RawMessage
    Signature string
}
```

`TextContent`, `ThinkingContent`, and `ToolCall` are valid assistant content.
`TextContent` and `FileContent` are valid tool-result content. Role-specific
sealed interfaces prevent callers from constructing invalid combinations.

Input and result constructors validate media types, URLs, inline data, and
bounded sizes. Provider adapters use `FileInput.MediaType` or
`FileContent.MediaType` to choose their image or general-file representation.
`Filename` may be empty for image media types and is required for other file
types. Provider metadata needed for replay remains opaque to tools and
applications.

## Prompt and execution API

### Prompt

```go
type PromptOptions struct {
    Steer        bool
    AdmissionKey string
}

type ExecutionHandle interface {
    TurnID() TurnID
    Wait(ctx context.Context) (Outcome, error)
    Snapshot(ctx context.Context) (ExecutionSnapshot, error)
}

func (d *Droid) Prompt(
    ctx context.Context,
    input Input,
    options PromptOptions,
) (ExecutionHandle, error)
```

Zero-option prompts start immediately when the droid is ready. With
`options.Steer`, admission requires an active steerable turn; an idle droid
returns `ErrConflict` rather than starting a new turn. When occupied, the option
selects whether to reject the input or steer the current turn. `Prompt` returns
only after prompt or steering admission is durably acknowledged by the Store.

The call context bounds admission only. Canceling it after admission does not
abort execution. `ExecutionHandle.Wait` similarly cancels only the wait.

Without an admission key, each successful `Prompt` call is a new instruction.
An embedding application crossing a separate durable boundary may supply a
bounded `AdmissionKey`. The key and normalized input hash are committed
atomically with immediate prompt admission. Repeating the same key and input
returns a handle for the original turn, including its terminal outcome after
restart; reusing the key with different input returns `ErrConflict`.
Admission keys are not accepted for steering. Applications remain responsible
for choosing stable, unique keys and for deciding whether an ambiguous request
should be retried.

Provider failures, tool failures, aborts, and context overflow are represented
by `Outcome`. Go errors from `Prompt` and `Wait` represent API, admission,
context-wait, closed-droid, or unrecoverable Store failures.

### Outcome

```go
type ExecutionStatus string

const (
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

type ExecutionSnapshot struct {
    TurnID TurnID
    Status ExecutionStatus
    Reason string
    Error  *DroidError
}

type Outcome struct {
    ConversationID ConversationID
    TurnID         TurnID
    Status         ExecutionStatus
    FinalMessage   *MessageEnvelope
    Error          *DroidError
    CheckpointID   CheckpointID
    Usage          Usage
}
```

A paused outcome is non-terminal and can be resumed. Completed, failed, and
aborted turns are terminal. An interrupted turn remains recoverable until the
caller resumes or aborts it.

### Waiting for quiescence

```go
type QuiescentKind string

const (
    QuiescentSettled     QuiescentKind = "settled"
    QuiescentPaused      QuiescentKind = "paused"
    QuiescentRecoverable QuiescentKind = "recoverable"
)

type QuiescentState struct {
    Kind      QuiescentKind
    TurnID    TurnID
    Execution *ExecutionSnapshot
    LastEvent EventSequence
}

func (d *Droid) WaitQuiescent(
    ctx context.Context,
) (QuiescentState, error)
```

`WaitQuiescent` returns an atomic state only when no provider request, retry
wait, tool call, or admitted execution is runnable. It distinguishes a settled
conversation from paused and recoverable turns without a second racing snapshot
read.

A caller that only wants execution to proceed does not need to inspect this
state first:

```go
droid, err := droids.Spawn(ctx, id, config)
if err != nil {
    return err
}
if err := droid.Resume(ctx); err != nil {
    return err
}
```

A caller may still inspect `QuiescentState.Execution.Reason` before deciding to
resume or abort an interrupted turn. Event consumers obtain the same
classification from durable pause and interruption events.

## Prompt behavior

### Default

With zero-value options, `Prompt` starts immediately when the droid is ready. If
a turn is running, paused, aborting, or recoverable, it returns `ErrBusy` without
admitting the input.

### Steering

With `PromptOptions{Steer: true}`, an idle droid returns `ErrConflict`. When a
turn is active or paused, `Prompt` durably records the input as pending steering
for that turn and returns the current execution handle. Pending steering is consumed in
acceptance order after the active assistant response and complete tool batch,
before the next provider request. It keeps an otherwise complete execution
alive. Steering accepted while paused waits for resume.

A recoverable interrupted turn is not steerable and must first be resumed or
aborted. Admission is serialized with settlement, so steering that loses a race
with normal settlement returns `ErrConflict`; the embedding application may
then admit an ordinary new turn without risking an orphaned instruction.

### Caller-managed follow-ups

Droids does not enqueue prompts for future turns. A caller that wants follow-up
behavior owns that queue and submits the next prompt with zero-value options
after the droid becomes ready. The caller is responsible for any durability,
ordering, restoration, or batching required by that queue.

## Boundary-message API

Boundary messages carry external agent-domain events into a conversation
without pretending they are user prompts.

```go
type BoundaryMessage struct {
    ID         string
    ReceiptIDs []string
    Kind       string
    Source     string
    Content    []InputContent
    Details    json.RawMessage
}

func (d *Droid) Inform(
    ctx context.Context,
    message BoundaryMessage,
) error

func (d *Droid) BoundaryReceived(
    ctx context.Context,
    id string,
) (bool, error)
```

Boundary messages are persisted and ordered by the droid. A non-empty `ID`
makes one materialized boundary identifiable. `ReceiptIDs` atomically records
all application source records represented by a coalesced boundary; when it is
empty, `ID` is also the sole receipt. `BoundaryReceived` lets an application
reconcile delivery without inferring it from compactable model context.
Repeating a fully received set is a successful no-op. `Details` carries bounded,
canonical JSON metadata that remains available on the pending boundary and its
materialized `ContextMessage`. When an execution is active, boundaries are
injected at the next safe model boundary. When idle, they wait for the next
user-initiated turn and do not start work by themselves.

Applications use this API for events such as child-agent completion. Droids
does not define application-specific child or mailbox types.

## Pause, resume, continuation, abort, and recovery

### Pause and resume

```go
func (d *Droid) Pause(
    ctx context.Context,
    reason string,
) error

func (d *Droid) Resume(ctx context.Context) error
```

Pause is cooperative. It records the request immediately, then transitions at
the next safe model boundary after the active response and tool batch quiesce.
The execution remains `pausing` until that checkpoint is durable.

`Resume` is an idempotent “continue if needed” operation:

- ready, running, retrying, pausing, or aborting: no-op;
- paused: validate the checkpoint and start a new attempt under the same turn;
- interrupted and safe to continue: validate context and start a linked
  successor under the same turn;
- interrupted after an abort request: complete aborted settlement without model
  work;
- interrupted with an unsafe continuation: return `ErrUnsafeContinuation`;
- closed: return `ErrClosed`.

A successful state transition is durable before `Resume` returns; model and tool
work continues asynchronously. Resuming a cycle-budget pause grants the new
attempt the configured execution budget again. Callers change policy through
droid configuration rather than passing a budget to `Resume`.

### Continuation primitive

Continuation is owned by droids and is not an unconstrained way for callers to
force another response from a settled turn:

```text
normal tool loop       droids continues automatically
provider retry         droids continues in a new attempt
compaction recovery    droids continues after checkpointing
paused execution       caller uses Resume
interrupted execution  caller uses Resume or Abort
settled turn           Resume is a no-op
```

These paths share one internal validated-continuation primitive. `Resume`
selects the state-appropriate behavior without requiring the caller to classify
the droid first.

`Resume` validates an interrupted turn's active-context prefix and creates a
linked successor under the same turn without appending the primary user message
again. It returns `ErrUnsafeContinuation` when tool execution may have started
without a durable raw result, the required hook is unavailable, the message tail
is invalid, a checkpoint is missing, or provider replay metadata is
incompatible.

### Abort

```go
func (d *Droid) Abort(ctx context.Context) error
```

`Abort` is the single operation for discarding unfinished work:

- running, retrying, or pausing: durably request abort, prevent new work, and
  cancel provider/tool contexts;
- paused or interrupted: settle the turn as aborted without model or tool work;
- already aborting: no-op while settlement continues;
- ready or settled: no-op.

For active work, the call acknowledges the abort request; settlement occurs only
after provider/tool workers quiesce and the terminal state is durable.
`ExecutionHandle.Wait` or `WaitQuiescent` observes that completion.

A tool already running may have produced side effects. Aborting preserves its
execution-started record for diagnostics but prevents continuation, so no
separate discard operation is needed.

## Execution policy

```go
type ExecutionBudget struct {
    MaxModelCycles uint64
}

type ExecutionPolicy struct {
    Budget           ExecutionBudget
    ToolExecution    ExecutionMode
    MaxParallelTools int
}
```

Defaults:

- `MaxModelCycles == 0`: unbounded
- `ToolExecution`: parallel
- `MaxParallelTools`: 4

The droid serializes its model cycles. Parallelism applies only inside an
eligible tool batch.

When a finite cycle budget is exhausted at a safe boundary, droids persists a
checkpoint and pauses before issuing another provider request.

## Retry policy

```go
type RetryPolicy struct {
    Enabled          bool
    MaxRetries       int
    BaseDelay        time.Duration
    MaxDelay         time.Duration
    UseProviderDelay bool
}

func DefaultRetryPolicy() RetryPolicy
```

Initial defaults:

```text
Enabled          true
MaxRetries       3
BaseDelay        2 seconds
MaxDelay         60 seconds
UseProviderDelay true
```

Retry delays use exponential backoff and honor a shorter or longer provider
request only within `MaxDelay`. Retry waits are cancellable.

Authentication, entitlement, invalid-request, protocol, and exhausted-usage
errors are not retried by default. Rate-limit, transient server, and transport
errors are eligible. Context overflow uses compaction recovery rather than the
ordinary retry budget.

Each retry starts a durable `AttemptID` inside the existing execution and turn.
No user input or completed tool call is repeated.

## Built-in automatic compaction

Compaction is always available and is triggered automatically from measured
context pressure or a provider-confirmed overflow. Automatic compaction,
overflow recovery, explicit settled-context compaction, and target-model
adaptation use one compactor. See [ADR 0024](./adrs/0024-unify-context-compaction.md)
for the complete contract.

```go
type CompactionConfig struct {
    Prompt string
    Model  Model // optional resolved, provider-bound summary model
}
```

These are the only initial compaction settings:

- `Prompt`: optional replacement for the built-in summarization prompt.
- `Model`: optional resolved model used for summary generation.

Zero values use the built-in prompt and active conversation model (or the
requested target during adaptation). The built-in prompt requests plain text
under these headings:

```text
## Goal
## Constraints & Preferences
## Progress
## Key Decisions
## Next Steps
## Critical Context
```

The headings are prompt content, not a new public structured-output schema.

The built-in process:

```text
measure active context
  below threshold → continue normally
  threshold crossed or provider overflow
    choose oldest compactable prefix
    target an approximately 20,000-token estimated suffix
    choose safe complete boundaries
    summarize prefix or fold complete source chunks incrementally
    construct summary + retained suffix
    validate provider replay shape and target size
    persist checkpoint
    continue with replacement context
```

Droids never splits an assistant tool-call message from its complete result
batch. A cutoff may occur inside a user turn, but the assistant/tool-result
group remains together. Diagnostic history remains unchanged. Only active
provider context is replaced. The 20,000-token suffix is a heuristic, not a
minimum; target fit or replay may retain less, and `Force` may compact a
non-empty context below that target. In-turn compaction protects newly admitted
input and the latest tool exchange not yet consumed by a model response. If that
protected tail cannot fit, compaction fails rather than discarding its evidence.

Summary generation is a fresh textual request with one user input, not
provider-native replay. Its projection retains user/assistant text, tool names
and arguments, tool error/success status, and boundary/attachment descriptions;
it omits thinking, opaque metadata, and tool output only from summary input. A
prior checkpoint summary is a separate update input.

If the prepared prefix cannot fit the summary model's actual metadata input or
context limits, droids folds bounded sequential requests: each sends the
previous summary plus the next serialized complete source-message chunk. An
assistant/tool-result group is one indivisible chunk. Only the final summary is
installed. If one complete source message cannot fit with the previous summary
and prompt, compaction fails without a checkpoint and without truncating source
text. Every observed summary response contributes usage, even if a later chunk
or final commit fails; automatic compaction inside a turn also contributes to
that turn's usage. There is no arbitrary 100,000-token cap. Candidate selection
reserves summary headroom, carries generated memory forward when shortening the
suffix, and bounds paid candidate attempts rather than repeatedly summarizing
the same prefix.

Compaction emits started, completed, and failed events. A failed or insufficient
compaction preserves the prior checkpoint and returns a typed compaction or
context-overflow outcome. Summary validation requires `StopReasonStop`; any
non-stop result (including `StopReasonLength`), empty output, and tool calls are
rejected. Checkpoint installation guards the captured request configuration as
well as the source context; concurrent reconfiguration returns a conflict
without installing a replacement validated against obsolete budgets.

No new settings, public schema, wire/protocol value, storage version, or
post-turn schedule is added. File-tracking metadata and summary retry promises
are outside this contract.

### Quiescent target-model adaptation

A host preparing to change model configuration may ask droids to assess or adapt
settled active context without taking ownership of compaction mechanics. These
operations use the same compactor and safe boundaries:

```go
type ContextTarget struct {
    Model     Model // resolved, provider-bound target model
    Reasoning string
}

type ContextAssessment struct {
    Target             ContextTarget
    Usage              ContextUsage
    ReplayCompatible   bool
    RequiresCompaction bool
}

type CompactContextOptions struct {
    OperationID string
    Target      ContextTarget
    Force       bool
}

type CompactContextResult struct {
    OperationID  string
    Target       ContextTarget
    Forced       bool
    Compacted    bool
    CheckpointID CheckpointID
    Before       ContextUsage
    After        ContextUsage
}

func (d *Droid) AssessContext(
    ctx context.Context,
    target ContextTarget,
) (ContextAssessment, error)

func (d *Droid) CompactContext(
    ctx context.Context,
    options CompactContextOptions,
) (CompactContextResult, error)
```

Targets use resolved models; durable metadata uses exact namespaced IDs.
`none` reasoning is canonicalized to `off`; other unsupported levels fail
validation against the target model.

Both operations require a settled conversation. They reject active, paused, or
recoverable work with `ErrBusy`. While assessment or adaptation is running,
prompt admission and semantic forks also return `ErrBusy`; `WaitQuiescent`
waits for maintenance to finish. Shutdown cancels provider work and waits for
maintenance settlement. Boundary admission may proceed concurrently and is
merged into the latest runtime state rather than overwritten by a captured
context snapshot.

`AssessContext` reports target replay compatibility separately from context
pressure. Target replay incompatibility requests adaptation rather than making
assessment itself fail. Cancellation remains an operation error.

`CompactContext` uses the same compactor. It normally compacts only when
adaptation or the automatic threshold requires it; `Force` attempts it for any
non-empty settled context, including one shorter than the suffix target. An
empty context remains a durable no-op. The replacement must reduce context,
remain runnable by the configured model, and pass current and target replay and
budget validation. Target fit or replay may retain a shorter suffix, but source
messages are never split or truncated. The operation does not change the droid's
configured model. Exhausting valid candidates, including an oversized single
message, returns `ErrContextNotAdaptable` and preserves the prior checkpoint.

`OperationID` is a bounded, renderer-safe idempotency identity. Starting work
stores an immutable operation intent that binds the ID to its exact target and
force mode even when adaptation later fails. A successful compaction or no-op result also stores
an immutable receipt in the same Commit as its checkpoint and completion events.
Concurrent identical calls join one flight; later successful retries return the
original persisted result before checking busy state or resolving the current
model catalog. Reusing an ID with another target or force mode returns
`ErrConflict`, including after a failed attempt. Intents and receipts are inherited by forks as globally
unique ancestral operation identities and contain no branch-local revision or
event cursor.

Explicit maintenance emits conversation-level `compaction.started`,
`compaction.completed`, `compaction.failed`, and `context.updated` events with
the operation ID. It does not create a synthetic user turn. Summary envelopes
retain canonical provenance from the newest message represented by the
compacted prefix.

## Tool SDK

### Defining a tool

```go
type ToolContext struct {
    ConversationID ConversationID
    TurnID         TurnID
    AttemptID      AttemptID
    ToolCallID     ToolCallID
}

type Tool[Args any] struct {
    Name        string
    Description string
    Parameters  map[string]any
    Mode        ExecutionMode
    Execute     func(
        ctx context.Context,
        call ToolContext,
        args Args,
        update ToolUpdate,
    ) (ToolResult, error)
}

func NewTool[Args any](tool Tool[Args]) (AnyTool, error)
func MustTool[Args any](tool Tool[Args]) AnyTool
```

Construction validates the name, execution mode, function, explicit schema, and
argument type. `MustTool` panics on invalid definitions and is intended for
package-level declarations and tests.

When `Parameters` is nil, droids derives an inline JSON-object schema from
struct fields and `json`/`jsonschema` tags. The root argument type must encode as
an object; unsupported recursive or non-object roots return an error. Explicit
schemas must also describe an object.

Raw arguments are validated against the tool schema and then decoded as one
JSON object. Unknown fields are rejected unless the explicit schema permits
additional properties.

`ToolContext` exposes stable identities so a tool calling an external service
can use `ToolCallID` as an idempotency key.

### Tool results

```go
type ToolResult struct {
    Content   []ResultContent
    Details   json.RawMessage
    IsError   bool
    Terminate bool
}

type ToolResultDelta struct {
    Content []ResultContent
    IsError bool
}

type ToolUpdate func(ToolResultDelta)

func EncodeDetails(value any) (json.RawMessage, error)
```

`EncodeDetails` validates and bounds canonical JSON before tool execution
returns. Updates are append-only and transient. The returned result is
authoritative and durable.

`IsError` reports an application-level tool failure to the model without
failing the droid runtime. Returning a Go error is converted into an error tool
result.

`Terminate` requests normal completion after the entire batch quiesces and all
results are durable. By default, any terminating result prevents another model
cycle.

### Execution modes

```go
type ExecutionMode string

const (
    ModeDefault    ExecutionMode = ""
    ModeSequential ExecutionMode = "sequential"
    ModeParallel   ExecutionMode = "parallel"
)
```

If the execution policy or any tool in a batch is sequential, the whole batch
runs sequentially. Otherwise calls run concurrently up to `MaxParallelTools`.

Starts and durable results use assistant source order. Live completions may
arrive in completion order.

### Tool hooks

```go
type BeforeToolCallHook func(
    ctx context.Context,
    call ToolContext,
    request ToolCall,
) (BeforeToolResult, error)

type BeforeToolResult struct {
    Reject bool
    Reason string
    Result *ToolResult
}

type AfterToolCallHook func(
    ctx context.Context,
    call ToolContext,
    result ToolResult,
) (*ToolResult, error)
```

Droids validates arguments and durably admits the tool call before invoking any
hook. Hook functions are configured capabilities and are not serialized. Droids
persists the hook phase, stable `ToolContext`, tool request, decisions, and raw
tool result needed to invoke the configured hook again after reopening.

The durable lifecycle is:

```text
tool call admitted
  before-hook pending
    reject/short-circuit decision durable → terminal result
    proceed decision durable
      tool execution started
      raw tool result durable
        after-hook pending
        final result durable
```

This ordering provides two restartable hook boundaries:

- If the process stops while `BeforeToolCall` is waiting for approval, `Resume`
  invokes it again with the same `ToolCallID` and request. The tool has not run.
- If the process stops while `AfterToolCall` is pending, `Resume` invokes it
  again with the already durable raw tool result. The tool is not repeated.

A process interruption or shutdown cancellation leaves the current hook phase
pending rather than converting it to a tool error. Other hook errors become
per-call terminal error results and do not crash the droid.

Hooks that coordinate external approval or other interaction must be restartable
and key external state by `ToolCallID`. Reopening must supply the hook again in
`Config`; if a required hook is unavailable, `Resume` returns
`ErrUnsafeContinuation`.

The ambiguous phase begins only after `tool execution started` is durable and
before the raw tool result is durable. Droids does not automatically re-enter
that phase because the tool may already have produced side effects.

## Provider SDK

A provider supplies model capabilities and streams one provider-neutral
assistant response.

```go
type Model struct {
    Provider        string
    ID              string
    ContextWindow   int
    MaxInputTokens  int
    MaxOutputTokens int
    Capabilities    ModelCapabilities
}

type Provider interface {
    ID() string
    Models() []Model
    Stream(
        ctx context.Context,
        model Model,
        request ModelRequest,
    ) (AssistantStream, error)
    ValidateReplay(
        ctx context.Context,
        model Model,
        messages []MessageEnvelope,
    ) error
}

type ContextUsage struct {
    InputTokens    int
    ReservedOutput int
    ContextWindow  int
    MaxInputTokens int
    Remaining      int
    Exact          bool
}

type ContextMeasurer interface {
    MeasureContext(
        ctx context.Context,
        model Model,
        request ModelRequest,
    ) (ContextUsage, error)
}

type Providers interface {
    Models() []Model
    Resolve(selector string) (Model, error)
    Model(id string) (Model, bool)
    RefreshModels(context.Context) error
}
```

`ContextWindow` and any provider-specific input/output limits are required for
a model that participates in automatic compaction. Providers may additionally
implement `ContextMeasurer` for exact token counting. Otherwise droids uses its
built-in conservative provider-neutral estimator and marks the measurement
approximate.

`ModelRequest` contains system instructions, validated active-context messages,
tool schemas, reasoning configuration, and output limits. It never contains UI
or Kit protocol types.

### Assistant streams

```go
type AssistantStream interface {
    Events() <-chan StreamEvent
    Result() (AssistantMessage, error)
    Close() error
}
```

`Stream` may return a synchronous setup error before a stream exists. Once it
returns a stream successfully:

- `Events` returns the same channel on every call;
- the channel produces ordered updates and closes exactly once;
- exactly one authoritative terminal result is available through idempotent
  calls to `Result` after the channel closes;
- request-context cancellation causes the provider to stop transport work and
  close the stream;
- `Close` cancels transport work and releases resources, and is idempotent;
- a provider never reports both a successful terminal message and a terminal
  stream error.

Droids continuously consumes or closes every stream it starts, so provider
resources cannot depend on an application reading droid events.

Provider errors use stable classifications:

```go
type ProviderErrorKind string

const (
    ProviderAuthentication ProviderErrorKind = "authentication"
    ProviderEntitlement    ProviderErrorKind = "entitlement"
    ProviderUsageLimit     ProviderErrorKind = "usage_limit"
    ProviderRateLimit      ProviderErrorKind = "rate_limit"
    ProviderTransport      ProviderErrorKind = "transport"
    ProviderContextWindow  ProviderErrorKind = "context_window"
    ProviderInvalidRequest ProviderErrorKind = "invalid_request"
    ProviderProtocol       ProviderErrorKind = "protocol"
    ProviderInternal       ProviderErrorKind = "internal"
)
```

A provider error may carry a bounded retry delay and safe diagnostic text.
Provider-specific response bodies and secrets are never placed in general
metadata.

`ValidateReplay` enforces provider/model/account constraints, including opaque
response and credential scopes.

## Events and subscriptions

### Event envelope

```go
type EventEnvelope struct {
    Sequence       EventSequence
    Durable        bool
    OccurredAt     time.Time
    ConversationID ConversationID
    TurnID         TurnID
    AttemptID      AttemptID
    Event          Event
}
```

Durable events have a non-zero conversation sequence and are written to the
Store outbox in the same transaction as their state change. Transient stream
updates have `Durable == false` and are anchored to durable identities.

### Event families

The initial event families are:

```text
conversation.created / forked
turn.admitted / started / settled
execution.started / paused / resumed / aborted / interrupted / settled
attempt.started / retry_scheduled / settled
model_cycle.started / settled
message.started / text_delta / thinking_delta / completed
steering.accepted / consumed
boundary.accepted / consumed
tool.admitted / hook_pending / hook_completed / started / raw_result / updated / completed
compaction.started / completed / failed
context.updated
usage.updated
```

Exact Go payload types are a sealed `Event` union. Stable wire values are
snake-case strings as shown above.

`conversation.created`, `message.completed`, steering and boundary admission,
attempt transitions, tool-call admission, hook phases and decisions, raw and
final tool results, compaction checkpoints, cumulative usage updates, and
settlement are durable. `UsageUpdated` carries the complete post-commit
`SessionUsage` total rather than a delta, so replay and duplicate delivery are
idempotent. Text, thinking, and tool progress deltas are transient.

### Subscription

```go
type SubscribeOptions struct {
    After            EventSequence
    IncludeTransient bool
    Buffer           int
}

type Subscription interface {
    Events() <-chan EventEnvelope
    Err() error
    Close()
}

func (d *Droid) Subscribe(
    ctx context.Context,
    options SubscribeOptions,
) (Subscription, error)
```

Subscription atomically establishes live delivery and replays durable events
after `After`. Event delivery never blocks the droid state machine indefinitely.
A slow subscriber is closed with `ErrSubscriberLagged` and can resynchronize
from a snapshot and durable sequence.

## Snapshots

```go
type SnapshotOptions struct {
    RecentMessageLimit int
}

type TurnSnapshot struct {
    ID     TurnID
    Status ExecutionStatus
    Error  *DroidError
    Usage  Usage
}

type PendingBoundarySnapshot struct {
    Message    BoundaryMessage
    AcceptedAt time.Time
}

type PendingInputSnapshot struct {
    Steering   int
    Boundary   int
    Boundaries []PendingBoundarySnapshot
}

type Snapshot struct {
    Conversation ConversationSnapshot
    Recent       MessagePage
    Active       *ExecutionSnapshot
    Pending      PendingInputSnapshot
    Context      ContextSnapshot
    Usage        SessionUsage
    LastEvent    EventSequence
}

func (d *Droid) Snapshot(
    ctx context.Context,
    options SnapshotOptions,
) (Snapshot, error)

func (d *Droid) Turn(
    ctx context.Context,
    id TurnID,
) (TurnSnapshot, error)

func (d *Droid) History(
    ctx context.Context,
    query HistoryQuery,
) (MessagePage, error)
```

A snapshot and `LastEvent` describe one atomic Store revision. Consumers apply
only durable events after that sequence. A forked conversation snapshot also
contains its immediate `ForkPoint`. `RecentMessageLimit` is clamped to a bounded
SDK maximum, and older diagnostic history is retrieved through cursor pagination.

`Turn` reads the canonical terminal status, durable error, and per-turn usage
for an exact settled turn, allowing hosts to reconstruct transient client
handles without persisting a parallel run status. `Snapshot.Usage` is the
cumulative conversation total at the same Store revision as `LastEvent`.

Diagnostic messages and active model context are separate fields. Snapshot
consumers never infer provider context by filtering display messages.

## Store contract

### Core interface

A Store instance persists one droid conversation while droids owns transition
policy. The instance is dedicated to one live `Droid`; callers do not share it
between droids. Adapters may still share lower-level implementation resources
where appropriate, except that the initial SQLite adapter uses one database file
per Store.

```go
type Store interface {
    Open(
        ctx context.Context,
        request OpenConversation,
    ) (OpenConversationResult, error)

    Commit(
        ctx context.Context,
        request CommitRequest,
    ) (CommitResult, error)

    State(ctx context.Context) (StoredConversation, error)

    Record(
        ctx context.Context,
        kind string,
        id string,
    ) (EncodedRecord, error)

    Records(
        ctx context.Context,
        query RecordQuery,
    ) (RecordPage, error)

    Events(
        ctx context.Context,
        query EventQuery,
    ) (EventPage, error)
}
```

After `Store.Open`, `Commit`, `State`, `Record`, `Records`, and `Events` are
implicitly scoped to that Store's conversation; their requests do not select
another conversation. `Record` performs an exact stable kind/ID lookup and
returns `ErrRecordNotFound` when absent.

`Store.Open` atomically finds or creates the conversation. On creation it stores
the supplied initial records and initial outbox events. Historical initial
records receive Store sequences in supplied slice order. On an existing
database it returns the stored conversation and leaves domain records unchanged.
`State` returns `ErrStoreUninitialized` when called before the Store is bound by
`Open`, allowing semantic fork initialization to distinguish an empty Store from
a failed inspection.

### Revision-based commits

```go
type OpenConversation struct {
    ID             ConversationID
    InitialRecords []EncodedRecord
    InitialEvents  []EncodedDurableEvent
}

type OpenConversationResult struct {
    Conversation StoredConversation
    Created      bool
}

type RecordScope string

const (
    RecordRuntime RecordScope = "runtime"
    RecordHistory RecordScope = "history"
)

type EncodedRecord struct {
    Kind     string
    ID       string
    Scope    RecordScope
    Sequence uint64 // zero for runtime; Store-assigned for history
    Version  uint16
    Payload  json.RawMessage
}

type StoredConversation struct {
    ID           ConversationID
    Revision     uint64
    RuntimeState []EncodedRecord
    LastEvent    EventSequence
}

type EncodedMutation struct {
    Operation  MutationOperation // put | delete | assert_absent
    RecordKind string
    RecordID   string
    Scope      RecordScope
    Version    uint16
    Payload    json.RawMessage
}

type EncodedDurableEvent struct {
    Kind       string
    Version    uint16
    Payload    json.RawMessage
    OccurredAt time.Time
}

type CommitRequest struct {
    ExpectedRevision uint64
    Mutations        []EncodedMutation
    Events           []EncodedDurableEvent
}

type CommitResult struct {
    Revision  uint64
    LastEvent EventSequence
}
```

`Commit` is atomic:

1. verify `ExpectedRevision` for the bound conversation;
2. apply all mutations or none;
3. append all supplied outbox events;
4. increment the conversation revision;
5. return the committed revision and event high-water mark.

A stale revision returns `ErrConflict`. Droids reloads and reconciles rather
than assuming an ambiguous commit failed. Revision checking protects Store
consistency but is not a distributed ownership mechanism; the embedding
application remains responsible for avoiding duplicate live droids.

`State` reloads the bounded materialized state required to run the droid:
active-context checkpoint and tail, current execution/attempt, pending steering
and boundary messages, unresolved tool calls, revision, and event high-water
mark. `Open` returns the same shape during construction; `State` is used later
for recovery and ambiguous-commit reconciliation.

`Records` provides cursor-paginated access to immutable diagnostic history. It
is separate so opening or reconciling a long-running droid does not load its
entire history into memory.

Runtime records carry an explicit cumulative-usage initialization marker. The
first open of a pre-aggregate conversation rebuilds its total from droids-owned
canonical assistant history, validates every contribution, and commits the
aggregate and marker before returning. This one-time compatibility path is
O(history); ordinary opens remain bounded. Historical versions cannot recover
compaction usage that was never recorded by those versions.

Droids encodes domain records and events before calling the Store. Adapters
interpret only the versioned envelope, mutation operation, stable record keys,
and revision; payload bytes remain opaque. This lets an adapter round-trip
newer record fields without compiling their domain types and keeps agent policy
out of the storage implementation.

### Required atomic transition groups

One `Commit` contains all records for each of these transitions:

```text
prompt admission
  immediate: user message + turn + execution + event records
  steer: pending steering input + event records

steering/boundary consumption
  pending-input disposition + canonical message + event records

normal settlement
  terminal execution/turn + event records

tool-call admission and hooks
  unique admission record + before-hook phase + tool.admitted event
  hook decision + next phase + hook event

tool execution
  execution-started marker before invocation
  raw result + after-hook phase after invocation
  final result message + terminal record + completion events

compaction
  replacement active-context checkpoint + compaction events

explicit context adaptation
  idempotency receipt + optional replacement checkpoint + completion events

provider usage contribution
  immutable contribution + cumulative runtime aggregate + absolute usage event
```

### Ambiguous commits

A Store operation can return an error after the backend committed. Every
mutation therefore carries stable identities and is idempotent. Droids resolves
an ambiguous result by reloading the conversation revision or querying the
stable record before retrying.

No adapter may treat a context cancellation as proof that a transaction did not
commit.

### Encoding

Droids owns a versioned canonical encoding for messages, runtime state,
historical records, and durable events. Store adapters preserve opaque payload bytes
exactly and do not decode/re-encode records they do not modify.

Binary or arbitrarily large tool details are bounded or placed behind an
attachment/reference mechanism before entering canonical records.

## In-memory Store

The root SDK provides:

```go
func NewMemoryStore(options ...MemoryStoreOption) *MemoryStore
```

Properties:

- process-local and non-durable;
- dedicated to one droid conversation;
- concurrency-safe;
- complete revision, mutation, and outbox semantics;
- deep-copy reads and commits so callers cannot mutate stored state;
- deterministic hooks for conformance and failure-injection tests;
- no background goroutines required after its droid closes.

The in-memory Store is the default when `Config.Store` is nil.

It is not a reduced mock. Its observable transition behavior must match the
SQLite adapter except for survival across process exit.

## SQLite Store

The `sqlitestore` package provides a CGO-free adapter using
`modernc.org/sqlite`.

```go
type Options struct {
    Path        string
    BusyTimeout time.Duration
}

func Open(ctx context.Context, options Options) (*Store, error)
func (s *Store) Close() error
```

`Open` creates or opens one database file, owns its connection pool, and applies
droids-owned migrations transactionally. One SQLite Store is passed to one
droid. The database contains exactly one droid conversation.

Kit allocates a distinct database path for each session/droid. It does not attach
droids to Kit's server-wide database or reuse one SQLite Store across sessions:

```go
store, err := sqlitestore.Open(ctx, sqlitestore.Options{
    Path: sessionDroidDatabasePath(sessionID),
})
if err != nil {
    return nil, err
}

droid, err := droids.Spawn(ctx, droids.ConversationID(sessionID), droids.Config{
    Store:     store,
    Providers: providers,
    Model:     model,
    Tools:     sessionTools,
})
if err != nil {
    store.Close()
    return nil, err
}
```

The session host owns the concrete Store lifecycle: shut down the droid first,
then close its SQLite Store. `Droid.Close` does not close a caller-supplied
Store.

Required SQLite behavior:

- foreign keys enabled on every connection;
- WAL mode for file-backed databases;
- bounded busy timeout;
- short write transactions;
- atomic schema migrations with a version table;
- one conversation per database;
- compare-and-swap conversation revisions;
- unique constraints for tool-call admission;
- outbox sequence allocation in the state transaction;
- deterministic ordering by explicit sequence, never row order;
- context-aware reads and writes;
- no process-global database or cwd state.

### Initial SQLite schema

The initial adapter uses four tables. Because each database contains one droid,
conversation IDs are not repeated on every row.

```sql
CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY CHECK (version >= 1),
    name TEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE droid_state (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    conversation_id TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    last_record_sequence INTEGER NOT NULL DEFAULT 0
        CHECK (last_record_sequence >= 0),
    last_event_sequence INTEGER NOT NULL DEFAULT 0
        CHECK (last_event_sequence >= 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE records (
    record_kind TEXT NOT NULL,
    record_id TEXT NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('runtime', 'history')),
    sequence INTEGER,
    version INTEGER NOT NULL CHECK (version >= 1),
    payload BLOB NOT NULL,
    created_revision INTEGER NOT NULL CHECK (created_revision >= 0),
    updated_revision INTEGER NOT NULL
        CHECK (updated_revision >= created_revision),
    PRIMARY KEY (record_kind, record_id),
    UNIQUE (sequence),
    CHECK (
        (scope = 'runtime' AND sequence IS NULL) OR
        (scope = 'history' AND sequence IS NOT NULL AND sequence >= 1)
    )
) WITHOUT ROWID;

CREATE INDEX records_scope_sequence_idx
    ON records(scope, sequence);

CREATE INDEX records_kind_sequence_idx
    ON records(scope, record_kind, sequence);

CREATE TRIGGER records_history_immutable_update
BEFORE UPDATE ON records
WHEN OLD.scope = 'history'
BEGIN
    SELECT RAISE(ABORT, 'historical record is immutable');
END;

CREATE TRIGGER records_history_immutable_delete
BEFORE DELETE ON records
WHEN OLD.scope = 'history'
BEGIN
    SELECT RAISE(ABORT, 'historical record is immutable');
END;

CREATE TABLE events (
    sequence INTEGER PRIMARY KEY CHECK (sequence >= 1),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    kind TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version >= 1),
    payload BLOB NOT NULL,
    occurred_at TEXT NOT NULL
) WITHOUT ROWID;

CREATE INDEX events_revision_idx ON events(revision, sequence);
```

The tables map to the Store API as follows:

| Table | Purpose |
| --- | --- |
| `droid_state` | Identity, optimistic revision, and sequence high-water marks |
| `records` with `scope = 'runtime'` | The bounded data returned by `State` |
| `records` with `scope = 'history'` | Immutable diagnostic data returned by `Records` |
| `events` | The durable outbox returned by `Events` |
| `schema_migrations` | Ordered, checksummed adapter migrations |

Droids assigns `record_kind`, `record_id`, record scope, version, and payload.
The SQLite adapter does not decode payloads or understand turns, tools, hooks,
or checkpoints.

A runtime record can be updated or moved to history. Once historical, its row
is immutable and has a monotonically allocated `sequence`. The primary key
preserves stable identity across that transition and makes `assert_absent`
enforceable for tool-call admission.

`Commit` uses one short `BEGIN IMMEDIATE` transaction to compare the expected
revision, apply record mutations, allocate record and event sequences, update
`droid_state`, and append outbox events. All changes commit or roll back
together.

The adapter's public contract remains the droids `Store`, current state,
historical records, and outbox—not its table layout.

SQLite locks and transactions are confined to one droid's database file rather
than creating cross-session lock contention through a shared file.

## Store conformance SDK

The `droidstest` package exports a shared contract suite:

```go
type StoreFactory func(t *testing.T) Store

func RunStoreContract(t *testing.T, factory StoreFactory)
```

The suite covers:

- state-before-open classification and atomic find-or-create open;
- deterministic initial history ordering;
- optimistic revision conflicts;
- compound steering and boundary-message transitions;
- durable, unique tool-call admission;
- restart during before-hook approval and after-hook processing;
- no tool replay after a durable raw result;
- ambiguous commit reconciliation;
- message and content round trips;
- checkpoint replacement;
- state-plus-outbox atomicity;
- event pagination and high-water cursors;
- interrupted and paused recovery;
- concurrent mutation races;
- cancellation at transaction boundaries.

Both `MemoryStore` and `sqlitestore.Store` must pass the same suite. External
adapters can import it later.

## Errors

The SDK exposes stable sentinel and typed errors suitable for `errors.Is` and
`errors.As`:

```go
var (
    ErrClosed                = errors.New("droid closed")
    ErrBusy                  = errors.New("droid busy")
    ErrNoActiveExecution     = errors.New("no active execution")
    ErrTurnNotFound          = errors.New("turn not found")
    ErrRecordNotFound        = errors.New("record not found")
    ErrUnsafeContinuation    = errors.New("unsafe continuation")
    ErrContextNotAdaptable   = errors.New("context cannot be adapted to target model")
    ErrConflict               = errors.New("store revision conflict")
    ErrStoreUninitialized     = errors.New("store is not initialized")
    ErrForkAlreadyInitialized = errors.New("fork destination is already initialized")
    ErrForkDestinationExists  = errors.New("fork destination already contains another conversation")
    ErrSubscriberLagged       = errors.New("subscriber lagged")
)
```

An existing matching fork returns `ForkAlreadyInitializedError`, which unwraps
to `ErrForkAlreadyInitialized` and carries the durable `ForkPoint`. It does not
attach another live droid to the destination Store.

Terminal agent outcomes use `DroidError`:

```go
type DroidError struct {
    Kind      DroidErrorKind
    Message   string
    Retryable bool
    Cause     error
}
```

`Message` is safe, bounded diagnostic text. `Cause` is process-local and is not
serialized blindly.

## Concurrency and cancellation

All exported `Droid`, execution handle, subscription, MemoryStore, and SQLite
Store methods are safe for concurrent use unless explicitly documented
otherwise.

Each droid serializes conversation mutation. Tool calls may execute concurrently
inside one batch, but their admission and canonical results remain
deterministic.

Contexts have operation-specific meaning:

- open contexts bound find-or-create loading;
- prompt/control contexts bound admission, not accepted execution lifetime;
- wait contexts detach the waiter without aborting;
- provider/tool contexts are owned by the execution and canceled by abort or
  shutdown;
- Store contexts bound one operation but cannot be interpreted as proof that an
  ambiguous transaction did not commit.

No operation creates unbounded goroutines. Pending steering, boundary-message,
and subscriber buffers are finite.

## Kit adapter boundary

Kit configures droids with:

- authenticated providers;
- Kit coding and plugin tools;
- a dedicated SQLite Store and database path for that session;
- retry and execution settings;
- optional compaction prompt/model overrides.

Kit projects droid snapshots and events into its protocol. It does not reserve
parallel run records, perform retries, decide compaction, or settle executions
itself. If Kit offers deferred follow-ups, Kit owns that queue outside droids.

The Kit session supervisor may load, retain, evict, and shut down droid
instances. Client disconnect does not abort them. The droid remains the
authority for prompt admission and abort.

## Open naming details

The architecture and behavior in this specification are normative. Exact Go
names may be refined during implementation where normal Go conventions suggest
a clearer API, provided the following do not change without updating this
specification and ADR 0004:

- droid ownership of the complete state machine;
- explicit prompt priority and lifecycle intent;
- durable acknowledgement and compound transitions;
- unbounded model cycles by default;
- built-in automatic compaction with only prompt/model overrides;
- durable tool-call admission and safe continuation;
- equivalent MemoryStore and SQLite Store semantics;
- Kit as a host and projection layer rather than a second agent runtime.
