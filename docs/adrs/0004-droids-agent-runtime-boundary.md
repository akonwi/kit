# 0004: Model a droid as an autonomous agent runtime

## Status

Accepted

## Context

The name "droid" describes an autonomous unit. Once assembled with its
capabilities, a droid manages its own memory, state, work loop, and lifecycle.
An embedding application can instruct, configure, supervise, and observe it
without becoming part of its brain.

Droids is a reusable agent runtime. It is not tied to Kit, a particular storage
engine, a renderer, or a transport. Kit is one application that constructs and
uses droids.

## Decision

### A droid is self-managing

After a host gives a configured droid an instruction, the droid carries that
work safely to settlement. The host is not required to drive model cycles,
persist individual transitions, process pending steering, launch retries,
coordinate compaction, or determine when the work has settled.

The architectural test is:

> Can the droid carry an instruction safely to settlement while the host only
> configures, supervises, and observes it?

If the host must operate part of the agent's internal state machine, that
responsibility belongs in droids.

### A droid is assembled from configurable capabilities

A droid owns when and why its capabilities are used. Adapters own their physical
implementation.

```text
Droid
  Store             one adapter instance dedicated to this droid
  Provider          model access and provider-specific translation
  Tools             configured capabilities
  CompactionPrompt  optional override of the built-in prompt
  CompactionModel   optional override of the active model
  Policies          retry, execution limits, and termination
```

The host constructs adapters, may share provider and tool infrastructure,
chooses policy values, revokes capabilities, and stops the droid. Each live
droid receives its own Store instance. Configuration does not transfer ownership
of the resulting agent mechanics back to the host.

A Store instance is bound to one conversation. The initial implementations are
an in-memory Store and a CGO-free SQLite Store. Each SQLite Store owns a distinct
database file for its droid; Kit does not place multiple droids in one SQLite
database. Future Store implementations may use another physical backend while
preserving the one-instance-per-droid contract. The droid still owns its logical
records, transaction boundaries, acknowledgement gates, checkpoints, and
recovery sequence.

### One droid owns one conversation

Droids defines the following lifecycle concepts:

- **Conversation**: the long-lived diagnostic history, active model context,
  pending steering and boundary messages, and lifecycle state owned by one
  droid.
- **Turn**: a durable user-facing unit beginning with accepted primary user
  input. It includes all assistant messages, steering, tool calls, and tool
  results caused by that input.
- **Execution**: one runtime instance that processes a turn. Most turns have
  one execution; explicit recovery after interruption creates a linked
  successor execution under the same turn. Execution identity is internal;
  callers operate on the droid's current execution or an opaque handle.
- **Attempt**: the initial provider/tool loop or a retry or overflow
  continuation within one execution.
- **Model cycle**: one provider request, one assistant response, and the
  optional tool batch produced by that response.
- **Tool batch**: all tool calls requested by one assistant response.
- **Tool call**: one validated invocation within a tool batch.
- **Settled**: the turn has no active, paused, or recoverable execution and its
  terminal state is acknowledged by storage.

The containment is:

```text
Droid conversation
  turn
    execution generation
      attempt
        model cycle
          assistant message
          tool batch
            tool call
            tool result
        model cycle
        ...
    successor execution generation  # interruption recovery only
```

Retries create attempts inside the same execution and turn. Steering remains in
the active turn. Explicit interruption recovery continues the original turn
without appending its primary input again.

`Step` is not a public lifecycle concept. Events and APIs use the specific terms
turn, execution, attempt, model cycle, tool batch, and tool call.

### Droids owns its state machine

A droid is the semantic authority for:

- conversation, turn, attempt, message, and tool-call identities;
- prompt admission;
- turn boundaries;
- execution and settlement;
- durable admission and consumption of pending steering and boundary messages;
- model cycles and tool batches;
- retries and backoff;
- context measurement and compaction;
- transcript validation and active-context checkpoints;
- tool-call admission and terminal results;
- persistence gates around provider and tool work;
- pause, abort, interruption, continuation, and recovery;
- lifecycle events and snapshots.

