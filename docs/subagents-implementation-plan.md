# Subagent implementation plan

> Temporary implementation tracker. Delete this document after the work is
> complete and durable requirements have been incorporated into the relevant
> ADRs, product documentation, and roadmap.

## Goal

Add first-class subagents to Kit v2 as durable, concurrent, supervised child
executions. The model addresses durable child sessions by configured agent
name and never receives internal task or conversation identities. Starting work
does not hold a parent tool call open until completion. Child work survives
client detachment, remains isolated from the parent model context, and reports
results through a durable parent mailbox.

## Production behavior used as reference

Production `main` at `c9abdf2` provides:

- Markdown definitions from `~/.kit/agents/*.md`, `<cwd>/.kit/agents/*.md`, and
  plugin registrations.
- Required `name` and `description`, optional `model`, and Markdown-body
  instructions.
- Deterministic discovery with user definitions winning over project
  definitions.
- One active conversation per agent name per parent session.
- Resumable conversations reconstructed from persisted history.
- A `subagent` tool with `list_agents`, `run`, `status`, and `dismiss`.
- An isolated runtime using the parent cwd, settings, thinking level, tools, and
  system-prompt additions.
- Child tool exclusions for nested delegation, scratchpad editing, and image
  display.
- Detailed child history under `sessions/subagents/<conversation-id>.jsonl` and
  lightweight lifecycle references in the parent log.
- Live roster/transcript workspace panes and confirmed destructive dismissal.
- Recovery that marks executions interrupted when Kit restarts.

Production's `run` action blocks the parent tool call until the child finishes.
V2 intentionally replaces this with concurrent supervised execution and durable
queueing. Production's `app/docs/features/subagents.md` is stale; the code and
`docs/subagent-session-storage.md` describe its current persistence behavior.

## Scope

### Definitions

- [x] Discover `$KIT_HOME/agents/*.md`.
- [x] Discover `<cwd>/.kit/agents/*.md`.
- [x] Keep discovery non-recursive and sort filenames deterministically.
- [x] Apply first-loaded-wins precedence: user definitions, then project
      definitions.
- [x] Require non-empty `name` and `description` frontmatter.
- [x] Accept an optional model selector, including production-compatible IDs; canonicalize known selectors at delegation time and fall back to the active parent configuration with transient warning feedback when unavailable.
- [x] Use the Markdown body as child instructions.
- [x] Return non-fatal diagnostics for malformed and duplicate definitions.
- [x] Preserve a source descriptor that can later represent plugin definitions.

Plugin-contributed definitions are deferred until the plugin supervisor exists.

### Identity and lifecycle

- [x] A parent session owns zero or more subagent conversations.
- [x] Allow at most one non-dismissed conversation for each agent name in a
      parent session.
- [x] Give every conversation a stable conversation ID.
- [x] Give every submitted unit of work a distinct internal task ID without exposing it to the parent model.
- [x] Model conversation states as `idle`, `running`, `failed`, `aborted`, and
      `interrupted`.
- [x] Model task states as `queued`, `running`, `completed`, `failed`, `aborted`,
      and `interrupted`.
- [x] Keep completed, failed, aborted, and interrupted conversations inspectable
      and resumable until dismissal.
- [x] Make dismissal abort active work, abort queued work, and durably remove or
      tombstone the conversation.
- [x] Prohibit nested subagent delegation initially.

### Durable queue

- [x] `start` and idle `message` transactionally create a task in `queued`
      state before returning; running `message` durably steers the active droids
      turn without creating another task.
- [x] Keep queued tasks across client detachment and daemon restart.
- [x] Execute tasks within one conversation strictly in submission order.
- [x] Run at most one task per conversation at a time.
- [x] Enforce configurable or centrally defined global and per-session running
      limits.
- [x] Bound queue length per conversation, per parent session, and globally.
- [x] Return `SUBAGENT_QUEUE_FULL` without creating a task when a relevant bound
      is reached.
