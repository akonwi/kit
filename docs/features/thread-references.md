# Thread References

## Status

Foundation exists, but the inline `@@` reference UX is not fully wired in the
current minimum loop.

## Goal

Allow users to reference other sessions/threads from the composer via an `@@`
trigger.

## Current foundation

The codebase already includes:

- a thread/session index
- thread reference expansion logic
- session invalidation hooks in app state

Relevant modules:

- `src/features/threads/thread-index.ts`
- `src/features/threads/expand-references.ts`
- `src/features/threads/index.ts`

## Intended UX

1. User types `@@` in the composer
2. A filterable picker opens with matching sessions
3. Selecting a thread inserts a thread token/reference
4. On submit, the token is expanded into a formatted thread-context block for
   the agent

## Current caveat

Like file references, this is not yet fully reconnected to the rebuilt composer
flow. The indexing/expansion pieces exist, but the end-to-end inline trigger UX
still needs to be restored.

## Source

`src/features/threads/`
