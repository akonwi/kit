# 0004: Turn-based session model

## Status
Accepted

## Context

The transcript UI wants to present messages in turn-shaped groups:

- a user message
- assistant and tool activity generated from that user message
- per-turn visual treatment such as aborted state and grouped tool results

If sessions are modeled as a flat `AgentMessage[]`, the UI must reconstruct turns heuristically by scanning messages and treating each user message as the start of a new turn.

That creates several problems:

- turn grouping lives in the view layer
- grouping logic is derived rather than explicit
- transcript rendering does unnecessary transformation work

## Decision

Sessions persist explicit `Turn[]` data, and every committed message carries its `turnId`.

Current model:

```ts
type KitAgentMessage = AgentMessage & {
  turnId: string;
};

interface Turn {
  id: string;
  messages: KitAgentMessage[];
}
```

`Session` stores `turns: Turn[]`.

## Runtime implications

`KitAgent` owns Kit turn tracking while delegating the core agent loop to Pi's `Agent`.

- on `turn_start`, it creates a new turn
- on `message_end`, it tags the committed message with the active `turnId`
- the persisted turn structure is produced as the agent runs rather than reconstructed later by the UI

## UI implications

The transcript renders from explicit turns.

It may still derive presentation details inside a turn, such as:

- first user message in the turn
- collected tool results keyed by tool call id
- whether the turn contains an aborted assistant message

But it does not decide where turn boundaries are.

## Consequences

### Positive

- clearer data model
- cheaper and simpler transcript rendering
- easier future turn-level UX features
- each message still carries turn identity when inspected on its own

### Trade-off

- the session format is more app-specific than a flat generic message log

## Related

- `docs/adrs/0003-custom-shell.md`
- `docs/adrs/0002-storage-paths.md`
