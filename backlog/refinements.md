# Refinements Backlog

Deferred refinements that don't block shipping but should be revisited.

## Session / cwd behavior

- [x] Define how pi-kit should support mutable cwd during a session (deferred — see docs/decisions/mutable-cwd.md)

## Transcript rendering

- [ ] Strikethrough for canceled/aborted turns (strike through user message + all resulting assistant/tool messages, not just the assistant message with stopReason="aborted")

## Runtime / status

- [x] Decide what bell/speech/runtime indicators belong in the new app

## Features to port

- [ ] Auto session naming — generate a short title after agent turns (currently done by pi-kit extension via `pi -p`)
- [ ] Subagent tool — parallel/chained task delegation via child processes (currently registered by subagent extension)
