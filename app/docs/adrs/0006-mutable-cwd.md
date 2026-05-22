# 0006: Mutable cwd remains deferred

## Status
Deferred

## Context

Kit sessions currently store a `cwd`, and runtime tools are created against that session cwd.

Changing the working directory mid-session is not part of the core app model.

The current runtime and session architecture assumes:

- a session has one persisted `cwd`
- tools are created for that session context
- session lookup and persistence are keyed to the session's own stored metadata

## Decision

Do not support mutable cwd yet.

For now:

- a session's `cwd` is stable for the life of that session
- changing directories should be modeled as creating or switching sessions instead of mutating the active one in place

## Why this is deferred

Supporting mutable cwd correctly would require a clear policy for all of the following:

1. **Persistence**
   - should the session's persisted `cwd` change?
   - should cwd changes be recorded in session history?

2. **Runtime and tool rebuilding**
   - changing cwd may require rebuilding tools and refreshing dependent state

3. **Shell and UI semantics**
   - transcript references, file references, footer state, and palette behavior all need a clear answer for which cwd they should use

4. **Session UX**
   - if cwd changes substantially, should that still be the same session or a new one?

## Revisit when

Revisit this only after the current runtime and core UX layers are stable.

## Related

- `docs/adrs/0003-custom-shell.md`
- `docs/features/thread-references.md`
