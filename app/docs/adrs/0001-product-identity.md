# 0001: Product identity

## Status
Accepted

## Context

Kit has its own shell, storage, settings, auth, and app experience.

It still uses some lower-level libraries from the Pi ecosystem where they remain technically useful, but Kit is not defined by Pi compatibility.

Without an explicit decision record, documentation and implementation guidance can drift toward stale compatibility language or unclear product framing.

## Decision

Kit is a standalone app.

Kit owns its own:
- shell and UX decisions
- storage and session model
- settings and auth
- built-in tools

Kit may continue to use lower-level libraries when they are the right technical foundation, including:
- `@earendil-works/pi-agent-core`
- `@earendil-works/pi-ai`
- `@opentui/core`
- `@opentui/solid`

Those dependencies do not make Kit a Pi-compatible shell.

## Consequences

### Positive

- simpler product positioning
- fewer stale compatibility assumptions in docs and code
- storage and runtime behavior can evolve around Kit's needs
- shell and UX decisions can be made without compatibility-driven constraints

### Trade-offs

- Kit is not a compatibility target for older Pi shell assumptions
- migration or import behavior, if needed later, must be explicit rather than implied by architecture
- documentation must describe Kit as its own product rather than as a transition from Pi

## Related

- `docs/features/`
- `AGENTS.md`
