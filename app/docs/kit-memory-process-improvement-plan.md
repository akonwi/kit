# Kit memory and process improvement plan

## Findings

- Sub-agents are not separate OS-level `kit` processes. They run in-process as temporary `AgentRuntime`s.
- Live inspection showed multiple old `kit` processes with revoked stdin/stdout/stderr and no child processes, suggesting Kit can survive after the launching terminal/session is gone.
- Inactive sub-agents are just metadata reconstructed from the session JSONL; completed subagent runtimes are disposed after each run.
- Main session memory can still grow because the active runtime/Pi agent keeps session turns/messages in memory, and the UI may also retain/render long transcript state.
- Runtime disposal currently detaches listeners and disposes watchers, but should also abort active model/tool/subagent work before disposal.

## Goals

1. Prevent orphaned Kit processes after terminal/app close.
2. Ensure active model streams, tools, MCP connections, and subagent runs are aborted/disposed during shutdown.
3. Add user-visible subagent diagnostics via a focused `/subagents` command.
4. Add lower-level process/runtime diagnostics for future memory leak investigations.
5. Reduce memory growth for long sessions after shutdown correctness is fixed.

## 1. Shutdown hardening using OpenTUI lifecycle

OpenTUI guidance:

- Do not call `process.exit()` directly before terminal cleanup.
- Use `renderer.destroy()` as the primary shutdown path.
- Use `createCliRenderer({ onDestroy })` for cleanup hooks.
- OpenTUI supports configured `exitSignals` and registers signal handlers internally.
- If Kit uses `exitOnCtrlC: false`, custom Ctrl+C/quit handling must still route through `renderer.destroy()`.

Implementation plan:

- In `app/src/app/bootstrap.tsx`, make OpenTUI `onDestroy` the single bridge that disposes Kit app state/runtime/plugin resources.
- Capture the Solid/app disposer in a variable that `onDestroy` can call.
- Make `quitAndDestroy()` call only `renderer.destroy()`; avoid duplicating cleanup there.
- Keep the post-destroy watchdog only as a fallback after terminal cleanup has run.
- Do not include `SIGINT` in `exitSignals` for now, so Kit’s Ctrl+C keybindings can continue to clear input, close overlays, abort, or quit intentionally.

Pseudo-shape:

```ts
let disposeApp: (() => void) | null = null;

const renderer = await createCliRenderer({
  exitOnCtrlC: false,
  exitSignals: [
    "SIGTERM",
    "SIGQUIT",
    "SIGHUP",
    "SIGBREAK",
    "SIGABRT",
    "SIGPIPE",
    "SIGBUS",
    "SIGFPE",
  ],
  onDestroy: () => {
    try {
      disposeApp?.();
    } finally {
      disposeApp = null;
    }
  },
});
```

Add a stdio-loss fallback because orphaned processes had revoked stdio:

- Listen for `process.stdin` `end`, `close`, and `error`.
- Consider stdout/stderr `error` for `EIO`.
- These listeners should call `renderer.destroy()`, not `process.exit()`.
- Remove listeners during destroy if needed.

## 2. Abort active work during disposal

Update runtime cleanup so disposal is not only listener cleanup.

- In `AgentRuntime.dispose()`:
  - call `this.abort()` first
  - unsubscribe agent listener
  - dispose git watcher
  - dispose event bus
  - clear or release large retained state where safe

- Consider adding `Agent.dispose()` around the Pi agent boundary:
  - abort active generation
  - unsubscribe Pi subscription if upstream exposes an unsubscribe
  - dispose local event bus
  - clear local maps such as `toolArgsById`

- Ensure `FilePersistence` gets a chance to flush queued writes on normal shutdown if feasible.

## 3. Harden subagent cleanup

Subagent runtime behavior should be safe even if the app closes during a delegated run.

- Update `SubagentManager.reset()`:
  - for each active conversation, if `status === "running"`, call `runtime.abort("Session closed")`
  - then call `runtime.dispose()`
  - clear the active map

