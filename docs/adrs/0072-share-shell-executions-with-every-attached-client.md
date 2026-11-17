# 0072: Share shell executions with every attached client

## Status

Accepted. Extends [ADR 0052](0052-tell-every-attached-client-about-session-changes.md)
and the session event vocabulary of
[ADR 0033](0033-adopt-consistent-protocol-event-vocabulary.md).

## Context

A composer shell command (`!command`, or `!!command` to keep its result out of
the model's context) runs on the server, in the session's working directory.
A session runs one command at a time, and each command gets the session's next
execution sequence. When the command settles, the server stores it, including
its output, in the session's durable shell history, which lasts until the
session is deleted. A temporary session keeps its 64 most recent commands in
memory instead. An included command then becomes a `bash` boundary in the
conversation. An excluded command never reaches the model.

Several clients may be attached to one session. The client that starts a
command learns how it ends by asking for it. Every other client has to
discover the command, and a command that starts and settles between two looks
must not go unnoticed. An excluded command is no less the user's work for
being kept from the model, so other clients have to see it too, along with its
output, without that output entering the model's context.

The session's live event stream already reaches every attached client in
order, resumes from a cursor, and tells a client that has fallen behind its
retained events to resynchronize from a snapshot. The session event union is
closed for a protocol version (ADR 0032), and clients decode snapshots
strictly.

## Decision

### Two session events

The live session stream carries every direct shell command, whichever client
started it and whether or not it is excluded from context:

| Change | Event |
|---|---|
| A command starts | `bash.started` |
| A command settles | `bash.completed` |

Both events are session-scoped: they have no turn, because a command runs
beside turns rather than inside one. Each carries `bashExecution`, the command
as `getBash` returns it. In `bash.started` the command is `running` and has no
output. In `bash.completed` it has its terminal status (`completed`, `failed`,
`aborted`, or `interrupted`), its exit code, its output (at most 30,000 bytes,
with the truncation and timeout flags), and any error message. A command is at
most 64 KiB, so both events fit the 128 KiB event payload bound.

The server publishes `bash.started` when it admits the command, before the
start request answers, and `bash.completed` in the step that ends the command:
after the command is stored and, for an included command, after its boundary
is created. When `bash.completed` arrives, the session is no longer busy with
the command, so a client can start the next one. A repeated start request for
the same execution ID publishes nothing. Because a session runs one command at
a time, the stream carries a command's `bash.completed` before the next
command's `bash.started`, and executions settle in sequence order.

An included command appears twice, by the same ID: as `boundary.created`, which
records what the model was given, and as `bash.completed`. A client shows one
row per execution ID.

### Snapshots

`activeBashExecutionId` still names the running command. A new snapshot member,
`bashExecutions`, lists the commands the transcript doesn't record: the running
command, excluded commands, and included commands that settled without joining
the conversation, such as one interrupted by a server shutdown. It lists those
that started no earlier than the oldest message the snapshot includes, or all
of them when the snapshot includes the whole transcript, keeping only the 32
most recent. They are in sequence order and carry their output.

Persisted sessions read the list from durable shell history, and temporary
sessions read it from memory.

### Reconnecting and recovering from gaps

Shell events are ordinary session events. A client resumes the stream from
its cursor (`streamId:sequence`) as it does for any other event. The session
retains its most recent events, up to 4,096 events or 8 MiB, whichever is
reached first. A client whose cursor has fallen out of that window, or whose
stream was replaced, receives `session.resync` and takes a fresh snapshot,
which carries the running command and the recent excluded ones. There is no
separate shell cursor and no polling.

Commands older than the snapshot's list stay in durable shell history. Clients
recall them through `getBashHistory`, as they do now, and can read any of them
by ID with `getBash`.

### Retention

- Durable shell history keeps every settled command of a persisted session,
  with its output, until the session is deleted. Temporary sessions keep their
  64 most recent commands in memory until the session is deleted or the
  server stops.
- The live stream retains shell events like every other event, within its
  4,096-event and 8 MiB bound.
- Snapshots list at most 32 commands.

### Model context

Excluded output reaches clients only. The server gives the model a command
only as the `bash` boundary of an included command. Session events and
snapshots are client projections: the model never reads them, and plugins
receive only the status the server derives from events (ADR 0066), which shell
events don't change. The Agent Client Protocol bridge doesn't forward shell
events.

### Clients

The macOS, iOS, browser, and terminal clients show every command in the
session, from any client, with the same row they use for their own: a spinner
while it runs, then its output and outcome, and the not-in-context mark for an
excluded command. A command from the live stream goes where the stream reaches
it: after the transcript row that was last when it started. A command from a
snapshot goes after the last message created before it started.

Clients follow commands, including their own, through these events instead of
polling `getBash`. A client still uses `getBash` to learn whether a start
request whose answer was lost started the command. The running command keeps
the session busy in every client. The composer's Stop control stops it, in
whichever client started it. That includes iOS, which shows other clients'
commands and can stop them but doesn't start commands.

### Protocol

The new event kinds and the snapshot member are breaking changes to a closed
union and a strictly decoded response, so the session protocol becomes 51.

## Consequences

- Every attached client sees every shell command as it starts and as it ends,
  including commands kept from the model, and none can miss a short one.
- Excluded commands now survive a client reload within the snapshot's recent
  window.
- Clients stop polling for their own commands.
- Protocol 51 means clients must be updated with the server. Released
  protocol 50 clients can't attach to it.
- The live stream's retained window holds shell output as well as turn
  events, so a burst of large commands can push older events out of replay
  sooner.
- Output isn't streamed while a command runs. Every client, including the one
  that started it, sees the output when it settles.
