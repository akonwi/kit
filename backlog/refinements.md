# Refinements Backlog

Deferred refinements that don't block shipping but should be revisited.

## Session / cwd behavior

- [ ] Define how pi-kit should support mutable cwd during a session
- [ ] Decide whether cwd changes should be persisted as custom session entries
- [ ] Decide how cwd changes affect tool execution and file resolution

## Transcript rendering

- [ ] Strikethrough for canceled/aborted turns (strike through user message + all resulting assistant/tool messages, not just the assistant message with stopReason="aborted")

## Runtime / status

- [ ] Decide what bell/speech/runtime indicators belong in the new app
