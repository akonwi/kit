# 0040: Do not limit autonomous turn chains

## Status

Accepted. Amends [ADR 0001](0001-native-go-architecture.md),
[ADR 0014](0014-route-durable-peer-session-queries.md), and
[ADR 0030](0030-route-explicit-subagent-requests-and-replies.md).

## Context

A session can start a turn without new user input when a context boundary
arrives while it is idle. Subagent results, subagent request replies, peer
queries, peer results, and plugin-submitted messages
([ADR 0041](0041-let-plugins-submit-session-messages.md)) all admit
context-only turns this way. Long-running autonomous workflows, such as
experiment loops, legitimately chain many of these turns in succession.

A count of consecutive non-user turns does not identify runaway behavior. It
stops legitimate work at an arbitrary point, forces every boundary source to
handle a limit-reached state, and leaves undelivered boundaries pending until
unrelated user input arrives. Some sources would need to be exempt from such a
count, adding to its complexity.

## Decision

Kit does not limit the number of consecutive autonomous turns in a
conversation. Admitting a context-only turn from pending boundaries does not
consult or update a reaction counter, and there is no limit-reached error or
deferred-limit state. Every boundary source uses the same reaction admission.

Autonomous work remains bounded by resource and routing limits rather than turn
counts:

- per-session admission serialization: at most one active turn per conversation;
- daemon-wide concurrent reaction slots and bounded mailbox workers;
- bounded pending boundaries, queues, message sizes, and delivery batches;
- peer-query cycle rejection, thread depth, and cross-session hop count;
- subagent supervisor queue and execution limits.

The user remains in control of autonomous work. Any autonomous turn can be
aborted like a user-started turn. The source of each autonomous turn is visible
in the transcript and session events. Sources that need a stopping policy, such
as an autoresearch plugin, own that policy.

## Consequences

- Autonomous workflows can run indefinitely until they finish, fail, or the
  user stops them.
- Boundary sources do not handle a limit-reached state, and pending boundaries
  are not stranded by a turn-count limit.
- A misbehaving model, subagent pair, or plugin can consume provider tokens
  until the user notices and aborts it. Resource bounds limit concurrency, not
  the total amount of sequential work.
