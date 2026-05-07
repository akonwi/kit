# FilePersistence extraction and turn-persistence bug plan

## Goal

Fix the user-message persistence bug without deepening `AgentRuntime`'s coupling to file storage.

Target architecture:

```text
App/composition
  ├─ AgentRuntime        // live runtime state and UX events
  └─ FilePersistence     // observes runtime and writes JSONL entries
```

`AgentRuntime` should remain live-first and should not rely on persistence succeeding during normal usage.

## Background

The bug that motivated PR #3 exposed two architectural issues:

1. Kit currently maps Pi `turn_start` directly to Kit `Turn`, but Pi turns are internal LLM/tool-loop cycles. A single user-facing Kit turn can therefore be split across multiple runtime turns.
2. Runtime persistence currently listens for `agent.turn.completed` and persists only that event's turn. If the user message is in an earlier Pi-derived turn, it can be omitted from disk.

The preferred fix is not to persist every reconstructed turn. Instead, make persistence observe explicit session mutations and make Kit turn boundaries user-facing.

## Phase 1 — Add FilePersistence as an external observer

Create a new class, likely under `src/persistence/file-persistence.ts`:

```ts
export class FilePersistence {
  constructor(runtime: AgentRuntime) {}
  dispose(): void {}
}
```

Initial responsibilities:

- subscribe to runtime events
- perform the file writes currently triggered inside `AgentRuntime.registerPersistence()`
- expose persistence failure/status events if needed
- own any persistence queue/buffer logic

Composition changes:

- instantiate `FilePersistence` next to `AgentRuntime` in the app composition layer
- dispose it before or alongside runtime disposal
- route persistence failures to app toasts without making runtime emit persistence-specific failures

Deliverable:

- `AgentRuntime` no longer has `registerPersistence()` / `persistTurnToDisk()`
- runtime no longer imports normal append persistence helpers only for background save side effects

## Phase 2 — Define persistence-worthy runtime events

Add explicit runtime events for durable session mutations.

Candidate events:

- `session.message.appended`
- `session.compaction.appended`
- `session.handoff_summary.appended`
- `session.metadata.name.changed`
- `session.metadata.model.changed`
- `session.metadata.thinking_level.changed`

The exact names can be adjusted to fit the existing runtime event map.

Important contract:

- these events describe facts that already happened in live runtime state
- persistence observes them and writes JSONL entries
- persistence should not infer durable writes by scanning all of `session.turns`

Deliverable:

- `FilePersistence` writes from mutation events rather than from broad `agent.turn.completed`

## Phase 3 — Fix Kit turn boundaries

Make Kit turns user-facing instead of Pi-loop-facing.

Desired policy:

- a Kit turn starts when Kit accepts a user submission
- assistant messages, tool calls, and tool results caused by that submission remain in that same Kit turn
- Pi's internal `turn_start`/`turn_end` events do not automatically create durable Kit turns
- queued follow-ups and steering messages must have explicit, tested turn-boundary behavior

Open point to settle before implementation:

- whether steering creates a new user-facing Kit turn or remains part of the current one

Deliverables:

- tests showing a tool-using user submission restores as one Kit turn containing the user message and all assistant/tool activity
- tests covering plain prompt, prompt command, follow-up, steering, retry/continue, and aborted/error cases

## Phase 4 — Persist user submissions at acceptance time

Ensure user input becomes part of live/durable session state when Kit accepts it, not only if Pi later echoes it through a `message_end` event.

Flow direction:

1. user submits message or prompt command
2. runtime creates/assigns the Kit turn id
3. runtime appends the user message to live session state
4. runtime emits `session.message.appended`
5. `FilePersistence` writes the JSONL message entry asynchronously
6. runtime calls Pi to continue agent execution

Deliverables:

- submitting `/review` while idle persists a user message with prompt-command metadata
- submitting `/review` while streaming preserves prompt-command metadata when queued/consumed
- transcript remains live immediately even if persistence is delayed or fails

## Phase 5 — Make file writes robust inside FilePersistence/storage

Persistence retry semantics should be real, not only runtime-level buffering.

Current storage mutates in-memory state before disk flush succeeds. If a flush fails, the state can mark entries/turns as present even when the file was not updated.

Improve one of these ways:

- track pending entries separately from committed entries until flush succeeds
- or make append operations capable of retrying the same prepared entries after a failed flush

Deliverables:

- tests where disk write fails after entries are prepared but before file write succeeds
- retry persists the same entries in order without duplicating them
- idempotency remains explicit and tested

## Suggested test strategy

### FilePersistence tests

Use a fake runtime/event emitter, not a real Pi agent.

Assert that emitted runtime mutation events produce the expected JSONL entries:

- message append ordering
- prompt-command synthetic metadata round trip
- metadata changes
- compaction and handoff entries
- failure and retry behavior

### Runtime/turn tests

Use `KitAgent`/runtime-level tests to pin turn semantics:

- one prompt with multiple tool-loop cycles is one Kit turn
- new user submission starts the next Kit turn
- follow-up and steering behavior matches the decided policy
- provider echo deduping does not duplicate user messages

### Integration smoke tests

- session rehydrates to the same transcript after a prompt-command run
- assistant/tool messages remain ordered after reload
- persistence failure shows UI notification but does not block runtime progress

## Migration notes

- Keep reading existing JSONL files as-is.
- No file format change is required for the initial extraction.
- Existing malformed sessions with assistant-only turns should remain readable.
- Avoid writing synthetic read-model turns, such as compaction summaries or subagent delegation replays, back as ordinary `message` entries.

## Completion criteria

- `AgentRuntime` no longer owns background file persistence subscriptions
- `FilePersistence` is instantiated externally and disposed externally
- persistence has focused tests independent of Pi/tool execution
- Kit turns align with the user-facing model from ADR 0004
- the `/review` dropped-user-message bug is covered by regression tests
- full test suite, typecheck, and biome check pass
