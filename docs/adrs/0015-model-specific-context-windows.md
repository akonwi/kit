# 0015: Configure model-specific context windows in user settings

## Status

Accepted

## Context

Model catalogs provide conservative context-window metadata, but providers may
make larger windows available for particular models. Users need to opt into
those windows without changing provider identity or maintaining a separate
model catalog.

The context window is runtime budgeting metadata, not a provider request's
maximum output-token value. It controls Kit's context accounting and automatic
compaction behavior.

## Decision

Kit supports a `modelOverrides` object in user settings. Each key is an exact
`provider/model-id` selector. The supported override is `contextWindow`:

```json
{
  "modelOverrides": {
    "openai-codex/gpt-5.6-sol": {
      "contextWindow": 1000000
    }
  }
}
```

Settings parsing validates structure only: selectors contain a non-empty
provider and model ID, and `contextWindow` is a positive integer. Kit does not
compare the value with catalog metadata.

When Kit opens a model runtime, it copies the catalog model and replaces its
context window and effective input limit with the configured value. Context
accounting and automatic compaction therefore use the override. Provider output
limits remain unchanged.

The override is sampled when a runtime opens and when `/reload` atomically
reconfigures its context accounting. An in-flight request retains the budget it
started with. Model discovery projects the effective context window so clients
do not display stale catalog values.

The settings UI supports adding, changing, and clearing an override. In the
native TUI this management flow is available from the `/model` picker.

## Required properties

- Overrides are isolated by exact provider/model selector.
- Omitted overrides preserve catalog context-window metadata.
- Output-token request limits are unaffected.
- In-flight requests retain their captured context budget; `/reload` applies a
  changed budget to subsequent work.
- Context compaction uses the overridden budget.
- Model pickers display the effective overridden value.

## Consequences

Users can opt into provider-supported context windows while Kit retains its
existing compaction policy. Structurally valid but unsupported values may still
cause provider context-limit errors.

## Related

- [ADR 0004: Establish Droids as Kit's agent runtime boundary](0004-droids-agent-runtime-boundary.md)
- [ADR 0006: Make Droids the session data authority](0006-droids-as-session-data-authority.md)
