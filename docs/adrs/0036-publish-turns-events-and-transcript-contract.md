# 0036: Publish the turns, events, and transcript contract

## Status

Accepted

## Context

The session API currently exposes prompt admission, follow-up queue actions,
interaction responses, event polling, event streaming, and transcript reads as
uncatalogued routes. Its public records use both `turnId` and `runId`, while
ADR 0033 defines turn as the sole unit of admitted work. Event streaming must
also become a catalogued resumable SSE operation under ADR 0035.

## Decision

Protocol 43 publishes these capabilities as one `turns` contract slice.

- A public admitted-work identity is `turnId`; `runId` is removed from paths,
  operation and record fields, interactions, snapshots, and session events.
- Turn-scoped events carry `turnId`. Session-scoped events carry no turn
  identity.
- Event kind values use the ADR 0033 vocabulary, including `turn.started`,
  `turn.completed`, `user.message.added`, `tool.output.delta`,
  `context.changed`, `usage.changed`, and `session.name.changed`.
- Turn mutation and status operations are grouped beneath
  `/v1/sessions/{sessionID}/turns`. Session-wide transcript and event resources
  remain directly beneath `/v1/sessions/{sessionID}`.
- A finite event-page operation remains available for history and recovery.
  The live event operation is a resumable `text/event-stream` SSE response.
  A stale stream cursor emits one `session.resync` record and then closes; the
  client fetches a snapshot or event page before reconnecting.
- The catalog includes prompt submissions, prompt commands, follow-up restore
  and promotion, turn status and abort, interaction responses, message and
  transcript pages, event pages, and the event stream. Server, Go client, and
  native macOS client consume these catalogued operations.

## Consequences

- Protocol-42 clients and daemons are incompatible with protocol 43.
- Event and transcript reducers become turn-only and no longer reconcile two
  aliases for the same identity.
- SSE reconnect is bounded and deterministic: a resync record is a recovery
  instruction, not an in-stream error.
