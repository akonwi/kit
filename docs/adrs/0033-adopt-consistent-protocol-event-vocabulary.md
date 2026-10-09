# 0033: Adopt a consistent protocol event vocabulary

## Status

Accepted

## Context

Session events, subagent live events, and plugin notifications describe the same
agent activity. Kit's protocol must name that activity consistently so clients
can share reducers across streams and so generated unions (ADR 0032) have
predictable, self-describing variants.

The agent core identifies one unit of agent work as a *turn*. Transcript
messages, subagent tasks, and plugin notifications are keyed by that turn.

## Decision

### Terminology

- *Turn* is the only term for one admitted unit of agent work. The session
  protocol does not use *run* in operation paths, record names, field names,
  enum names, or event kinds.
- A turn is identified by `turnId`. There is no second identifier for the same
  turn.

### Event kind grammar

- An event kind is `<subject>.<event>`. A subject may span segments
  (`assistant.text`, `session.cwd`). Segments are lowercase, and multi-word
  segments use snake_case (`peer_query`).
- A process emits `.started` and exactly one terminal `.completed`. The outcome
  (status, error flag, error details) is carried in fields of the
  `.completed` event, not in separate terminal kinds.
- Incremental output is `.delta`.
- Identified records use `.created`, `.updated`, and `.deleted`. Singleton state
  uses `.changed`.
- Assistant output keeps the `assistant.` subject so the author is explicit.
- The same activity has the same kind on every session-protocol stream.

### Session event kinds

| Concept | Kind |
|---|---|
| Turn lifecycle | `turn.started`, `turn.completed` |
| User input accepted into a turn | `user.message.added` |
| Plugin message that started a turn | `plugin.message.added` |
| Assistant output | `assistant.started`, `assistant.text.delta`, `assistant.thinking.delta`, `assistant.completed` |
| Tool calls | `tool.planned`, `tool.started`, `tool.output.delta`, `tool.completed` |
| Context compaction | `compaction.started`, `compaction.completed` |
| Provider retry | `provider.retry.scheduled`, `provider.retry.started` |
| Context and usage accounting | `context.changed`, `usage.changed` |
| Session metadata | `session.name.changed`, `session.cwd.changed` |
| Shared state | `scratchpad.changed`, `subagent.changed`, `peer_query.changed` |
| Interactions | `interaction.requested`, `interaction.resolved` |
| Annotations | `annotation.created`, `annotation.updated`, `annotation.deleted`, `annotation.submitted` |

### Subagent live event kinds

Subagent live events use a closed subset of the session vocabulary:
`turn.started`, `turn.completed`, `assistant.started`, `assistant.text.delta`,
`assistant.thinking.delta`, `assistant.completed`, `tool.planned`,
`tool.started`, `tool.output.delta`, and `tool.completed`. The server projects
the agent core's lifecycle into exactly one `turn.started` and one
`turn.completed` per child turn and does not forward other agent-core lifecycle
kinds. With a closed vocabulary, subagent live events are discriminated unions
under ADR 0032.

### Scope

- Stream frame names (`session.events`, `session.resync`) are transport
  records, not event kinds, and are unchanged.
- The plugin JSON-RPC protocol is versioned separately and keeps its method
  names.

## Consequences

### Positive

- One reducer vocabulary serves session and subagent streams; clients no longer
  translate child kinds into session kinds.
- Each kind names its subject and what happened, and terminal outcomes have one
  place to look.
- Agent-core lifecycle details no longer leak into the public contract.

### Negative

- Every client and the server change together in one protocol version.
- Renaming run-based operations, records, and fields touches every client's
  turn-status and abort paths.
