# Plugin session messages

A plugin starts a turn in its owning session with the plugin-to-Kit request
`kit/session/submit-message`, as specified by
[ADR 0041](../adrs/0041-let-plugins-submit-session-messages.md):

```json
{
  "jsonrpc": "2.0",
  "id": "plugin-51",
  "method": "kit/session/submit-message",
  "params": { "text": "Continue the experiment loop.", "idempotencyKey": "experiment-4" }
}
```

`text` is required: non-blank UTF-8 without NUL, at most **128 KiB**.
`idempotencyKey` is optional: 1–128 ASCII letters, digits, `.`, `_`, `:`, or
`-`. Encoded params are bounded to 1 MiB. Unknown parameters are rejected; in
particular there is no `sessionId`, because the target is always the session
that owns the calling instance. On admission the result is:

```json
{ "messageId": "pluginmsg_…", "turnId": "turn_…" }
```

The response is returned once the turn is admitted, not when it finishes.
Correlate `turnId` with the turn lifecycle notifications, which may arrive
before or after the response. The completion projection contains user and
assistant text only, so it does not repeat the plugin's own message.

Kit admits a message only when the session can start a turn immediately.
Recording the message and starting its turn are atomic: a rejected request
records nothing, and Kit never queues or later delivers it. A turn that has
finished but is still settling, for example while its completion notification
is delivered, does not count as running, so submitting from an
`agent.turn.completed` handler is admitted unless the user has queued input.

| Code | Meaning |
| --- | --- |
| `-32006` | Session is busy: a turn is running (including one awaiting a dialog answer), user follow-ups are queued, or another admission, context operation, or runtime transition is in progress. `data.reason` is `session_busy`. Retry later; Kit does not. |
| `-32003` | The idempotency key was already admitted with different text. |
| `-32002` | The calling generation was revoked, stopped, or replaced. Nothing was admitted. |
| `-32602` | Invalid or unknown parameters. |
| `-32001` | The request was cancelled before admission. |
| `-32603` | Admission failed for another reason; details are in the server log. |

Without a key each request is a new message. With a key, repeating the same
text returns the original `messageId` and `turnId`, including while the session
is busy and after plugin reload or daemon restart. A busy rejection records no
key, so the same key can be admitted later.

The message is recorded as a `context` transcript message with `boundaryKind`
`plugin_message`, `boundarySource` set to the plugin ID, the submitted text as
its content, and details `{"version":1,"pluginId":"…"}`. The model receives it
framed as `[plugin_message from <plugin-id>]`; it is never recorded as user
speech. When the turn starts, the session event stream carries a
`plugin.message.added` event with the turn ID, `pluginId`, and text, so clients
show the message while the turn runs and not only after the transcript
refreshes.

Both clients present the message as one muted row naming the plugin, followed
by the message's first line. In the TUI it reads `◆ <plugin-id> · <first line> ▸`
in the plugin identity colour; the macOS app uses the plugin symbol and the
styling of a tool group. Clicking the row shows the full message beneath a left
rule, and the choice is kept for the turn when the live row gives way to the
recorded one. Plugin messages are not offered by prompt history recall, which
only recalls user prompts.

An admitted turn belongs to the session, not the plugin. It uses the session's
normal tools, interceptors, and approvals, runs with or without attached
clients, and can be aborted like any turn. Reloading or stopping the plugin
after admission does not cancel it, and Kit does not limit how many plugin
turns follow one another; a plugin that loops owns its stopping policy.

This differs from the v1 contract, which takes a `sessionId`, returns null, and
queues behind a running turn. The
[`session-message-demo`](../../.kit/plugins/session-message-demo/) fixture
demonstrates kickoff from a command, continuation after completion, keyed
retries, and busy rejection, and runs in the daemon subprocess tests.