Droids does not encode:

- an embedding application's protocol or renderer types;
- a daemon, client, workspace, or network model;
- application-specific notifications or UI policy;
- session naming, cwd discovery, themes, commands, or plugins;
- SQLite, Postgres, or another concrete persistence technology;
- client attachment, reconnection, or transport behavior.

### Prompt admission and steering are explicit

Every user instruction enters through `Prompt` with one of two modes:

- **default** starts a turn when the droid is ready and returns busy otherwise;
- **steer** starts a turn when the droid is ready or records input for the
  current active or paused turn's next safe model boundary.

Continue resumes from validated context without duplicating user input. Droids
invokes it automatically during its normal loop, retries, and compaction
recovery. The public `Resume` operation is an idempotent “continue if needed”:
it resumes paused or safely recoverable work and is a no-op when the droid is
already running or ready. Abort cancels active work or settles paused and
interrupted work as aborted.

Prompt admission atomically stores the immutable identified input, turn, and
execution before returning an opaque execution handle. Steering acceptance is
acknowledged only after the pending input is durable.

Consuming steering atomically marks the pending input consumed and appends its
canonical message to the active turn. Steering accepted while an assistant
response or tool batch is active keeps the execution alive for another model
cycle. Admission closes atomically with the final consumption boundary, so an
input racing normal settlement either steers that turn or starts a new turn; it
cannot be lost or leak into an unrelated turn.

Droids does not queue prompts for future turns. An embedding application that
wants follow-up behavior owns its queue and submits the next default prompt when
the droid becomes ready. That application owns the queue's durability,
ordering, restoration, and batching.

Droids also accepts boundary messages for external agent events such as
completed child work. It owns their durable admission, ordering, and
consumption. Boundary messages are consumed at safe model boundaries and never
initiate model work while the droid is idle.

### Model cycles are unbounded by default

A turn may perform hundreds of tool calls or model cycles while it continues to
make useful progress. Droids imposes no default cumulative cycle or tool-call
ceiling.

An optional embedder budget is named for the resource it counts. For example,
`MaxModelCycles` counts provider requests rather than tool calls, and zero means
unbounded.

A budget is evaluated only when the loop needs another model cycle. If the
assistant has already completed the work, the turn settles normally even when
its consumed budget equals the limit. If another request is necessary but no
budget remains, droids pauses before sending that request.

For example, a cycle budget can stop this execution only at the marked safe
boundary:

```text
Turn T, Execution E, Attempt A1

model cycle 12
  assistant requests tools
  tools run
  source-ordered results become durable

-------------- safe model boundary --------------
cycle budget exhausted
  active context checkpoint becomes durable
  Attempt A1 ends: limit_reached
  Execution E becomes: paused
-------------- no cycle 13 is started ------------
```

The checkpoint contains the exact validated context needed by the next provider
request, the durable message high-water mark, consumed budget counters, and the
pause reason. No provider request or tool invocation is in flight when the
paused transition is acknowledged.

Pausing is neither successful completion nor provider failure. All completed
messages and tool results remain part of the turn, but there is no fabricated
final assistant response.

A paused execution holds no provider or tool slot while retaining its turn:

```text
                         steer
                           │
                           ▼
                  pending on current turn
                           │
active ──limit reached──> paused ──resume──> attempt A2 ──> active
                           │
                           └──abort──> settled: aborted
```

Resume validates the checkpoint and starts another attempt under the same
execution. Resuming renews the configured finite cycle budget for that new
attempt; callers do not pass a budget to the operation. Steering waits for that
resume.

The budget check occurs before work begins, so droids never executes a tool
batch and then retroactively rejects that work because another provider response
is needed.

### Unbounded cycles do not mean unbounded concurrency

One droid performs one model cycle at a time. An assistant may request several
tools in a cycle, but configurable parallel-tool limits bound how many execute
simultaneously. Pending steering, boundary messages, event delivery, and tool
updates are bounded, and blocking provider and tool work observes context
cancellation.

