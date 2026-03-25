# Refinements Backlog

Deferred refinements that don't block shipping but should be revisited.

## Session / cwd behavior

- [x] Define how pi-kit should support mutable cwd during a session (deferred — see docs/decisions/mutable-cwd.md)

## Transcript rendering

- [x] Strikethrough for canceled/aborted turns (turn-based grouping: user message + all resulting assistant/tool messages struck through together)

## Runtime / status

- [x] Decide what bell/speech/runtime indicators belong in the new app

## Features to port

- [x] Auto session naming — generate a short title after agent turns (ported in-process via current model)
- [x] Subagent tool — parallel/chained task delegation via in-process AgentSession instances
