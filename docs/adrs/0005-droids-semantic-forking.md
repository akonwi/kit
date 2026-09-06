# 0005: Fork settled droid conversations semantically

## Status

Accepted

## Context

A host may need to branch an agent conversation so two droids share everything
known at one point and then evolve independently. Kit's handoff workflow is one
example: it creates another session from the current session and may submit a
new instruction to the child without changing the source session.

The droids SDK stores more than a display transcript. A conversation includes
immutable domain records, a durable event outbox, compacted active provider
context, provider replay metadata, boundary receipts, pending boundary messages,
and execution state. A byte-for-byte Store or SQLite file copy would also duplicate conversation
identity, Store revisions, outbox cursors, retry state, and possibly unsafe
in-flight tool state. It would couple the operation to one Store adapter and is
not safe around SQLite WAL files.

Forking is distinct from backup or process recovery. A fork creates a new,
ready conversation with inherited knowledge. Backup and restore may need to
retain paused, interrupted, or otherwise operational state exactly and can be
designed separately.

## Decision

Droids owns a semantic conversation-fork operation. The host supplies the new
`ConversationID` and a destination Store; it does not copy Store files or
rewrite droids records itself.

The initial API has this shape, with exact Go names subject to normal refinement
when implemented:

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

A nil destination Store has the same meaning as it does for `Open`: create an
in-memory Store. Durable hosts such as Kit always supply a dedicated persistent
Store. The child inherits the source droid's configured providers, model,
system prompt, reasoning, tools, hooks, retry policy, execution policy, and
compaction configuration. These configured capabilities may share underlying
host resources; conversation state and Store ownership do not.

The destination Store is dedicated to the child and is owned by the caller.
Forking does not transfer or close either Store.

### Fork only at a settled boundary

The initial operation accepts only a settled source conversation. Ready,
completed, failed, and aborted conversations have a settled boundary from which
a new turn can begin. Running, retrying, pausing, paused, aborting, and
interrupted conversations are occupied and cannot be forked.

In particular, a quiescent paused or interrupted execution is not considered a
safe fork point. Copying one would let two conversations resume the same turn,
repeat an approval, or cross an ambiguous tool side-effect boundary. The caller
must first settle the source through its ordinary lifecycle or wait for the
active turn to settle.

Fork does not wait for settlement implicitly. It serializes with `Prompt`,
`Inform`, pause, resume, abort, and other source mutations, checks the source
state while holding the droid's mutation lock, and returns `ErrBusy` when the
source is occupied. A racing operation is therefore ordered wholly before or
after the fork point; the fork cannot observe a partial transition.

### The child starts ready

Fork copies conversation knowledge, not the source's last execution control
state. The child starts in `ExecutionReady` with no active turn, attempt, retry,
abort request, unresolved tool phase, execution handle, provider request, or
tool worker.

The child inherits:

- complete immutable record history in source order;
- the exact active provider context, including a compacted context when one is
  installed;
- provider response metadata and signatures required for replay;
- the active-context checkpoint identity;
- durable boundary receipts;
- pending boundary messages accepted while the source was idle;
- immediate lineage identifying the source fork point.

The child does not inherit:

- an active, paused, or interrupted execution;
- pending steering for an active turn;
- subscribers or transient stream updates;
- provider transports, retry timers, goroutines, or tool workers;
- the source Store revision or record/event allocation state;
- the source durable event outbox.

Terminal errors, outcomes, attempts, and usage already present in copied
records remain available there. They are not installed as the child's current
execution state.

Fork does not invoke provider replay validation. It is a local state operation,
and the child inherits the same provider and model configuration as its source.
The ordinary model-request path validates replay before the child's next provider
request. Any credential, account-scope, content, or provider incompatibility is
reported through that prompt's normal outcome, allowing the host to reconfigure
or present recovery without preventing the fork itself.

### Ancestral identities remain stable

The child receives a new `ConversationID`. All droids-owned identities allocated
before the fork remain unchanged:

- `TurnID`;
- `AttemptID`;
- `MessageID`;
- `ToolCallID`;
- `CheckpointID`.

Embedded conversation fields in copied message envelopes and checkpoints are
rewritten from the source `ConversationID` to the child `ConversationID`.
Droids decodes and re-encodes each known versioned record that contains a
conversation identity; it never performs textual replacement inside opaque JSON
or user content. An unsupported record version fails the fork before destination
initialization.

Provider-owned response IDs, provider call IDs, credential scopes, encrypted or
opaque signatures, and other replay metadata remain byte-for-byte unchanged.
They describe the original provider interaction and are not droids identities.

Ancestral local IDs represent the same facts on both branches. Their complete
address is conversation-scoped:

```text
(source conversation, message M)
(child conversation,  message M)
```

After the fork, each branch allocates fresh IDs for new work and evolves
independently. A tool call completed before the fork remains one ancestral side
effect with one `ToolCallID`; it is not admitted or executed again in the child.

Hosts must likewise scope projections, caches, and UI state by conversation or
session rather than assuming a nested droids ID is globally unique. How a host
stores or simplifies those projections is outside the droids SDK contract.

### Lineage and event history

The destination durably records the immediate `ForkPoint`:

```text
parent conversation ID
parent revision
parent event high-water mark
```

Lineage is part of the destination's bounded conversation state, survives
reopen, and is exposed in its conversation snapshot. It is also represented by
a durable `conversation.forked` event. A destination that was initialized but
whose result was not observed can therefore be recognized and reopened safely.

The destination outbox starts fresh with atomic initialization events:

```text
conversation.created
conversation.forked
```

