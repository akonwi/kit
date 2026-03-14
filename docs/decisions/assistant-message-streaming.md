# Decision: Do not stream assistant message text

- Status: Accepted
- Date: 2026-03-14
- Scope: runtime loop, transcript rendering, in-flight turn UX

## Summary

pi-kit should **not stream assistant message text into the transcript** while the model is generating it.

Instead:

- thinking activity may be shown live
- tool calls may be shown live
- tool-running / tool-progress state may be shown live
- tool results may appear when complete
- the final assistant message should appear **atomically once complete**

## Why

Streaming assistant text makes the main transcript harder to read:

- the user starts reading before the message is stable
- the text continues to grow and can move out of view
- the distinction between partial and final output is visually confusing
- the transcript becomes less like a durable conversation record and more like a moving buffer

The shell should prioritize readability and transcript stability over token-by-token assistant streaming.

## Consequences

### Transcript behavior

- user messages remain stable once submitted
- assistant final text is appended only after completion
- the transcript should not reflow continuously due to a growing assistant message

### In-flight runtime feedback

Live feedback is still useful, but it should come from **ephemeral runtime activity**, not partially rendered assistant prose.

Examples of acceptable live activity:

- thinking indicator / thinking summary
- tool call started
- tool running
- tool completed / failed

This activity should primarily live in:

- the panel above the composer

It may also be reflected secondarily in footer/status state when useful, but the panel above the composer is the intended primary home for ephemeral runtime activity.

### Session model

The final assistant message should still be appended to the Pi-compatible session in the normal way.

Temporary runtime activity does **not** need to be represented as a final committed assistant transcript entry unless there is a Pi-compatible session entry type that should persist.

## Follow-up design work

This decision implies that the runtime loop should distinguish between:

1. **committed session-derived transcript items**
2. **ephemeral in-flight runtime activity**

That separation should guide the design of:

- backend turn execution state
- transcript refresh behavior
- tool activity presentation
- footer/panel runtime status
