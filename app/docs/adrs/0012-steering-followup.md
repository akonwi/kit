# 0012: Steering is the default mid-turn submission mode

## Status
Superseded

The implemented composer flow now queues follow-ups with `Enter` while streaming and promotes queued follow-ups to steering when `Enter` is pressed from an empty composer. The current user-facing behavior is documented in `docs/features/steering-followup.md`.

## Context

The agent session supports two message queueing modes while the agent is streaming:

- **Steering** — a message is delivered after current tool calls finish and before the next LLM call
- **Follow-up** — a message waits until the agent is completely idle

Kit needs a clear default for what happens when the user presses Enter in the composer while the agent is streaming.

## Options

### A — Enter steers by default, Alt+Enter queues follow-up

- **Enter** while streaming → steering
- **Alt+Enter** → follow-up
- **Alt+Up** → restore pending messages to the composer

### B — Enter queues follow-up, Alt+Enter steers

- **Enter** while streaming → follow-up
- **Alt+Enter** → steering

### C — No default, explicit commands only

- no automatic routing on Enter
- users must use explicit slash commands

## Decision

Choose option A.

- Enter steers by default while streaming
- Alt+Enter queues a follow-up
- Alt+Up restores pending messages to the composer

## Rationale

- steering is the most common use case while an agent is mid-turn: correcting, redirecting, or asking it to reconsider
- follow-up is for work that should happen after the current task completes
- option B makes the common case require a modifier key

## UI behavior

- pending queued messages should be visible
- the footer/status bar shows the queued count and an edit hint when follow-ups are queued
- queued follow-ups can be edited, deleted, or cleared from the queue editor opened with `Alt+Q` or `queue-editor.open` from the command palette

## Related

- `docs/features/steering-followup.md`
