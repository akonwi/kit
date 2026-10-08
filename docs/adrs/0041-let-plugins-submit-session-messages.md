# 0041: Let plugins submit session messages

## Status

Accepted. Supersedes the "Session information without autonomous submission"
section of [ADR 0026](0026-scope-plugin-processes-and-route-plugin-ui.md).

## Context

Some plugin workflows need to drive the agent rather than only react to it.
An experiment-loop plugin starts work when the user runs its command, then
continues it after each settled turn until the plugin decides to stop.
Plugin processes are owned by one session and one instance generation
(ADR 0026), and plugins already receive turn start and completion events.

A plugin message is not user speech. It must stay attributable to the plugin
in durable history, in model context, and in client presentation. It must also
respect session ownership, admission serialization, and normal tool policy.

The agent core already represents non-user agent-domain input as durable
context boundaries with a kind, source, receipt identity, and an idempotent
context-only turn admission. Subagent results and peer queries use this path.

## Decision

### Plugin-submitted messages are context boundaries

A plugin submits a message to its owning session with the plugin-to-Kit request
`kit/session/submit-message`. Kit records the message as a context boundary
with kind `plugin_message` and the plugin ID as its source, and starts a
context-only turn that consumes it. The message is never recorded as a user
message.

The model receives the message framed with its plugin source. The boundary's
details record the version, plugin ID, and submitted text so clients can present
the message without parsing model-facing framing.

### Request contract

Parameters are an object with:

- `text`: required, non-blank, valid UTF-8 without NUL, at most 128 KiB, the
  same as the user prompt text limit.
- `idempotencyKey`: optional, 1–128 bytes of ASCII letters, digits, `.`, `_`,
  `:`, or `-`.

Unknown parameters are rejected. The request has no session parameter. The
target is always the session that owns the calling instance. A plugin cannot
address another session.

A successful result is `{ "messageId": string, "turnId": string }`. The result
is returned after the turn is admitted, not after it finishes. Plugins correlate
`turnId` with the existing `kit/events/agent.turn.started` and
`kit/events/agent.turn.completed` events. These events may arrive before or
after the response.

Text-only content is supported. Attachments, images, prompt commands, and draft
annotations are not accepted from plugins.

### Idle-only atomic admission

Kit admits a plugin message only when the session can start a turn
immediately. Recording the boundary and starting its turn are one atomic
operation. Either both happen, or nothing is recorded.

The request fails with a busy error (`-32006`) and leaves no record when:

- a turn is active, including one awaiting a user interaction;
- user follow-ups are queued;
- another turn admission, context operation such as compaction, or runtime
  transition such as reload, cwd change, or deletion is in progress.

Queued user follow-ups therefore take precedence over plugin messages. Kit
does not queue, retry, or later deliver a rejected plugin message. The plugin
decides whether and when to try again, typically after
`kit/events/agent.turn.completed`.

Any context boundaries already pending when a plugin message is admitted, such
as subagent results, are consumed by the same turn.

Plugin messages submitted while a plugin handles a user-invoked command use the
same admission as any other plugin message.

### Ownership and generations

Only the current ready generation of a plugin instance may submit. Requests
from a revoked, stopped, failed, or superseded generation fail with the plugin
generation unavailable error (`-32002`) and admit nothing.

An admitted turn belongs to the session, not to the plugin instance. Reloading,
crashing, or stopping the plugin after admission does not cancel or retarget
the turn. Cancelling the request before admission prevents admission;
cancelling it after admission has no effect on the turn.

### Idempotency and retries

Without an idempotency key, each request is a new submission. With a key, Kit
derives a durable receipt from the plugin ID and key in the owning session.
Repeating the key with the same text returns the original `messageId` and
`turnId`, including after plugin reload or daemon restart. Repeating a key with
different text fails with a conflict error (`-32003`). A busy rejection records
no receipt, so a retry with the same key can be admitted later.

### Turn execution

A plugin-started turn runs with the session's current model, tools, prompt
contributions, interceptors, approvals, and tool policy. It may raise user
interactions, which follow the shared session interaction broker. Admission
does not require an attached client. Any client may abort the turn as it would
abort a user-started turn.

Kit does not limit how many plugin-started turns follow one another
([ADR 0040](0040-do-not-limit-autonomous-turn-chains.md)). A plugin that loops
owns its stopping policy.

### Restart and recovery

Kit keeps no plugin submission state outside the admitted boundary and turn.
After a daemon restart, an admitted turn follows normal interrupted-turn
recovery. Kit does not resubmit plugin messages, and nothing remains pending
for later delivery.

### Presentation

Plugin messages are user-visible transcript rows, distinct from user messages
and labelled with the submitting plugin. Clients do not hide them as they hide
agent-to-agent context boundaries. The transcript contract carries them as
`context` messages with `boundaryKind` `plugin_message` and `boundarySource`
set to the plugin ID.

## Consequences

- Plugins can start and continue agent work in their own session without
  impersonating the user.
- Plugins must handle busy rejection and choose their own retry and stopping
  policy; Kit offers no plugin message queue.
- User input, including queued follow-ups, is never displaced or delayed by a
  plugin message.
- A plugin can consume provider tokens and perform tool side effects without
  further user input, within the session's normal tool policy. Opening a
  project with plugin manifests already authorizes this trusted local code.
- A passive channel that informs a session without starting a turn remains a
  separate capability.
