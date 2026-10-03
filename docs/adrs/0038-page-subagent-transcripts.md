# 0038: Page subagent transcripts

## Status

Accepted

## Context

A child conversation can outgrow one synchronization response. The parent
transcript already solves that with a bounded complete-turn page and an
exclusive durable cursor. A child transcript needs the same query shape so a
client can load older history without importing daemon internals or receiving
an unbounded snapshot.

## Decision

`GET /v1/sessions/{sessionID}/subagents/{conversationID}/transcript` returns one
complete-turn page, using the parent transcript policy: at most 50 messages
while retaining at least the newest four complete turns that fit the response.
Omitting `before` selects the newest page. `before` is an exclusive durable
sequence and must identify an existing complete-turn boundary. An unavailable
cursor is HTTP 409 `transcript_cursor_unavailable`, distinct from other child
conflicts. A full child queue remains HTTP 429 `capacity_exceeded`.

The page keeps durable message sequences. When older history remains it sets
`hasMoreMessages` and `previousMessageCursor` to the sequence of its oldest
included message. The next request is the same operation with
`before={previousMessageCursor}`. Messages are chronological so a client can
prepend them. A message that cannot fit in one response is an error. A client
that has already loaded older pages merges a newest-page refresh onto that
history instead of replacing it.

## Consequences

Opening a child conversation transfers a bounded page. Clients that want older
history request the preceding page and prepend it without renumbering
sequences. A cursor that no longer names a turn boundary must be discarded and
the newest page loaded again.
