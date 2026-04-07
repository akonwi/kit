# Handoff

## Status

Planned / partially explored, not currently active in the minimum loop.

## Goal

Allow the current session to hand off context into a fresh session with a
compact summary of what matters.

## Current foundation

There is existing command-side handoff code in:

- `src/features/commands/handoff.ts`

That code reflects the intended workflow direction, but it is not currently part
of the active command set.

## Intended behavior

1. User initiates handoff
2. The app generates a compact summary of the current work
3. A new session is created with that summary as its starting context
4. The user continues in the new session while preserving the parent session as
   its own artifact

## Why this is useful

- continue work in a fresh context window
- branch into a new direction without losing prior work
- preserve important context in a tighter form

## Current caveat

Handoff needs to be reintroduced deliberately on top of the new standalone
session/runtime model rather than assuming earlier Pi-era or transition-era
behavior.
