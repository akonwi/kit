# Decision: Mutable CWD remains deferred

**Date:** 2026-03-17
**Updated:** 2026-04-07
**Status:** Deferred

## Context

`kit` sessions currently store a `cwd` and runtime tools are created against that
session cwd.

Today, changing the working directory mid-session is not part of the core app
model.

The current runtime/session architecture assumes:

- a session has one persisted `cwd`
- built-in tools are created for that session context
- session lookup and persistence are independent from Pi, but still keyed to the
  session's own stored metadata

## Why this is still deferred

Supporting mutable cwd correctly would require a clear policy for all of the
following:

1. **Persistence**
   - should the session's persisted `cwd` change?
   - should cwd changes be recorded as events in session history?

2. **Runtime/tool rebuilding**
   - tools like `bash`, `read`, `write`, `edit`, `grep`, and `find` are created
     relative to the session context
   - changing cwd may require rebuilding tool instances and refreshing dependent
     indexes

3. **Shell/UI semantics**
   - transcript references, file references, footer state, and palette behavior
     all need a clear answer for which cwd they should use

4. **Session UX**
   - if cwd changes substantially, should that still be the same session or a
     fork/new session?

## Decision

Do not support mutable cwd yet.

For now:

- a session's `cwd` is stable for the life of that session
- changing directories should be modeled as creating/switching sessions instead
  of mutating the active one in place

## Revisit when

Revisit this only after the rebuilt runtime, pager, threads, handoff, and other
core UX layers are stable on the current architecture.