These controls bound concurrent work and retained memory without limiting how
many productive cycles or tool calls a turn may complete. Provider adapters or
embedding applications may add account-wide rate limits when their environment
requires them; such scheduling is not part of a droid's conversation semantics.

### Droids owns the productive loop

At each safe boundary, the droid behaves conceptually as follows:

```text
before each model request
  observe cancellation
  consume accepted steering and boundary messages
  evaluate explicit stop or budget policy
  measure and automatically compact context when required

request and stream one assistant message

if the response requests tools
  validate complete calls
  durably admit each tool call before invoking it
  execute the batch with bounded concurrency
  persist source-ordered terminal results
  continue
else if steering was accepted before admission closed
  continue
else
  settle the turn
```

Real tool execution requires an explicit provider tool-use stop. When a response
stops for output length while containing tool calls, droids does not execute
potentially truncated arguments. It appends one synthetic error result per call
in source order and continues so the model can issue complete replacements.
Provider adapters support that canonical replay shape.

A tool result may request termination. The complete batch first quiesces and all
results are stored. By default, any terminal result prevents another model
cycle. This is normal control completion rather than a provider failure.

### Droids owns storage coordination

A droids `Store` is defined in agent-domain terms. It supports the atomic
operations needed to:

- atomically find or create a conversation;
- admit prompts, turns, and executions;
- append canonical messages once;
- consume pending steering and boundary messages into canonical messages;
- settle a turn;
- start, pause, resume, and finish attempts and executions;
- durably admit a tool call before hooks or invocation;
- persist hook phases and decisions;
- persist a raw tool result before after-hook processing;
- complete the admitted tool call with its final terminal result;
- install an active-context or compaction checkpoint;
- settle or interrupt an execution;
- append durable transitions to a conversation outbox;
- load conversation and validated continuation state.

Droids initially includes an in-memory implementation for ephemeral use and
conformance testing and a CGO-free SQLite implementation for durable local use.
Each instance is bound to one droid conversation. Future adapters may provide
equivalent semantics with another backend.

Store mutations use optimistic conversation revisions. Droids does not provide
distributed ownership for a conversation; the embedding application must not
open multiple live droids with the same conversation ID.

Canonical message appends are acknowledged before their content is used in a
later provider request. A storage failure stops the attempt with a typed
persistence-blocked outcome. The droid does not continue with memory-only state
that cannot be reconstructed.

Every durable transition appends a monotonically sequenced domain event to the
conversation outbox in the same transaction. Event consumers can restart from a
high-water mark without losing the event between state mutation and
publication.

Live text, thinking, and tool-progress deltas may remain transient. They are
anchored to durable turn, attempt, and message identities. Completed messages,
steering and boundary admission, attempt transitions, tool-call admission, hook
phases and decisions, raw and final tool results, checkpoints, and settlement
are durable state-plus-outbox transitions.

### Tool hooks and side effects have durable boundaries

Before invoking a hook or tool, droids durably admits the call under its stable
tool-call identity. Conversation revision and uniqueness checks ensure that only
the runtime whose admission was acknowledged may process it.

Hook functions are configured capabilities rather than serialized code. Droids
persists which hook phase is pending, each returned decision, and the stable
inputs needed to invoke the configured hook again after reopening. A before-hook
waiting for approval can therefore be re-entered on `Resume` without invoking
the tool. After tool execution returns, droids persists the raw result before
running the after-hook, so that hook can also be re-entered without repeating
the tool.

The ambiguous side-effect boundary begins only after tool execution may have
started and before its raw result is durable. That state prohibits continuation.
Droids never rolls back or repeats an unknown side effect merely because result
persistence failed.

Tool batches guarantee:

- complete call validation before execution;
- sequential execution when the batch or any contained tool requires it;
- otherwise concurrent execution under a configured limit;
- source-ordered starts and canonical result messages;
- completion-ordered live terminal events;
- append-only progress updates;
- per-call error results for hook or tool failures;
- cancellation propagation;
- complete durable results before the next provider request.

### Droids owns retries and settlement

Providers return typed errors and retry metadata. Droids applies its configured
retry policy, including attempt count, backoff, provider-requested delay bounds,
and cancellation. Retry lifecycle is part of droid state and events.

