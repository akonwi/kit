# 0021: Keep file persistence outside AgentRuntime as a runtime observer

## Status
Accepted

## Context

Kit persists sessions as append-only JSONL entry logs, while `AgentRuntime` owns the live session state used by the shell, transcript, tools, and plugins.

Today, `AgentRuntime` also performs file persistence directly:

- it imports storage functions such as `appendTurn`, `appendCompaction`, `appendSessionInfo`, and related helpers
- it registers an internal persistence subscription
- it persists only the `agent.turn.completed` payload for normal turns
- persistence failures are emitted back through runtime events

This creates several problems.

First, runtime UX becomes coupled to a file-persistence concern. The runtime should be able to accept input, update live transcript state, stream assistant output, and remain usable even if writing to disk is slow or temporarily failing.

Second, persistence is harder to test in isolation. Current tests need to partially construct or mock `AgentRuntime` internals to validate persistence behavior.

Third, the current persistence hook exposed a real bug: a single user submission that uses tools can produce multiple Pi loop cycles. Kit currently maps Pi `turn_start` directly to Kit `Turn`, so one user-facing turn can be split into multiple runtime turns. Persisting only the last completed turn can drop the original user message from disk.

The deeper issue is that Pi's loop-cycle events leaked into Kit's product-level turn model. A Kit turn should be user-facing: one user submission plus the assistant/tool activity caused by it. Pi may need multiple internal loop cycles to complete that work, but those cycles should not define durable Kit turn boundaries.

## Decision

Introduce a `FilePersistence` class outside `AgentRuntime`.

`FilePersistence` depends on an `AgentRuntime` instance and subscribes to runtime events:

```ts
const runtime = new AgentRuntime(session, options);
const persistence = new FilePersistence(runtime);
```

The dependency direction is one-way:

```text
FilePersistence -> AgentRuntime
AgentRuntime -/-> FilePersistence
```

`AgentRuntime` must not instantiate `FilePersistence`, import it, or know which persistence implementation is active.

The app/composition layer owns both objects and disposes both:

```ts
persistence.dispose();
runtime.dispose();
```

No persistence interface is introduced yet. A concrete class boundary is enough for now. If persistence needs to become configurable later, an interface can be introduced around the stabilized shape.

## Responsibilities

### AgentRuntime

`AgentRuntime` owns live runtime semantics:

- active session state
- user input handling
- model/tool execution orchestration
- live transcript events
- user-facing Kit turn boundaries
- session mutation events that describe what changed

Runtime behavior should be live-first. It should not block normal UX on file persistence.

### FilePersistence

`FilePersistence` owns file persistence:

- subscribing to runtime/session mutation events
- translating runtime mutations into JSONL session entries
- preserving append ordering
- handling write buffering and retry behavior
- exposing persistence-specific failure/status events, if needed
- disposing its subscriptions

Persistence failures should be surfaced as persistence events or UI notifications by the composition layer, not by making runtime depend on persistence.

## Runtime event direction

The current coarse `agent.turn.completed` event is not a sufficient persistence contract.

Persistence should eventually observe durable session mutation events such as:

- a message was appended to a Kit turn
- session metadata changed
- a compaction entry should be appended
- a handoff summary should be appended

This lets persistence write exactly the corresponding JSONL entries instead of inferring durable changes from a reconstructed `Session.turns` read model.

## Turn boundary direction

Kit should not treat every Pi `turn_start` as a new durable Kit turn.

A Kit turn is user-facing:

- starts with a user submission accepted by Kit
- includes assistant messages, tool calls, and tool results caused by that submission
- continues across Pi's internal tool-loop cycles
- ends when that user-facing unit of work is complete or when another user submission starts a new Kit turn

This preserves the session model described in ADR 0004 while allowing Pi to keep its own lower-level loop lifecycle.

## Non-goals

This ADR does not introduce a configurable persistence interface yet.

This ADR does not require all session discovery/opening APIs to move immediately. Existing session repository functions such as loading, listing, or deleting sessions may remain where they are while mutation persistence is extracted first.

This ADR does not prescribe the final runtime event names. The implementation should choose names that fit the current event map and can evolve incrementally.

## Consequences

### Positive

- `AgentRuntime` becomes less coupled to file storage
- runtime UX remains robust when disk writes fail or lag
- persistence can be tested independently from Pi, tools, and UI
- future persistence backends have a natural seam
- durable writes can be based on explicit session mutations rather than inferred read-model state
- the user-message-drop bug can be fixed by persisting user submissions as durable session mutations, not by persisting every reconstructed turn

### Trade-offs

- app composition must own another lifecycle object
- persistence status/failure reporting needs a small event surface outside runtime
- existing runtime methods that currently call storage directly need migration
- Kit/Pi turn boundary semantics need to be made explicit in tests

## Related

- `docs/adrs/0004-turn-based-session-model.md`
- `docs/adrs/0017-namespaced-runtime-events.md`
- `docs/adrs/0020-jsonl-session-storage.md`
- `docs/features/steering-followup.md`