- [x] Allow queued tasks to be inspected, waited on, and canceled.
- [x] Mark a canceled queued task `aborted` without acquiring a runtime slot.
- [x] On dismissal, cancel the running task and all queued tasks
      transactionally.
- [x] On session deletion, cancel or cascade all owned subagent state.

No in-memory channel is authoritative. Scheduler wakeups are hints; claims must
use a transactional compare-and-set from `queued` to `running`.

### Scheduling policy

Use session-fair FIFO:

1. Preserve FIFO within each conversation.
2. Rotate among parent sessions with admissible work.
3. Within a session, select the oldest eligible task.
4. A task is eligible only when its conversation has no running task and both
   its session and the process have available execution slots.
5. Release slots and wake the scheduler after every terminal transition.

This prevents one parent session from monopolizing global capacity while
preserving conversational ordering.

### Child execution

- [x] Give each running conversation/task an independently cancelable context.
- [x] Create an isolated droids instance and conversation store identity.
- [x] Preserve the owner session cwd for the child execution.
- [x] Resolve the configured child model exactly, falling back to the parent's
      current model when no child model is configured.
- [x] Begin with the parent's thinking level.
- [x] Compose child instructions with applicable parent context without copying
      the parent's model transcript.
- [x] Provide the applicable coding tools while excluding the subagent tool and
      other deliberately unsupported child-only tools.
- [x] Persist authoritative transcript milestones and completed tool results.
- [x] Publish bounded live deltas and tool updates through a child event stream.
- [x] Keep client disconnects independent from execution cancellation.
- [x] Cancel children during session deletion and daemon shutdown with bounded
      cleanup deadlines.

### Restart and recovery

- [x] On daemon startup, atomically mark tasks found `running` as `interrupted`.
- [x] Keep tasks already in `queued` state queued and make them eligible after
      recovery commits.
- [x] Do not automatically retry interrupted work.
- [x] Support explicit retry as a new task with lineage to the interrupted task.
- [x] Never claim that an arbitrary provider stream or side-effecting tool has
      resumed exactly.
- [x] Start scheduling only after startup recovery has committed.

### Parent mailbox

- [x] Persist an idempotent mailbox item when a task completes, fails, aborts
      after running, or is interrupted.
- [x] Keep task and conversation identity on the internal mailbox receipt while
      exposing only agent identity, terminal state, and a bounded result/error
      summary to the parent model.
- [x] Emit a parent-session event immediately for attached clients.
- [x] Inject undelivered mailbox items into an active parent only at a safe
      boundary between model turns.
- [x] If the parent is idle or unloaded, start an autonomous context-only
      reaction turn from the durable mailbox boundary.
- [x] Let the parent model decide whether to respond, use tools, delegate more
      work, or stop, with bounded global concurrency and a durable consecutive
      reaction limit.
- [x] Mark delivery transactionally and idempotently.
- [x] Never inject the child's full transcript into the parent context.
- [x] Keep queued tasks canceled before execution visible to internal/native
      task history without delivering them to parent model context.

### Model-facing tool

Expose one built-in `subagent` tool with asynchronous actions:

- `list_agents`
- `start`
- `message`
- `inspect`
- `wait`
- `cancel`
- `dismiss`

Semantics:

- [x] `list_agents` returns discovered definitions and renderer/model-safe
      metadata.
- [x] `start(agent, message)` creates or continues the named child session and
      promptly returns agent-scoped state without storage identities.
- [x] `message(agent, message)` uses live droids steering when a turn is active
      and admits new durable work when the child is settled.
- [x] `inspect(agent)` returns current state and the latest completed response
      summary.
- [x] `wait(agent)` waits until no active or queued child work remains and
      returns immediately when already settled.
- [x] `cancel(agent)` generation-safely cancels active and queued work while
      preserving the child session.
- [x] `dismiss(agent)` destructively resets the child session.
- [x] Tool guidance tells the parent to start work and continue independently
      rather than immediately waiting.

The parent model should not need to reason about storage identities.

### Persistence model

Use Kit-owned SQLite state rather than production's nested JSONL layout.
Conceptual tables:

