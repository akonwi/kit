# 0066: Share session status with plugins

## Status

Accepted. Extends plugin protocol version 2
([ADR 0065](0065-stabilize-the-plugin-protocol-as-version-2.md)) within its
compatibility rules; no new protocol version.

## Context

Clients show a session's model, thinking level, parent session, context use,
and cumulative token usage and cost; the TUI and macOS debug views list them.
Plugins see none of this. A plugin that shows the session's cost in the
footer, warns as the context fills, stops tool calls past a spending limit, or
records what an experiment cost has no way to learn the numbers, and a plugin
that keeps state across turns cannot tell when the conversation was
compacted.

## Decision

### Session status

Kit gives each plugin instance its session's *status*:

```json
{
  "sessionId": "session_…",
  "model": "anthropic/claude-sonnet-4-5",
  "thinkingLevel": "medium",
  "parent": { "id": "session_…", "name": "Building Kit" },
  "context": { "tokens": 48210, "window": 200000 },
  "usage": {
    "input": 812345, "output": 40211, "cacheRead": 700000, "cacheWrite": 52000,
    "reasoning": 9000, "totalTokens": 1613556,
    "cost": { "input": 2.44, "output": 0.6, "cacheRead": 0.21, "cacheWrite": 0.2, "total": 3.45 }
  }
}
```

- `sessionId` is the session the instance belongs to.
- `model` is the `provider/id` selector and `thinkingLevel` the applied
  level, as clients show them.
- `parent` is the session this one was started from, such as a subagent's
  parent, or null. Its `name` is null when the parent has none.
- `context` is the estimated input of the latest model request and the
  model's context window, as clients' live context events report them, or
  null before Kit knows the window.
- `usage` is the session's cumulative provider usage and cost in US dollars,
  the session API's `SessionUsage` record. It covers this session only; a
  subagent runs as its own session with its own status.

The record reuses the session API's usage type, so plugins and clients show
the same numbers.

### Delivery

- `initialize` params carry `status` with the values when the instance
  starts.
- Kit sends `kit/events/session.status.changed`, whose params are the full
  status, when any member changes. Usage and context change with every model
  response, so Kit coalesces: an instance has at most one status notification
  queued, and a newer status replaces a queued one that has not been written.
  An instance therefore always ends with the current status without its queue
  growing.
- A plugin can ask with the request `kit/session/status`, which has no params
  and returns the latest status Kit has.

The session runtime hands the host each new status without waiting; the host
delivers it from its own loop, so a slow plugin never holds up the session.
Status notifications may arrive after the turn event of the same model
response.

### Compaction

Kit sends `kit/events/compaction.started` with `{ sessionId, compactionId,
turnId }` when the session starts compacting its conversation, and
`kit/events/compaction.completed` with the same members and a
`failureReason` when it ends. `turnId` is null for a compaction outside a
turn, such as one a client asked for; `failureReason` is null when compaction
succeeded and otherwise is the session API's compaction failure reason.
Like turn events, compaction events are best effort: the host holds a small
bounded number awaiting delivery and drops the rest with a diagnostic, so a
start is not always followed by a completion. Status has no such loss,
because the latest value replaces older ones.

### Compatibility

These are optional members, notifications Kit adds, and a plugin-to-Kit
method, which ADR 0065 allows within version 2. Plugins ignore notifications
they do not know. The schema marks `status` in `initialize` optional, so a
plugin validating against it accepts an older Kit that does not send it. A
plugin that calls `kit/session/status` on such a Kit receives `-32601`.

### TypeScript helper

`kit.session.status` holds the latest status, or null when Kit sent none, and
updates before the helper delivers `session.status.changed` to listeners.
`kit.session.getStatus()` sends `kit/session/status`. The `compaction.started`
and `compaction.completed` events are delivered like the other events.

## Consequences

- Plugins can show and act on cost, context, and model without a client.
- The numbers plugins see match the debug views.
- Status traffic is bounded per instance regardless of how often usage
  changes.
- The status covers one session; a plugin that wants a total across
  subagents adds up their sessions itself.
