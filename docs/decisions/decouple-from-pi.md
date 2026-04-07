# Decision: Full decoupling from Pi

## Status

Accepted

## Context

`kit` started life as Pi extension work and then as a standalone app that still
tried to preserve Pi compatibility.

That is no longer the goal.

The app now prefers independence over compatibility and uses Pi packages only
where they are still the right technical foundation.

## Decision

Remove `@mariozechner/pi-coding-agent` and stop targeting Pi storage/session
compatibility.

Keep only the lower-level foundations that are still useful:

- `@mariozechner/pi-agent-core` — agent loop, tool/message/event types
- `@mariozechner/pi-ai` — provider/model abstraction
- `@opentui/core` + `@opentui/solid` — terminal UI rendering

## What this means in practice

### Storage is app-owned

`kit` owns its own storage under `~/.kit/`:

```text
~/.kit/
  auth.json
  settings.json
  sessions/
    <id>.json
```

No Pi session format or Pi session directory is used.

### Runtime wiring is app-owned

`kit` uses its own runtime wrapper around `pi-agent-core`.

Current key pieces:

- `src/runtime/kit-agent.ts`
- `src/runtime/agent-runtime.ts`

### Tools are app-owned

Built-in tools are implemented in `src/tools/` and no longer come from
`pi-coding-agent`.

### Settings and auth are app-owned

- settings: `~/.kit/settings.json`
- auth: `~/.kit/auth.json`

There is no Pi fallback.

## Replaced assumptions

These old assumptions are obsolete:

- Pi session compatibility
- Pi settings fallback
- Pi auth fallback
- `compat/` as a major architectural layer
- `pi-kit` naming and storage roots

## Consequences

### Positive

- simpler architecture
- fewer hidden dependencies on Pi internals
- app-specific UX no longer constrained by compatibility decisions
- persistence format can evolve around `kit`'s shell/runtime needs

### Trade-offs

- existing Pi sessions are not a compatibility target
- some previously deferred design choices now belong fully to `kit`
- documentation must describe `kit` as its own product, not a Pi-compatible shell

## Current foundation after decoupling

The current baseline is now established:

- standalone session storage
- standalone auth storage
- standalone settings
- standalone built-in tools
- `KitAgent` + `AgentRuntime` around `pi-agent-core`
- OpenTUI shell

## Decision

`kit` is a standalone app built on Pi core libraries, not a Pi-compatible shell.
