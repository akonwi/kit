# 0006: Make droids authoritative for session conversation data

## Status

Accepted

## Context

Kit currently persists one canonical droid conversation in a dedicated SQLite
Store per session and also copies droid-owned turns, run status, messages, and
live events into `kit.db`. The duplicate Kit records support the current HTTP
polling protocol, transcript snapshots, generation-bound aborts, and restart
repair, but they recreate an agent lifecycle beside droids.

The duplication creates two identities for one semantic turn, requires
cross-database reconciliation, writes streamed output into Kit's shared SQLite
database, and prevents semantic forks from preserving ancestral message IDs
because Kit's message primary key is global. Droids now owns durable prompt
admission, turns, executions, history, outcomes, context, tools, events,
recovery, and semantic forks.

Kit should persist data only when Kit defines its meaning. It should not retain a
second durable representation merely to make its current transport convenient.

## Decision

### Droids is the session conversation authority

For every Kit session, its dedicated droid Store is the sole durable authority
for:

- turns, executions, attempts, and outcomes;
- user, assistant, tool-result, and context messages;
- file inputs and provider replay metadata;
- tool calls, checkpoints, retries, compaction, and recovery;
- pending steering and external boundary messages;
- durable droid lifecycle events and their high-water sequence;
- fork lineage and ancestral identities.

Kit does not persist projections of those records in `kit.db`.

The Kit session ID remains the droid `ConversationID`. Droid turn, message,
tool-call, and checkpoint IDs pass through the session protocol without
replacement. Clients scope those nested IDs by session.

### Kit persists only its registry and genuinely harness-owned state

The initial cutover keeps `kit.db` as the session registry:

```text
sessions
  id
  cwd
  name
  persistent
  parent_session_id
  model_provider
  model_id
  thinking_level
  configuration_revision
  droid_initialized_at
  created_at
  updated_at
  archived_at
```

`configuration_revision` guards one atomic update of model provider, model ID,
thinking level, and monotonic activity time. A stale expected revision fails
without changing the registry. Kit does not persist command-replay receipts for
this operation: after an ambiguous transport response, clients resynchronize
from the authoritative session snapshot rather than replaying old intent.
Droids separately retains stable operation IDs for compaction, where duplicated
work would consume provider resources and rewrite context.

Kit may later persist domain-specific harness records that cannot be represented
by droids, such as plugin process configuration or workspace presentation
metadata. Such records must describe a Kit concern rather than duplicate droid
history, status, or events.

The unused subagent and mailbox schema is removed in this cutover. Child agents
will be represented by their own droids plus a deliberately designed harness
registry when subagent supervision is implemented.

### External bash results enter through `Inform`

Direct composer bash remains a Kit-supervised process while it is running. Live
process output is transient harness state delivered to attached clients. When
the execution settles, Kit submits its command, output, status, and relevant
metadata through `Droid.Inform` using the bash execution ID as the stable
boundary and receipt identity.

Droids therefore owns the durable conversation record and decides when the
boundary becomes active model context. `ExcludeFromContext` means Kit does not
inform the droid; such an execution is transient and is not durable after the
client or daemon loses it.

Kit does not acknowledge a terminal bash result to clients until `Inform`
succeeds. It retains the completed payload in the supervising goroutine and may
retry an ambiguous failure while the daemon remains alive. A daemon interruption
may discard an unacknowledged in-flight or just-completed direct bash execution.
This initial simplification does not introduce another durable Kit execution
model solely to represent interrupted shell work.

Bash boundary content includes a versioned structured metadata object containing
command, status, exit code, truncation, timeout, and timestamps rather than
flattening those fields irreversibly into prose.

Droids exposes pending boundary identity, content, source, kind, and acceptance
time through its bounded snapshot. Once consumed, the same boundary is exposed
as a `ContextMessage` in droid history. Protocol clients choose whether and how
to render either form. Kit does not filter context messages because of UI
policy.

### Files remain droid message content

Files supplied with prompts or boundary messages remain `FileInput` or
`FileContent` in canonical droid records. Kit may stage physical bytes under its
private filesystem, but it does not persist a parallel attachment-to-message
projection. A future blob lifecycle policy may be harness-owned without copying
conversation metadata.

### Session snapshots project directly from droids

A session snapshot combines:

1. Kit session registry metadata;
2. the current droid snapshot;
3. canonical droid history projected directly into wire messages; and
4. pending droid boundaries projected separately for clients that render them.

A settled snapshot is captured while Kit excludes prompt admission and therefore
has stable history. While a turn is active, Kit omits that active turn from the
historical portion and clients reconstruct it by replaying the runtime stream
from that turn's retained `run.started` cursor, not from the stream's current
cursor. The manager appends `run.finished` before clearing its active-turn
identity, so synchronization observes either the complete active stream or the
settled canonical history.

The in-memory stream does not evict the start of its active run. If the configured
bound would be exceeded, it marks live replay unavailable rather than retaining
unbounded data; reconnecting clients show the droid's durable active messages and
wait for a settled snapshot instead of submitting new intent. This avoids
claiming that independently paged changing history and state form one atomic
point.

There is no intermediate Kit transcript table or message codec. Snapshot
fallback remains authoritative after reconnect, event lag, or daemon restart.

### Live events are transport projections, not durable Kit records

Kit projects a droid subscription directly into renderer-neutral protocol
events. Transient text, thinking, and tool updates are retained only in a
bounded in-memory stream for the lifetime of the loaded session runtime. A
runtime or daemon restart creates a new stream identity and requires clients to
resynchronize from a snapshot.