- `subagent_conversations`
- `subagent_tasks`
- child transcript/activity records or a bounded subagent event journal
- `parent_mailbox`

`subagent_tasks` needs at least:

- task ID
- conversation ID
- owner session ID
- sequence within the conversation
- prompt/message
- state
- priority class, initially fixed
- retry/lineage task ID
- queued, started, and finished timestamps
- terminal error or cancellation reason
- bounded result summary
- cancellation generation

Persistence requirements:

- [x] Use foreign keys that cascade from the owner session.
- [x] Keep transactions short.
- [x] Make task admission and initial lifecycle state atomic.
- [x] Make terminal task state and mailbox insertion atomic.
- [x] Represent dismissal durably before asynchronous cleanup.
- [x] Make mailbox delivery idempotent and generation-safe.
- [x] Index oldest queued work by owner session.
- [x] Index queued work by conversation sequence.
- [x] Index active work by parent session.
- [x] Index startup lookup of running tasks.
- [x] Keep SQLite authoritative for ownership, lifecycle, scheduling, mailbox,
      and client projection even if child messages use a separate droids store.

### Protocol and clients

- [x] Add subagent definitions and diagnostics to the appropriate session
      snapshot or feature operation.
- [x] Project conversation and task roster state.
- [x] Project ordered lifecycle events.
- [x] Support child transcript synchronization by conversation ID.
- [x] Add start, message, wait, cancel, and dismiss operations with validation
      and generation guards.
- [x] Project parent mailbox notifications.
- [x] Preserve snapshot/high-water synchronization and bounded replay behavior.
- [x] Ensure multiple attached clients observe one authoritative state.

### Initial native TUI

- [x] Add a singleton `/subagents` roster pane.
- [x] Show agent, model, state, active/queued tasks, and last activity.
- [x] Open one retained transcript tab per conversation.
- [x] Add compact delegation/task markers to parent Activity.
- [x] Support task cancellation.
- [x] Confirm destructive dismissal and close the dismissed conversation tab.
- [x] Preserve selection and scroll while tabs remain open.
- [x] Participate in standard narrow/wide workspace behavior.
- [x] Reset renderer-owned tabs when switching parent sessions without mutating
      server state.

## Out of scope for the first delivery

- Plugin-contributed subagents.
- Nested delegation.
- Batch delegation APIs.
- Automatic retries of interrupted tasks.
- Automatic parent model calls.
- Tool restrictions in agent frontmatter.
- Special composer syntax.
- Cross-parent conversation sharing.
- More than one simultaneously running task in a conversation.
- Migration of production subagent JSONL files.
- Full semantic web UI.

## Implementation phases

### Phase 1: discovery and domain contracts

- [x] Create `internal/subagent`.
- [x] Implement definition loading, validation, precedence, and diagnostics.
- [x] Define identifiers, states, errors, limits, and state transitions.
- [x] Define repository, scheduler, runtime-factory, and event-sink ports.
- [x] Add tests for precedence, malformed files, readable symlinks, duplicates,
      and bounds.
- [x] Define how session runtime bundle construction receives the parent-facing
      subagent tool without introducing a dependency cycle.

### Phase 2: durable state and queue operations

- [x] Add SQLite migrations for conversations, tasks, transcript/activity, and
      mailbox items.
- [x] Implement transactional queue admission and queue-bound checks.
- [x] Implement transactional task claiming.
- [x] Implement terminal transitions and mailbox insertion.
- [x] Implement queued/running cancellation and conversation dismissal.
- [x] Implement startup interruption recovery.
- [x] Add repository, migration, restart, and idempotency tests.

### Phase 3: fair scheduler and supervisor walking skeleton

- [x] Implement session-fair FIFO selection.
- [x] Enforce global and per-session running limits.
- [x] Make scheduler wakeups idempotent and state-driven.
- [x] Create isolated droids/store/context instances for claimed work.
- [x] Persist lifecycle and transcript milestones.
- [x] Deposit terminal mailbox results.
- [x] Keep execution alive after all clients detach.
- [x] Implement inspect, bounded wait, cancel, and dismiss.
- [x] Add race tests for claims, cancellation, dismissal, shutdown, and slot
      release.