A retry continues from the latest validated context under the same execution
and turn. It does not duplicate primary input or repeat tools with acknowledged
results. The droid settles only when no retry, overflow recovery, pause, or
recoverable interruption remains.

Policy values are configurable. The retry state machine and its safety
invariants are not delegated to the host.

### Compaction is built in and automatic

Every droid manages its own context pressure. Compaction is not an optional
adapter and does not require the embedding application to orchestrate it.

Before each model request, droids measures the active context against the
selected model's limits. When the built-in threshold is crossed, droids
compacts automatically before making the request. A provider-confirmed context
overflow can trigger the same built-in compaction and retry path after any model
cycle.

```text
before model request
  measure active context
  below threshold → request model
  threshold crossed
    choose oldest compactable prefix
    summarize it with the compaction model
    build summary + retained complete suffix
    validate replacement
    persist replacement checkpoint
    request model with compacted context

provider returns context overflow
  compact using the same path
  retry inside the same execution and turn
```

The built-in compactor selects a compactable prefix and retains a recent suffix
without splitting an assistant tool-call message from its results. It generates
a provider-neutral summary, combines it with the retained suffix, and validates
that the replacement:

- reduces context and satisfies the target budget;
- preserves a complete tool-call and result tail;
- is accepted by the selected provider adapter;
- retains required model, account, and replay metadata.

The replacement and checkpoint are acknowledged by storage before use in
another provider request. Compaction changes active model context without
erasing diagnostic history.

Compaction has useful defaults and only two configurable inputs:

```text
CompactionPrompt  optional prompt override
CompactionModel   optional model override
```

Without overrides, droids uses its built-in compaction prompt and the active
conversation model. Choosing another compaction model changes only the model
used to produce the summary; droids still owns triggering, prefix selection,
validation, checkpointing, retry, and failure behavior.

If summary generation fails or no valid replacement can satisfy the target,
droids preserves the prior context and settles with a typed compaction or
context-overflow outcome. Overflow recovery remains distinct from transient
provider retry and stays inside the active execution and turn.

### Diagnostic history and active model context are distinct

A droid exposes:

- immutable diagnostic history containing accepted input, attempts, completed
  messages, errors, tool-call admission and results, steering and boundary
  transitions, and lifecycle transitions;
- replaceable active context containing the validated messages sent to the
  provider.

Continuation requires a validated active-context checkpoint. Validation
requires:

- every included canonical message is acknowledged by storage;
- partial, aborted, and terminal-error assistant messages remain diagnostic but
  are excluded from active context;
- every assistant tool-call message is followed immediately by exactly one
  terminal result per call in source order;
- every admitted tool call has a terminal result;
- the tail is valid before another assistant response;
- model, provider, account, response, and credential-scope metadata permit
  replay.

Validation returns either a continuation plan with a durable high-water mark or
a typed unsafe reason. The droid never guesses across an ambiguous side-effect
boundary.

### Process interruption requires explicit recovery

On open, droids reconstructs unfinished work and durably classifies any running,
retrying, or aborting execution as interrupted. It does not resume model or tool
work automatically.

The caller may invoke `Resume` unconditionally after opening. It is a no-op when
nothing needs continuation, resumes an intentional pause, and continues a
recoverable turn from a validated prefix. Interrupted continuation creates a
linked successor execution under the same turn without appending primary input
again. A caller that wants policy or UI control may inspect the snapshot or
quiescent state before choosing `Resume` or `Abort`.

A pending hook is restartable, but a tool whose execution may have started
without a durable raw result remains an ambiguous side-effect boundary and makes
`Resume` return an unsafe-continuation error. A fully checkpointed intentional
pause remains paused across process restart and may be resumed explicitly.

### Droids events expose its complete state machine

Durable events identify their conversation, turn, attempt, message, checkpoint,
and tool call as applicable. Event names distinguish turns from model cycles.

Snapshots expose the same semantic state as the event stream, including:

