# Steering + Follow-up: Default Behavior Decision

**Date:** 2026-03-25
**Status:** Accepted

## Context

The agent session supports two message queueing modes while the agent is streaming:
- **Steering** — message is delivered after current tool calls finish, before the next LLM call
- **Follow-up** — message waits until the agent is completely idle (no tool calls, no steering messages)

`kit` needs to decide what happens when the user presses Enter in the composer while the agent is streaming.

## Options

### A — Enter steers (default), Alt+Enter queues follow-up
- **Enter** while streaming → `sendUserMessage(text, { deliverAs: "steer" })`
- **Alt+Enter** → `followUp(text)`
- `Alt+Up` → restore pending messages to composer

### B — Enter queues follow-up (matches Pi), Alt+Enter steers
- **Enter** while streaming → `followUp(text)`
- **Alt+Enter** → `sendUserMessage(text, { deliverAs: "steer" })`

### C — No default, explicit commands only
- No automatic routing on Enter
- Users must use `/steer` and `/followup` slash commands

## Decision

**Option A — Enter steers by default.**

Rationale:
- Steering is the most common use case while an agent is mid-turn: correcting, redirecting, asking it to pause or reconsider
- Follow-up is for follow-on work that should happen after the current task completes — rarer and more deliberate
- The keybinding `Alt+Enter` is consistent with Pi's `app.message.followUp` convention
- Option B makes the common case (correction) require a modifier key

## Implementation

- `runtime.submitUserMessage()` passes `deliverAs: "steer"` when `agentSession.isStreaming`
- `Alt+Enter` in the composer calls `runtime.sendFollowUp(text)`
- `Alt+Up` calls `runtime.clearPendingMessages()` and restores content to the composer
- Footer shows `📬N` when N pending messages are queued