### Phase 4: parent runtime integration

- [x] Register the asynchronous model-facing tool.
- [x] Exclude nested delegation from child bundles.
- [x] Build child prompts without copying parent transcript history.
- [x] Add safe-boundary mailbox consumption to the parent run loop.
- [x] Start an autonomous parent reaction turn for idle or unloaded completions.
- [x] Verify the child transcript never enters parent context except through the
      bounded mailbox projection.

### Phase 5: protocol and native TUI

- [x] Extend protocol records, snapshots, events, validation, and server routes.
- [x] Extend session-client contracts and reducers.
- [x] Build roster and retained transcript panes.
- [x] Add compact parent Activity markers.
- [x] Add cancellation and confirmed dismissal interactions.
- [x] Test reconnect, multiple observers, client detachment, and narrow/wide
      workspace layouts.

### Phase 6: roadmap extensions

- [ ] Add plugin-contributed definitions after plugin supervision exists.
- [ ] Add the semantic web workspace.
- [ ] Migrate production subagent history.
- [ ] Evaluate richer retry/reconstruction controls and configurable queue
      policies.

## Manual verification record

On 2026-09-11, an isolated authenticated CLI smoke test used a temporary
`KIT_HOME` and a real `scout` definition. The initiating `kit print --new`
parent returned conversation `subagent_014545303069d4897e2fa5c44e27c2d1`
and task `task_458e70de0976314b12615907be949b5c` while the child remained
`running`. A second `kit print --session ...` invocation observed that same
running task, and a later invocation observed its durable completed summary.

A restart scenario then submitted running task
`task_90af3ee6c6c3535c9cd0df7b35d3e355` followed by queued task
`task_7913f50c70fc4ba1a9741520010df314`. `kit daemon restart` left the first
`interrupted`, preserved and subsequently completed the queued task, and
created one mailbox item for each terminal task. The next parent CLI run
received the pending mailbox records and reported both authoritative states.
The child droid transcript was separately verified through the authenticated
conversation-transcript protocol. After final review fixes, the smoke was
repeated with conversation `subagent_9af583b84f402c8b33ca90cb356bd47c`
and task `task_f1e5e3cc9518f0a707e2b1ee526d44df`; a second CLI invocation
observed its durable completed state and summary. Each daemon was stopped and
all isolated files were removed after verification.

## Required concurrency tests

- [x] FIFO execution within a conversation.
- [x] Session fairness under global contention.
- [x] Global, session, and conversation queue bounds.
- [x] Global and per-session running limits.
- [x] Duplicate scheduler wakeups.
- [x] Exactly-once transactional claiming.
- [x] Cancellation before claim.
- [x] Cancellation racing with claim and completion.
- [x] Dismissal racing with initialization, execution, and completion.
- [x] Session deletion with queued and running tasks.
- [x] Client disconnect while work remains active.
- [x] Daemon shutdown cleanup.
- [x] Restart interruption of running tasks.
- [x] Preservation and later execution of queued tasks after restart.
- [x] Exactly-once mailbox insertion and delivery.
- [x] Safe-boundary delivery while the parent is active.
- [x] Automatic context-only parent reaction while idle or unloaded.

Run `go test -race ./...` throughout implementation.

## First vertical-slice acceptance criteria

A parent agent can start a configured `scout` by name and continue immediately
without receiving a storage identity. The scout runs when scheduler capacity is
available, accepts live steering at droids model boundaries, can be waited on by
name until settled, survives TUI detachment, streams status to another attached
client, persists its transcript, and deposits completion in the parent mailbox.
Completion reaches an active parent at a safe boundary or starts a context-only
reaction turn when the parent is idle or unloaded. The model decides how to
proceed. The named child can be inspected, messaged, canceled, or dismissed.
Restarting the daemon marks running work `interrupted`, preserves queued work,
and resumes scheduling that queued work without corrupting either transcript.
