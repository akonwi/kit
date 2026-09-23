# Print mode does not exit after replying

`kit -p --no-session "Reply with exactly OK"` prints `OK` and then stays
alive indefinitely instead of exiting. Reproduces on the code before the
pi-ai 0.87 upgrade too, so it is not caused by that change. It blocks
`bun run smoke:print-mode`, which waits on the first step forever.

Seen on macOS with the author's user config (plugins, MCP servers, skills).
Next step: find what keeps the event loop alive after the run completes
(open handles from plugins, MCP connections, watchers, or timers) and make
headless mode dispose them or exit explicitly.

If a smoke run is interrupted, delete the fixtures it leaves behind before
re-running: `.kit/plugins/headless-print-mode-smoke/` and
`.kit/agents/headless-print-mode-smoke.md`.
