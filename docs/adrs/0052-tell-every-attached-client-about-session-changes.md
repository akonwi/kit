# 0052: Tell every attached client about session changes

## Status

Accepted.

## Context

Several clients may be attached to one session at once: a terminal client, the
macOS app, and others on other machines. The Go server is authoritative for the
session, and each client reconciles a snapshot with the session's live event
stream ([ADR 0011](0011-use-sse-for-session-events.md),
[ADR 0036](0036-publish-turns-events-and-transcript-contract.md)).

A change one client makes must reach the others while it matters, not only at
their next snapshot. Some session state has no other path to them:

- The follow-up queue. A prompt submitted while a turn runs is queued; any
  client may restore the queue to its composer or promote it into the running
  turn, and the server starts the first queued prompt as each turn ends.
- The model configuration. A thinking-level change keeps the session's runtime
  and its event stream.
- Compaction a client requests, and compaction a model change needs. These run
  while the session is idle, outside any turn, so no turn stream carries their
  progress.

A queued prompt can also fail to start when its turn comes: an attachment it
names may be gone, or the session's store may refuse the write. While prompts
are queued on an idle session the server starts nothing else, so a queue that
cannot start blocks the session.

## Decision

### The live stream carries session changes

The session's live stream carries session-scoped events, with no turn, for
every change a client can make to shared session state:

| Change | Event |
|---|---|
| Name | `session.name.changed` |
| Working directory | `session.cwd.changed` |
| Model, thinking level, and configuration revision | `session.configuration.changed` |
| Follow-up queue | `follow_ups.changed` |
| Compaction outside a turn | `compaction.started`, `compaction.completed` |

`session.configuration.changed` carries the applied configuration: the model,
the thinking level, the inputs the model accepts, and the configuration
revision. A client keeps the configuration with the highest revision it has
seen. A model change replaces the session's runtime and its event stream; the
replacement stream starts with the applied configuration.

`follow_ups.changed` carries the whole queue in the shape snapshots and queue
operations return it. The server publishes it in the same step as the change,
under the session's runtime lock, for a prompt queued, restored, promoted, or
started, and for a queue that becomes held. A queued prompt leaves the queue in
the step that starts its turn: the turn's stream carries the queue without it
after the turn's `turn.started` and `user.message.added` events, and no
snapshot shows the prompt both queued and running.

`compaction.started` and `compaction.completed` without a turn report a
compaction a client requested, or one a model change needed, including a
failed one, whose `compaction.completed` carries its failure reason
([ADR 0053](0053-classify-compaction-failures.md)). A repeated
request for a compaction that already succeeded returns its result without
compacting again and is not announced again. A snapshot waits for such a
compaction to end, so it never shows one in progress.

Queue previews, like every text these events carry, are one line of
renderer-safe text: control and format characters become spaces. The server
checks each event against the rules clients apply before it enters the
stream, so an event clients would refuse invalidates the stream instead.

### A queue that cannot start is held

The server starts the first queued prompt as each turn ends, including a turn
resumed when the session loads. If another holder can start a turn, the
prompt stays queued: a turn that holder starts starts the prompt when it ends,
and a holder that starts none, as a subagent mailbox or peer query deciding
whether to start one, starts the prompt when it lets go. A prompt whose turn
cannot launch goes back first in the queue.

When the prompt cannot start for any other reason, the queue is held: the
queue record's `admissionFailure` says why, `follow_ups.changed` announces it,
and every snapshot carries it while the queue is held. The prompt stays first in
the queue and keeps its annotations. New prompts are refused as they are while
any prompt is queued on an idle session; the session's error names the reason.
A client unblocks the session by restoring the queue, which returns the
prompts to its composer to edit, resend, or discard. A later turn's end, such
as an autonomous one, retries the held prompt, and starting it clears the
failure.

A turn that ends with a provider policy stop still leaves its queued prompts
queued without starting them and without an admission failure.

### Clients

Clients apply these events to the attached session as they arrive and do not
poll for them. The queue a client shows comes from the stream and snapshots
only: a queue operation's response may arrive after the stream has announced
a newer queue, so a client does not show the queue the response carries. They
show a held queue's reason with the queue and offer restoring it, and explain
a new prompt refused behind a queued one by pointing to the queue. A
compaction outside a turn is session work that clients show in progress; the
client that requested it, or the model change that needed it, reports how it
ended.

## Consequences

- A second client sees queue, configuration, name, working-directory, and
  compaction changes when they happen, and the macOS app no longer polls
  snapshots for the queue.
- A queue that cannot start is visible to every client, with its reason,
  instead of silently blocking the session.
- Session protocol 50 adds the two event kinds, the queue's
  `admissionFailure`, and turn-less compaction events. Event kinds are a
  closed union that clients decode strictly, so this is a breaking protocol
  change.
- Reloading a session's configuration and prompt sources is not announced;
  other clients see it at their next snapshot.
