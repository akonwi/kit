# Steering & Follow-up Messages

## Status

Available in the current runtime/composer flow.

## Overview

`kit` supports queueing messages for the agent while it is actively processing a
turn.

This allows the user to either:

- **steer** the in-flight work as soon as the current tool batch completes, or
- **queue follow-up** work for after the agent becomes fully idle

## Concepts

### Steering

A steering message is delivered after the current tool-call batch finishes and
before the next LLM call.

Use it to:

- correct the agent's direction
- redirect the task
- inject a clarification before the turn continues

### Follow-up

A follow-up message is delivered only when the agent is fully idle.

Use it to:

- queue the next piece of work
- ask something that should wait until the current turn is done

## Current UX

### Keybindings

| Key | Action |
|-----|--------|
| **Enter** while streaming with composer text | queue as follow-up |
| **Enter** while streaming with empty composer and queued follow-ups | promote queued follow-ups to steering |
| **Up** in empty composer | restore queued follow-ups first, otherwise recall the last user message |

### Runtime methods

The current runtime/composer path exposes:

```ts
runtime.sendFollowUp(text: string): void
runtime.sendSteer(text: string): void
runtime.clearPendingMessages(): void
runtime.drainPendingMessages(): string[]
runtime.promotePendingFollowUpsToSteering(): void
runtime.getPendingMessageCount(): number
```

## Current behavior notes

- follow-ups are visible above the composer while queued
- when the next turn begins consuming queued follow-ups, that visible stack clears
- steering/follow-up are currently exposed through composer behavior rather than slash commands

## Related decision

See `../adrs/0012-steering-followup.md`.
