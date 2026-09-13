# 0013: Bound initial transcript snapshots

## Status

Accepted

## Context

Session snapshots previously transferred the complete parent transcript whenever a
client attached or resynchronized. Rendering can be virtualized, but an unbounded
snapshot still makes server projection, transport, decoding, and client memory grow
with the lifetime of a session.

Transcript messages belonging to one turn must remain together. Splitting a turn can
separate tool calls from their results and produce a presentation that cannot satisfy
the session protocol's ordering invariants.

## Decision

An authoritative session snapshot contains a recent complete-turn tail rather than
the entire transcript. The server targets at most 50 messages while retaining at
least the newest four complete turns. The 50-message target is soft: when those four
turns contain more than 50 messages, all four turns are included. The existing
active-run coherent-cut rule takes precedence: a replayable active turn is omitted
and reconstructed from events, while an unreplayable active tail remains in the
snapshot even though that turn is not yet complete.

When older history exists, the snapshot sets `hasMoreMessages` and a
`previousMessageCursor` containing the durable sequence of its oldest included
message. A client retrieves the preceding complete-turn page with:

```text
GET /v1/sessions/{sessionID}/messages?before={previousMessageCursor}
```

The `before` cursor is exclusive. Pages are requested from newest to oldest, use the
same 50-message/four-turn policy, and return messages in chronological ascending
order so clients can prepend them directly. `hasMoreMessages` means at least one
older eligible message exists, and the next cursor is again the durable sequence of
the oldest included message. Durable record sequences remain stable across appends. An unavailable cursor returns
HTTP conflict; the client then obtains a fresh snapshot.

The local session protocol version is bumped because snapshot shape and initial
history semantics change. Pagination is an optional renderer-neutral bound-session
capability so clients do not import daemon internals.

## Consequences

Initial transfer and client transcript state are bounded in the common case. A small
number of exceptionally tool-heavy turns may exceed the target by design. Clients
must prepend older pages without disturbing the visible scroll anchor and must merge
new snapshots with already-loaded history when appropriate. The server reads durable
history backward and may scan one additional complete turn to prove the page boundary.
