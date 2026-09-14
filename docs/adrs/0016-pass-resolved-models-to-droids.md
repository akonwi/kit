# 0016: Pass resolved models to Droids

## Status

Accepted

## Context

Droids currently accepts a model selector and the complete provider registry in
`Config`. `Spawn` resolves that selector into separate provider behavior and
model metadata. This makes Droids responsible for application-level selection,
hides the catalog generation at which resolution occurred, and permits forks
to resolve different metadata from their parent.

A usable model is more than catalog metadata. It couples immutable model
metadata with the provider behavior that streams requests, validates replay,
and optionally measures context. Passing only the existing metadata value would
still require Droids to recover that provider binding and would not move model
resolution to the caller.

## Decision

Droids represents a resolved model as one provider-bound value. Model
registries resolve user-facing selectors into that value before `Spawn`:

```go
model, err := models.Resolve("openai-codex/gpt-5.6-sol")
if err != nil {
    return err
}

model = model.WithContextWindow(1_000_000)

droid, err := droids.Spawn(ctx, id, droids.Config{
    Model: model,
})
```

The resolved model owns both effective metadata and the behavior needed to use
it. `Config.Model` accepts that resolved value rather than a selector. The
active runtime does not receive the complete registry merely to recover its
provider.

`WithContextWindow` returns an immutable copy with adjusted effective context
metadata and the same provider binding. It does not compare the value with
catalog metadata or presumed provider limits. Settings boundaries may enforce
structural requirements such as a positive integer, but the provider request
and response remain authoritative about actual compatibility.

Durable and configuration boundaries continue to use canonical
`provider/model-id` selectors. The session layer resolves those selectors and
applies user overrides before constructing a droid. Droids persistence projects
resolved models back to canonical selectors and never stores provider bindings.

Features that use an alternate model, including explicit context adaptation and
separately configured compaction, accept an already resolved model. Droids does
not retain a registry or resolver in `Config`. The zero compaction model uses the
active model.

Resolved models are snapshots. Catalog refresh affects future resolutions but
does not mutate existing droids. Forks inherit the parent's resolved model and
effective metadata rather than resolving the selector again. Dynamic request
inputs such as rotating credentials remain request-time behavior inside the
provider binding.

## Required properties

- A resolved model cannot accidentally pair metadata with a different provider.
- `Spawn` does not resolve the active model from a selector.
- Model metadata exposed to callers is copied or immutable.
- `WithContextWindow` preserves provider behavior and does not perform provider
  capability validation.
- Existing droids retain their resolved metadata across catalog refreshes.
- Forks inherit the exact active model snapshot.
- Session and protocol persistence continue to store canonical model identity,
  not process-local provider capabilities.
- Alternate-model resolution remains explicit and narrowly scoped.
- Provider failures continue through the normal model stream and error path.

## Consequences

### Positive

- Application code owns selection and configuration while Droids owns model
  execution.
- Provider/model mismatches become unrepresentable in normal construction.
- Resolution timing and catalog snapshot semantics are explicit.
- Context-window overrides compose naturally with model resolution.
- Tests can supply one resolved fake model without constructing an unrelated
  provider registry for ordinary execution.

### Trade-offs

- This is a breaking internal API change across Droids callers and tests.
- Metadata-only projections need a distinct type or accessor.
- Callers must resolve context-adaptation and alternate-compaction models before invoking Droids.
- Provider implementations must construct bound model values while preserving
  request-time credential rotation and optional context measurement.

## Related

- [ADR 0004: Establish Droids as Kit's agent runtime boundary](0004-droids-agent-runtime-boundary.md)
- [ADR 0006: Make Droids the session data authority](0006-droids-as-session-data-authority.md)
- [ADR 0015: Configure model-specific context windows](0015-model-specific-context-windows.md)
