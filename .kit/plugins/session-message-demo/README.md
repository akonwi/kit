# Session message demo

Run Kit from this worktree and use `/reload` to discover the plugin. Each
submission outcome appears as a toast.

- `/session-message-demo.send <text>` — submits the text as a session message,
  starting a turn when the session is idle.
- `/session-message-demo.send-keyed <text>` — submits with a fixed idempotency
  key. Repeating the same text returns the original admission; different text
  is rejected as a conflict.
- `/session-message-demo.loop <count>` — submits a message, then another after
  each turn it started completes, until `count` turns have run.

Requires Python 3. Submitting while a turn is running is rejected as busy.