Droid durable events remain in the droid Store. Kit does not copy them into a
second event journal. Harness events that eventually require durable replay must
be derived from their authoritative harness record or receive a separately
justified harness event design.

The initial HTTP transport may continue polling this in-memory stream. Polls
name both stream identity and cursor; a missing or replaced runtime stream
requires snapshot resynchronization. Synchronization first acquires the same
per-session control lock used by prompt admission, then holds the manager's
stream and active-state locks while choosing one of two coherent cuts: settled
droid history with no active stream, or history excluding the active turn plus
that turn's retained run-start cursor. Prompt admission holds the control lock
through droid admission, active-turn binding, and `run.started` publication.
Droid events may continue buffering, but they cannot be published or followed
by active-state clearing across this cut. The lock order is control, stream,
then active state. This ADR does not require WebSocket or SSE adoption.

### One droid turn is one client run

Kit removes its parallel turn and parent-run records. After droid prompt
admission, the droid `TurnID` is both the protocol turn identity and run handle
identity.

```text
client prompt
  Kit validates session and serializes admission
  droid.Prompt durably admits the turn
  Kit returns the droid TurnID
```

Kit keeps only bounded in-memory handles and outcomes needed by currently
attached clients. Prompt admission and generation validation use one per-session
control lock. Abort holds that lock while checking the requested droid `TurnID`
and invoking `Droid.Abort`, so a successor prompt cannot be admitted between the
check and abort. Kit does not persist a duplicate status machine.

### Reconnection precedes new intent

Droids provider retry remains unrelated to client transport recovery. When a
client loses a prompt response or disconnects, it does not blindly resend the
prompt. It reconnects, obtains an authoritative snapshot, re-establishes event
context, and only then submits new intent.

The server may make a best-effort determination about an ambiguous request from
the current droid state. This initial design does not add a durable Kit request
receipt or droid admission idempotency token. Strong replay of an ambiguous
network command is deferred until a concrete transport requires it. This
explicitly supersedes ADR 0001's requirement that every client prompt generation
be durably reserved before acknowledgement; reconnect-and-resynchronize replaces
automatic ambiguous command replay for this protocol generation.

### Recovery has one agent authority

On daemon startup Kit opens each session's droid lazily. Droids classifies and
recovers its own unfinished execution state. Kit may invoke `Resume`
unconditionally and expose the resulting state, but it does not repair or settle
parallel Kit run rows.

Closing a client remains detach-only. Daemon shutdown asks loaded droids to shut
down and waits for their durable settlement. There is no Kit execution recovery
pass.

### The development schema starts at the new authority boundary

This rewrite remains a development branch and does not migrate pre-decision Kit
v2 data. The initial schema is changed in place to contain only the session
registry; no forward migration, legacy projection staging, or bash backfill is
implemented. Development databases created from the discarded schema must be
reset rather than upgraded.

The session registry records whether its droid Store has been initialized. A
registered initialized session whose Store is missing or invalid fails closed;
Kit never silently recreates it as an empty conversation. New uninitialized
sessions may create their Store once and atomically mark the registry after
successful droid initialization. Fork integration must use an equivalent
recoverable registry/store handshake.

## Required properties

The implementation must demonstrate:

- `kit.db` contains session registry data but no droid turn, run, message, or
  event projection tables;
- snapshots are reconstructed directly from Memory and SQLite droid history;
- consumed `ContextMessage` records and pending boundaries cross the protocol
  without renderer policy in the server;
- semantic-fork ancestry projects without global message-ID collisions;
- droid `TurnID` is the protocol run identity;
- concurrent prompt admission remains serialized per session;
- an abort cannot target a successor turn;
- client wait cancellation detaches without aborting droid work;
- a new runtime stream identity forces snapshot resynchronization;
- model provider, model ID, thinking level, configuration revision, and
  monotonic activity time change atomically behind an expected-revision guard;
- ambiguous configuration responses recover through authoritative snapshot
  resynchronization rather than a durable command receipt;
- live text and tool events require no writes to `kit.db`;
- every client-acknowledged terminal included bash result is durably admitted
  through `Inform` exactly once;
- excluded bash work does not enter droid history;
- runtime and daemon restart recovery uses droid state only; and
- multiple sessions continue to execute concurrently.

## Consequences

### Positive

- Kit stores what Kit creates; droids stores what droids creates.
- One semantic turn has one identity and one durable status authority.
- Streaming no longer contends on Kit's shared SQLite database.
- Semantic forks preserve ancestry without projection-key collisions.
- Snapshot and recovery code become direct projections rather than repair logic.
- Future clients can choose whether to render context and harness boundaries.

### Trade-offs

- Existing event polling must read a runtime-owned in-memory stream.
- A daemon restart requires snapshot resynchronization instead of replaying a
  Kit event journal.
- Ambiguous prompt delivery is not automatically replayed.
- In-flight and excluded direct bash executions are not durable.
- Protocol and client code must stop assuming a separately persisted Kit run.
- Droids snapshots expose more boundary detail to support presentation choices.

## Related

- [0001: Native Go application architecture](./0001-native-go-architecture.md)
- [0004: Model a droid as an autonomous agent runtime](./0004-droids-agent-runtime-boundary.md)
- [0005: Fork settled droid conversations semantically](./0005-droids-semantic-forking.md)
- [`../droids-sdk.md`](../droids-sdk.md)
