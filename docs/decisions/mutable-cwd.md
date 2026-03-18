# Mutable CWD: Exploration and Deferral

**Date:** 2026-03-17

## Context

Pi's `SessionManager` treats `cwd` as immutable — set once at session creation, stored in the session header, and used to derive the session directory path. All tools (bash, edit, read, write, grep, find) capture cwd in closures at creation time via `createAllTools(cwd, ...)`.

We explored whether pi-kit could support changing the working directory mid-session.

## What we explored

### Approach 1: Fork session into new directory

Use `SessionManager.forkFrom(sourcePath, newCwd)` to copy the full session history into a new session in the new directory, then create a new `AgentSession` with the forked session manager.

- **Pros:** Clean — new session, new tools, full history preserved
- **Cons:** Requires recreating the entire agent session. The old session file would need to be deleted to avoid confusion. Complex lifecycle management.

### Approach 2: Mutate private fields + reload

Cast `agentSession` to `any` and mutate `_cwd`, plus mutate `sessionManager.cwd`, then call `agentSession.reload()` which internally calls `_buildRuntime()` — recreating all tools with the new cwd.

```typescript
(agentSession as any)._cwd = newCwd;
(agentSession.sessionManager as any).cwd = newCwd;
process.chdir(newCwd);
await agentSession.reload();
```

- **Pros:** Simple, tools get recreated with new cwd, conversation context preserved
- **Cons:** Relies on private implementation details. The session file stays in the original directory (derived from the original cwd), so quitting and reopening from the new directory finds a different session. Session persistence doesn't follow the directory change.

### Prototype results

We implemented Approach 2 as a `/cd` command. It worked — tools resolved paths relative to the new cwd, the status bar updated, and the agent operated in the new directory.

However, session persistence broke: the session file remained in the original cwd's session directory, so reopening from the new directory didn't find the same session.

## Decision

Defer mutable cwd. The session persistence problem is fundamental — `SessionManager` physically stores sessions under `~/.pi/agent/sessions/<encoded-cwd>/`, and changing cwd mid-session doesn't move the session file. Solving this properly would require either:

1. Upstream changes to `SessionManager` to support cwd migration
2. A session file move/copy mechanism
3. Decoupling session storage from cwd entirely

None of these are worth the complexity right now.

## What we kept

The prototype yielded useful infrastructure that we kept:

- `runtime.refreshStatus()` — forces a status bar update
- `runtime.emitError(title, lines)` — emits errors to the transcript
- `AppError` type and error rendering in the transcript (red left border)
- `applyRuntimeStatus` now refreshes `cwd` from `process.cwd()`