- active and terminal turns and executions;
- attempts and retry state;
- diagnostic messages and active-context checkpoint;
- pending steering and boundary messages;
- admitted tool calls and results;
- pause, interruption, compaction, and settlement state;
- outbox high-water sequence.

A host can project these events and snapshots but does not infer missing droid
state from presentation concerns.

## Kit integration

Kit acts as an operator console and host for droids:

```text
Kit
  constructs and configures droids
  supplies concrete adapters and shared resources
  supervises process lifetime
  routes authenticated client operations
  projects droid events and snapshots into Kit's protocol
  presents droid state in terminal and web clients
```

Kit owns:

- daemon and server lifecycle;
- session discovery and application metadata;
- authentication and provider, tool, and plugin composition;
- selecting droid configuration from user settings;
- one dedicated SQLite Store and database path per session droid;
- external-service adapters;
- process supervision for parent and child droids;
- protocol validation, client synchronization, and transport backpressure;
- transcript, Activity, composer, queue, retry, and error presentation.

Kit does not maintain a parallel agent state machine. Its session host delegates
prompt admission, retries, compaction, tool-call admission, continuation, abort,
and settlement to the selected droid. Kit may own a caller-level queue for
deferred follow-up prompts.

The interaction is:

```text
client prompt
  Kit authenticates and selects a droid
  droid admits prompt + turn + execution through its Store
  Kit returns the projected execution handle

droid runs autonomously
  Kit projects durable and live events to attached clients
  clients render the projected state

client steering prompt / abort
  Kit validates and routes the request
  droid performs the transition
  Kit projects the result
```

Each child agent is another configured droid. A supervisor submits child
completion as a boundary message to the parent droid; the parent owns its
admission, ordering, and consumption.

Droids may remain copied beneath `internal/` while its API and Kit integration
evolve together. Its semantic contracts remain application-neutral so a future
standalone module can expose the same autonomous unit.

## Required properties

An implementation of this decision must demonstrate:

- a droid completing more than 100 sequential model and tool cycles;
- hundreds of tool calls across one or more batches;
- independent simultaneous droids without shared conversation state;
- equivalent behavior through dedicated in-memory and SQLite Store adapters;
- independent SQLite failures and locking across droids;
- durable tool-call admission before execution;
- restart and resume while a before-hook approval or after-hook is pending;
- no repeated tool execution after its raw result becomes durable;
- deterministic recovery at every compound steering transition;
- state and outbox atomicity for durable transitions;
- steering during tool execution and a no-tool assistant response;
- safe abort and settlement-racing steering;
- caller-managed follow-up submission after readiness;
- retry without duplicate primary input or repeated acknowledged tool effects;
- overflow recovery after an arbitrary model cycle;
- durable pause, restart, steering, resume, and abort;
- automatic compaction and checkpoint acknowledgement before replacement
  context is used;
- provider and credential-scope replay rejection;
- unconditional caller `Resume` after open without automatic side-effect
  replay;
- Kit protocol projection without a parallel lifecycle implementation.

## Consequences

### Positive

- A droid is a complete autonomous agent primitive rather than a partial loop.
- Applications configure and observe agents instead of rebuilding their state
  machines.
- Storage technology is replaceable without changing agent semantics.
- Long tool-heavy turns are not terminated by a hidden cumulative limit.
- Admission, steering, retries, compaction, persistence, and tool side effects
  use one coherent lifecycle.
- Parent and child agents share the same correctness model.
- Kit clients remain decoupled through explicit protocol projections.

### Trade-offs

- The droids storage contract is substantial and requires transactional adapter
  implementations.
- Durable steering, outboxes, and tool-call admission increase agent-runtime
  complexity.
- Autonomous unbounded cycles require visible progress, cancellation, bounded
  local concurrency, context management, and cost observability.
- Applications must adapt droid domain events and snapshots into their own
  protocols and presentation models.

## Related

- [0001: Native Go application architecture](./0001-native-go-architecture.md)
- [0002: Internalize the droids agent core during the rewrite](./0002-internalize-agent-core.md)
- [Kit v2 parity ledger](../parity.md)
- `internal/droids/README.md`
