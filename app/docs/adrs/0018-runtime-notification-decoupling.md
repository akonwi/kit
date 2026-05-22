# 0018: Remove toast-style notifications from `AgentRuntime`

## Status
Accepted

## Context

`AgentRuntime` currently exposes toast-like notification concepts directly:

- runtime events:
  - `notification.error`
  - `notification.warning`
  - `notification.info`
- convenience methods:
  - `runtime.emitError(...)`
  - `runtime.emitWarning(...)`
  - `runtime.emitInfo(...)`

That couples the runtime to user-facing UI feedback.

This is a poor boundary for Kit's architecture:

- the runtime should own agent/session/tool orchestration and domain events
- the app/shell/plugin layer should own presentation concerns like toasts
- many non-runtime callers currently use `runtime.emit*()` as a generic feedback bus
- `app-state` currently converts runtime notification events into toast UI state

Kit already has the beginnings of a better boundary through app-owned/plugin-owned UI surfaces.

## Decision

Kit will move toast-style feedback out of `AgentRuntime`.

### Ownership boundary

- `AgentRuntime` should emit domain/runtime events and throw typed errors when appropriate
- the app/shell/plugin layer should own user-facing toast presentation
- commands, controllers, and plugins should report imperative user feedback through a UI-owned `.toast()` API
- runtime domain events may still be translated into toasts, but that translation must happen outside the runtime

### Preferred API

The preferred imperative UI feedback API is:

- `.toast({ title, lines, variant })`

not:

- `.notify(message, type)`

This keeps the shared API aligned with the actual toast model already used by the shell.

## Initial rollout shape

### UI-owned toast API

Expose `.toast()` in:

- `PluginUI`
- `CommandContext`
- controller dependencies where imperative UI feedback is needed

### Runtime slimming direction

Over time, remove from `AgentRuntime`:

- `notification.error`
- `notification.warning`
- `notification.info`
- `emitError(...)`
- `emitWarning(...)`
- `emitInfo(...)`

and replace runtime-internal notification emission with:

- domain-specific runtime events
- typed errors surfaced to callers

## Migration categories

### Category A — Imperative UI feedback

Move these first to `.toast()`:

- slash commands
- composer/controller validation messages
- modal actions
- plugin-owned UX messages

Examples:

- command success/failure messages
- thread reference validation failures
- queued attachment restrictions
- session auto-name warnings
- code review browser open/failure feedback

### Category B — Runtime domain events

Replace runtime-owned toast emission with domain signals.

Examples:

- session persistence failure
- auto-compaction success/failure
- overflow recovery success/failure
- pending follow-ups promoted to steering
- unexpected runtime/finalization errors

## Consequences

### Positive

- `AgentRuntime` becomes narrower and more architecturaly coherent
- presentation concerns move to the app/plugin layer where they belong
- command/controller/plugin code gets a clearer user-feedback surface
- runtime events can become more domain-specific and less UI-shaped

### Trade-offs

- migration touches many call sites
- we must avoid scattering toast policy everywhere
- some runtime events will need replacement domain events before the old notification events can be removed

## Rollout checklist

### Phase 1 — Introduce shared toast API
- [x] Add a shared toast payload type
- [x] Add `.toast()` to `PluginUI`
- [x] Add `.toast()` to `CommandContext`
- [x] Add `.toast()` to controller dependencies that currently lean on `runtime.emit*()`
- [x] Keep the app as the owner of actual toast rendering/state

### Phase 2 — Migrate imperative callers off `runtime.emit*()`
- [x] Migrate slash commands to `.toast()`
- [x] Migrate composer/controller validation feedback to `.toast()`
- [x] Migrate plugin UX feedback to `.toast()`
- [x] Migrate modal-driven local UI feedback to `.toast()` where practical

### Phase 3 — Replace runtime-internal notification emissions
- [x] Identify each runtime-owned `notification.*` emission site
- [x] Convert runtime success/failure notifications into domain events where needed
- [x] Throw typed errors instead of emitting toast-shaped events where caller-owned handling is better
- [x] Add any missing runtime events required for app/plugin reaction

### Phase 4 — Remove runtime notification API
- [x] Remove `notification.error` from `RuntimeEventMap`
- [x] Remove `notification.warning` from `RuntimeEventMap`
- [x] Remove `notification.info` from `RuntimeEventMap`
- [x] Remove `emitError(...)`
- [x] Remove `emitWarning(...)`
- [x] Remove `emitInfo(...)`
- [x] Remove the `app-state` bridge from runtime notification events to toast state

### Phase 5 — Cleanup and validation
- [x] Ensure toast policy remains centralized and readable
- [x] Verify commands still report useful feedback
- [x] Verify runtime-originated failures still surface to the user appropriately
- [x] Run `bun run typecheck`
- [x] Run `bun run check`
- [x] Re-run `bun run typecheck`

## Immediate next slice

Start with Phase 1 and the lowest-risk part of Phase 2:

- add the shared `.toast()` API
- wire it through `App.tsx`, `PluginUI`, `CommandContext`, and composer controller dependencies
- migrate command-side feedback first

This captures the new boundary without yet forcing the deeper runtime event redesign.

## Related

- `docs/adrs/0015-plugin-system.md`
- `docs/adrs/0017-namespaced-runtime-events.md`
