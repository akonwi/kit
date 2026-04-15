# Namespaced runtime events

## Status

- [ ] not started

## Goal

Replace the current ad hoc runtime event union with a typed, namespaced event map and prefix-capable subscriptions.

Preferred direction: an event-map design like:

```ts
type RuntimeEventMap = {
  "session.updated.model": { modelId: string | undefined };
  "session.updated.name": { name: string | undefined };
  "runtime.updated.git": { git: GitInfo };
};
```

With APIs along the lines of:

- `emit<K extends keyof RuntimeEventMap>(type: K, payload: RuntimeEventMap[K])`
- exact subscriptions by event name
- prefix subscriptions for grouped listeners

## Why

Current broad events like `status_changed` and `session_changed` are too coarse for policy-driven reactions like model-switch overflow handling.

A typed namespaced event system would let us:

- react specifically to model changes
- avoid overloading generic status events
- subscribe to exact events or grouped prefixes cleanly
- scale event granularity without growing a fragile monolithic union

## Deferred for now

Short-term, use a simpler additive event such as `session_updated` and keep the larger event-system refactor for later.
