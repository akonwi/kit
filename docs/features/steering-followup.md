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
| **Enter** while streaming | queue as steering message |
| **Alt+Enter** | queue as follow-up message |
| **Alt+Up** | clear pending messages / restore that state path |

### Runtime methods

The current runtime/composer path exposes:

```ts
runtime.sendFollowUp(text: string): void
runtime.sendSteer(text: string): void
runtime.clearPendingMessages(): void
runtime.getPendingMessageCount(): number
```

## Current caveat

The underlying decision remains stable, but some earlier docs described richer
pending-message inspection/restore APIs than the current runtime exposes. The
minimum working loop currently keeps the behavior simpler.

## Related decision

See `../decisions/steering-followup.md`.
