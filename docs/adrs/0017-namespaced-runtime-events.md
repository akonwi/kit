# 0017: Use namespaced runtime events with typed event maps

## Status
Accepted

## Context

`AgentRuntime` previously exposed an ad hoc event union with coarse names such as:

- `status_changed`
- `session_changed`
- `session_updated`
- `turn_complete`

That model worked for simple listeners, but it had several shortcomings:

- event names did not scale well as the runtime grew
- related events were not grouped structurally
- subscriptions were all-or-nothing, so consumers had to filter manually
- adding finer-grained runtime reactions risked growing a fragile monolithic union

Kit now has multiple runtime consumers across the shell, plugins, and feature integrations. The event system needs a more structured shape.

## Decision

Runtime events use a typed namespaced event map.

The runtime now defines a `RuntimeEventMap` keyed by namespaced event names such as:

- `session.turns.changed`
- `runtime.status.changed`
- `session.changed`
- `session.updated`
- `session.updated.name`
- `session.updated.model`
- `runtime.updated.git`
- `turn.completed`
- `notification.info`
- `notification.warning`
- `notification.error`

`AgentRuntimeEvent` is derived from that map rather than hand-written as a separate ad hoc union.

Runtime emission follows the event-map shape:

```ts
emit<K extends RuntimeEventName>(type: K, payload: RuntimeEventMap[K])
```

Subscriptions support three forms:

- subscribe to all runtime events
- subscribe to one exact event name
- subscribe to a namespaced prefix

## Subscription API

The runtime exposes:

- `runtime.subscribe(listener)`
- `runtime.subscribe("turn.completed", listener)`
- `runtime.subscribe({ prefix: "session.updated." }, listener)`

The plugin base class also exposes convenience helpers for exact and prefix subscriptions.

## Granularity policy

This refactor does not require every consumer to use only highly specific events.

Kit keeps both:

- coarse but namespaced events such as `session.changed` and `session.updated`
- finer additive events such as `session.updated.name`, `session.updated.model`, and `runtime.updated.git`

This keeps migration practical while enabling more specific policy-driven reactions over time.

## Consequences

### Positive

- runtime events have a consistent namespaced structure
- event typing comes from one source of truth
- listeners can subscribe exactly or by prefix instead of always filtering broad subscriptions
- finer-grained events can be added without inventing a new parallel union shape
- related event families are easier to reason about across shell and plugin code

### Trade-offs

- event names are more verbose than the old snake-case names
- migration requires touching all runtime consumers when event names change
- both coarse and fine events now coexist, which is intentional but slightly broader than a minimal exact-only model

## Initial usage direction

Consumers should prefer:

- exact subscriptions for highly specific reactions
- prefix subscriptions for grouped concerns
- broad subscriptions only when a consumer truly coordinates many runtime surfaces at once

Examples:

- plugins reacting to `turn.completed`
- title updates reacting to `session.changed` and `session.updated.name`
- future grouped listeners reacting to prefixes like `notification.` or `session.updated.`

## Related

- `docs/adrs/0015-plugin-system.md`
- `backlog/session-explorer-graph-view.md`
