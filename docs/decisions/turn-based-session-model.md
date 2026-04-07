# Decision: Persist explicit turns, not heuristic message groups

- Status: Accepted
- Date: 2026-04-07

## Context

The transcript UI wants to present messages in turn-shaped groups:

- user message
- assistant/tool activity generated from that user message
- per-turn visual treatment such as aborted state and grouped tool results

When sessions were modeled as a flat `AgentMessage[]`, the UI had to reconstruct
turns heuristically by scanning messages and treating each user message as the
start of a new turn.

That had a few problems:

- turn grouping lived in the view layer
- grouping logic was derived rather than explicit
- Pi core does not expose persisted turn buckets, only turn lifecycle events
- transcript rendering had to do unnecessary transformation work every render

## Decision

Persist sessions as explicit `Turn[]` and tag every committed message with its
own `turnId`.

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

`Session` now stores `turns: Turn[]`.

## Runtime implications

`KitAgent` owns turn tracking.

- on `turn_start`, it creates a new turn
- on `message_end`, it tags the committed message with the active `turnId`
- persisted/runtime turn shape is therefore produced as the agent runs, not
  reconstructed later by the UI

## UI implications

The transcript renders from explicit turns.

It may still derive presentation details inside a turn, such as:

- first user message in the turn
- collected tool results keyed by tool call id
- whether the turn contains an aborted assistant message

But it no longer decides where turn boundaries are.

## Consequences

### Benefits

- clearer data model
- cheaper and simpler transcript rendering
- easier future turn-level UX features
- each individual message still carries its turn identity when inspected alone

### Trade-off

- session format is more app-specific than a flat generic message log

## Decision

Turn boundaries are first-class runtime and persistence data, not a transcript
heuristic.
