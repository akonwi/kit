# 0007: Do not stream assistant message text

## Status
Accepted

## Context

Kit needs transcript behavior that prioritizes readability and stability over token-by-token rendering.

## Decision

Kit does not stream assistant message text into the transcript while the model is generating it.

Instead:

- thinking activity may be shown live
- tool calls may be shown live
- tool-running or tool-progress state may be shown live
- tool results may appear when complete
- the final assistant message appears atomically once complete

## Why

Streaming assistant text makes the transcript harder to read:

- users start reading before the message is stable
- the text continues to grow and can move out of view
- the distinction between partial and final output becomes visually confusing
- the transcript becomes less like a durable conversation record and more like a moving buffer

## Consequences

### Transcript behavior

- user messages remain stable once submitted
- assistant final text is appended only after completion
- the transcript does not reflow continuously due to a growing assistant message

### In-flight runtime feedback

Live feedback is still useful, but it comes from ephemeral runtime activity rather than partially rendered assistant prose.

Examples include:

- thinking indicator or thinking summary
- tool call started
- tool running
- tool completed or failed

### Session model

The final assistant message is still appended to the persisted session in the normal way.

Temporary runtime activity does not become a committed transcript entry unless a deliberate persisted entry type is introduced later.

## Related

- `docs/adrs/0003-custom-shell.md`
- `docs/adrs/0004-turn-based-session-model.md`