- Update dismiss behavior if needed:
  - running subagent: abort first, append `subagent_aborted`, then append `subagent_dismissed`
  - idle/failed/aborted subagent: append `subagent_dismissed`, dispose runtime if present, remove state

- Ensure subagent-created `AgentRuntime.dispose()` also aborts active streams after the runtime disposal change.

## 4. Add `/subagents` command

Only support these command forms:

```txt
/subagents
/subagents dismiss <name>
```

### `/subagents`

Show a table view of available sub-agents and their stats.

Suggested columns:

| Name | Status | Model | Source | Last activity | Description |
| --- | --- | --- | --- | --- | --- |
| summarizer | inactive | default | plugin | — | Summarize text or topics concisely |
| code-reviewer | running | gpt-5.5 | plugin | now | Expert code reviewer... |
| scout | idle | claude-haiku-4-5 | kit-project | 2m ago | Fast codebase reconnaissance |

Status values:

- `inactive`: available, no active conversation
- `idle`: active conversation exists, not running
- `running`
- `failed`
- `aborted`

Display rules:

- Truncate long description/model/source for terminal width.
- Include active stats from `SubagentManager.listActive()`.
- Include available definitions from discovery/plugin subagents.
- Prefer a clear table-style command response, not `/debug`.

### `/subagents dismiss <name>`

Behavior:

- If active and idle/failed/aborted: dispose/reset state and persist `subagent_dismissed`.
- If running: abort first, persist `subagent_aborted`, then `subagent_dismissed`.
- If available but inactive: show `No active conversation for <name>.`
- If unknown: show `Unknown sub-agent <name>.`

Implementation notes:

- Add `app/src/features/commands/subagents.ts`.
- Register it with the built-in command registry.
- The subagents plugin currently owns `SubagentManager`; expose a small runtime-facing registry/API so commands can access:
  - available subagent definitions
  - active subagent states
  - dismiss operation
- Avoid coupling command code directly to plugin internals.

## 5. Runtime/process diagnostics

Keep lower-level diagnostics separate from `/subagents`.

Add a debug/runtime diagnostics surface showing:

- PID
- uptime
- `process.memoryUsage()`
- current session id and cwd
- turn/message count
- active handles count when `KIT_DEBUG_SHUTDOWN=1`
- active MCP connections
- active subagent count/status summary

Optional runtime registry:

- Write heartbeat files under `~/.kit/runtime/<pid>.json` with:
  - pid
  - session id
  - cwd
  - startedAt
  - lastHeartbeatAt
- Remove the heartbeat file on clean shutdown.
- On startup, detect stale Kit runtime files and optionally warn or clean them.

## 6. Long-session memory reduction

After shutdown correctness is fixed:

- Verify auto-compaction replaces the loaded runtime turns/messages, not only persisted context.
- Add memory-based compaction trigger in addition to token/context usage.
- Avoid eager loading/rendering of entire transcript where possible.
- Consider loading only recent turns + compacted summaries into `AgentRuntime`, while keeping full JSONL available for history browsing.
- Paginate/virtualize transcript rendering for long sessions.
- For subagents, avoid scanning/rebuilding from the full main JSONL on every run if it becomes expensive; maintain compact indexed state if needed.

## 7. MCP/tool process cleanup audit

MCP stdio servers can spawn external child processes. The manager already calls `transport.close()` on dispose, but verify all shutdown paths reach it.

Checks:

```sh
ps -axo pid,ppid,rss,comm,args | grep kit
pgrep -P <kit-pid>
```

Add tests or manual verification for:

- app quit
- terminal close/SIGHUP
- running MCP tool during quit
- running subagent during quit
- failed provider/model stream during quit

## Priority order

1. OpenTUI-aligned shutdown via `onDestroy` and `renderer.destroy()`.
2. Abort-on-dispose for runtime, agent, and subagents.
3. Add `/subagents` and `/subagents dismiss <name>`.
4. Add process/runtime diagnostics and optional heartbeat registry.
5. Reduce long-session memory growth.
6. Audit MCP/tool child-process cleanup.
