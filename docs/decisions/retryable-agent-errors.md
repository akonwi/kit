# Retryable Agent Errors

- Status: Accepted
- Date: 2026-04-08

## Decision

Kit should handle transient agent/provider failures using a Pi-aligned runtime retry
flow rather than surfacing the first retryable error immediately.

## Behavior

When an assistant turn ends with an error, Kit should:

1. inspect the terminal assistant error message
2. classify it as one of:
   - context overflow
   - transient retryable error
   - non-retryable error
3. recover accordingly

### Context overflow

Context overflow errors should **not** enter the transient retry path.

Instead, Kit should treat them as an auto-compaction recovery case:

1. remove the terminal assistant error from live agent context
2. compact the session
3. retry with `Agent.continue()`

This keeps overflow handling aligned with Kit's compaction model and avoids
wasting retry attempts on a request that cannot succeed without reducing
context.

### Transient retryable errors

Transient errors should use exponential backoff and retry with
`Agent.continue()`.

Default retry settings mirror Pi:

- enabled: `true`
- max retries: `3`
- base delay: `2000ms`
- max provider-requested delay: `60000ms`

Default backoff schedule:

- attempt 1: `2s`
- attempt 2: `4s`
- attempt 3: `8s`

## Why `continue()`

Retries must use `Agent.continue()` rather than resending the original user
message.

This preserves the exact live context state and avoids duplicating user input in
agent context.

## UI/runtime expectations

Retry and overflow recovery should be visible in Kit's runtime panel state.

The original prompt submission should remain pending until the retry or recovery
flow finishes.

## Scope for now

Kit should implement the runtime behavior and persisted settings first.

A richer settings UI for retry tuning and explicit retry lifecycle events can be
added later if needed.
