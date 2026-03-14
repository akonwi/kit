# AGENTS.md

## Project identity

`v2/` is a **standalone pi-kit application**, not a Pi extension pack.

The goal is to build a custom coding-agent UX while preserving compatibility with the Pi ecosystem where it matters most:

- existing Pi session files
- core Pi agent/message/tool semantics
- baseline Pi settings and storage conventions where practical

## Architecture priorities

1. **Pi-core compatibility first**
   - Preserve session compatibility with `~/.pi/agent` by default.
   - Prefer additive compatibility layers over rewriting Pi data contracts.

2. **Standalone shell, not extension-hosted UI**
   - Do not optimize for `ctx.ui.*`, footer widgets, or Pi interactive-mode constraints.
   - Build app-owned shell abstractions instead.

3. **App-native settings win**
   - pi-kit app settings live at `~/.pi-kit/settings.json`.
   - If both Pi and pi-kit settings exist, pi-kit settings take precedence.

4. **Keep future UI extensions possible, but deferred**
   - Avoid painting the shell into a corner.
   - Do not introduce a public extension API prematurely.

## Implementation guidance

- Favor modular layers:
  - `compat/`
  - `backend/`
  - `shell/`
  - `features/`
- Extract reusable logic from the old extension code, but do not carry over extension-era structure unless it still serves the new app.
- Keep renderer-specific assumptions isolated from domain and compatibility logic.
- When we make an architectural or feature design decision, capture it under `docs/`.
- When we identify outstanding work or defer something, capture it under `backlog/`.

## Non-goals for now

- Pi extension API compatibility
- Reproducing Pi interactive mode UI behavior
- Designing a full plugin/UI extension platform before the shell is stable