Source outbox events are not copied. Replaying them from the child would present
ancestral activity as new child activity and would conflate two independent
event cursors. The copied snapshot and immutable records form the child's
inherited state and message baseline; events after initialization describe only
changes to the child branch. Source-only lifecycle chronology, such as a
transient compaction failure represented only by an outbox event, remains in the
source conversation rather than becoming child history.

Historical record order is preserved, but record sequences, event sequences,
and Store revisions are scoped to a conversation. Callers must not use numeric
sequence equality across branches as lineage evidence. The `ForkPoint` and
stable ancestral IDs define the branch relationship.

### Store-neutral capture and initialization

Fork holds the source droid mutation lock while it reads the bounded runtime
state and pages immutable history through the `Store` interface. A Store is
dedicated to one live droid, so preventing droid commits during those reads
produces one consistent source revision even when history requires several
pages.

Only after the complete fork image is transformed does droids initialize the
destination with its runtime records, copied history, lineage,
and initial events. Destination initialization is atomic under the existing
`Store.Open` contract. Fork supplies historical records in ascending source
sequence, and `Store.Open` must allocate destination history sequence in that
slice order. This ordering guarantee becomes part of the Store contract and its
shared conformance suite. The source is never modified.

The destination ID must differ from the source ID. At the beginning of `Fork`,
droids inspects the destination before checking whether the source is currently
settled. An uninitialized destination proceeds to source capture. Any initialized
destination returns an already-initialized error without attaching another live
`Droid`; when its durable lineage names this source, the error also exposes the
saved `ForkPoint`. The Store contract must let droids distinguish an
uninitialized Store from a failed inspection; the exact API may be a stable
sentinel returned by `State` before `Open`.

Within one `Fork` call, the fork image carries a private, random initialization
token in addition to its semantic `ForkPoint`. Droids reconciles a destination
initialization error that may have occurred after commit by inspecting the
destination. When both the token and fork point match the image captured by that
call, construction continues and attaches exactly one child without recapturing
newer source state. The token distinguishes this call from a concurrent fork of
the same source revision and is not part of ancestral identity. A later call
that discovers the completed destination does not reopen it implicitly. A host
recovering after process exit may explicitly `Open` that conversation once it
has established exclusive ownership.

The destination ID therefore remains the host's durable idempotency identity,
but it is not permission to create multiple live droids over one Store. The
existing rule still applies: one Store instance and conversation may have only
one live `Droid`, and the host must not concurrently attach another instance.

The first implementation may materialize the complete record set in memory
before the atomic destination open. This keeps Memory and SQLite behavior
identical through the current Store contract. A future streaming snapshot/import
or adapter optimization may reduce memory and copy cost for very large histories,
but it must preserve the semantic transformation, identity, lineage, and
atomicity defined here. It must not expose raw SQLite copying as the droids
contract.

### Host integration remains separate

Fork creates and opens the child droid. It does not create a Kit session, copy a
scratchpad, choose client focus, duplicate protocol projections, or submit a
handoff instruction. Those are host operations above droids.

Kit may use the returned child and fork point to create its session lineage and
then submit an optional handoff message as the child's first ordinary prompt.
Because Kit metadata and the child droid Store are separate durability domains,
Kit must make its larger handoff workflow idempotent and recoverable. That
coordination does not move conversation-copy semantics out of droids.

Whether Kit can retain fewer droid-derived projections is intentionally deferred
and does not change this fork contract.

## Required properties

The implementation must demonstrate:

- equivalent forks through Memory and SQLite Stores, including cross-adapter
  source and destination pairs;
- independent source and child prompts after the fork;
- unchanged source state and history;
- a new child `ConversationID` with stable ancestral turn, attempt, message,
  tool-call, and checkpoint IDs;
- fresh IDs for work admitted independently after the fork;
- preserved provider IDs, signatures, account scope, and replay metadata;
- correct forks after compaction and after completed, failed, and aborted turns;
- copied pending idle boundaries and boundary receipts without duplicate
  materialization;
- no inherited provider request, retry, hook, tool execution, or side effect;
- rejection of running, paused, interrupted, aborting, and otherwise occupied
  sources;
- deterministic ordering when prompt, boundary admission, or control operations
  race a fork;
- complete immutable record-history capture across Store pagination;
- atomic destination initialization and reconciliation of ambiguous results
  without attaching multiple live droids;
- typed detection of an already initialized matching fork and rejection of a
  destination belonging to another conversation or fork;
- a fresh child outbox containing creation and fork lineage rather than copied
  source events;
- successful reopen and continued execution of both branches.

## Consequences

Positive:

- handoff and branching become reusable droids capabilities rather than
  SQLite- or Kit-specific copying;
- child conversations begin ready with the source's exact active model context;
- completed tool side effects and provider exchanges retain their ancestral
  identity;
- source and child can run concurrently without sharing mutable conversation
  state;
- lineage and destination initialization are durable and retryable;
- Memory, SQLite, and future Store adapters expose the same behavior.

Trade-offs:

- settled-only forking does not capture or branch an active execution;
- hosts must qualify nested droids IDs by conversation or session;
- copying complete immutable record history can be expensive for long conversations;
- versioned history records require droids-owned fork transformations;
- host-level session creation and droid initialization still require a
  recoverable multi-store workflow;
- exact backup, restore, copy-on-write storage, and Kit projection reduction
  remain separate design work.

## Related

- [0002: Internalize the droids agent core during the rewrite](./0002-internalize-agent-core.md)
- [0004: Model a droid as an autonomous agent runtime](./0004-droids-agent-runtime-boundary.md)
- [Droids API and SDK specification](../droids-sdk.md)
- [Kit v2 parity ledger](../parity.md)
