# 0008: Retryable agent errors

## Status
Accepted

## Context

Kit needs a consistent runtime policy for transient agent and provider failures.

Not all assistant-turn errors should be handled the same way.

## Decision

When an assistant turn ends with an error, Kit classifies the terminal assistant error as one of:

- context overflow
- transient retryable error
- non-retryable error

Kit then applies the corresponding recovery behavior.

## Behavior

### Context overflow

Context overflow errors do not enter the transient retry path.

Instead, Kit treats them as an auto-compaction recovery case:

1. remove the terminal assistant error from live agent context
2. compact the session
3. retry with `Agent.continue()`

### Transient retryable errors

Transient errors use exponential backoff and retry with `Agent.continue()`.

Default retry settings:

- enabled: `true`
- max retries: `3`
- base delay: `2000ms`
- max provider-requested delay: `60000ms`

Default backoff schedule:

- attempt 1: `2s`
- attempt 2: `4s`
- attempt 3: `8s`

### Non-retryable errors

Non-retryable errors are surfaced without entering retry recovery.

## Why `continue()`

Retries use `Agent.continue()` rather than resending the original user message.

This preserves the live context state and avoids duplicating user input in agent context.

## UI and runtime expectations

Retry and overflow recovery should be visible in Kit's runtime panel state.

The original prompt submission remains pending until the retry or recovery flow finishes.

## Related

- `docs/adrs/0007-assistant-message-streaming.md`
- `docs/adrs/0009-compaction-strategy.md`
