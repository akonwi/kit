# 0003: Custom shell architecture

## Status
Accepted

## Context

Kit needs an app-owned shell architecture that cleanly separates runtime behavior, persisted sessions, reactive UI state, and feature logic.

Without a clear architectural decision, shell concerns, persistence concerns, and renderer-specific details can become entangled.

## Decision

Kit uses a layered architecture with separate runtime, session, state, shell, and feature responsibilities.

## Architecture

### Runtime layer

The runtime layer owns agent execution, turn lifecycle, and runtime event emission.

### Session layer

The session layer owns persisted session data and the on-disk session model.

Sessions are stored as explicit turn-based data rather than as a flat message log.

### State layer

The state layer translates runtime events into reactive app state for the shell.

### Shell layer

The shell layer owns terminal presentation, focus, layout, and interaction patterns.

The shell is organized around explicit regions:
- transcript or main content
- fixed bottom composer or dock
- inline picker or overlay layer
- ephemeral toast layer

### Feature layer

The feature layer owns behavior built on top of the shell and runtime foundation.

## Runtime event flow

Runtime-to-UI communication follows a subscription model:

```text
Agent runtime
  -> emits runtime events
App state
  -> updates reactive state
Shell
  -> renders current state
User actions
  -> call runtime or shell actions
```

## Session and transcript model

Turn-based sessions are a first-class part of the architecture.

This means:
- sessions persist explicit turns
- messages belong to a specific turn
- transcript rendering can derive directly from persisted turn structure

## Consequences

- runtime, persistence, UI state, and shell rendering stay cleanly separated
- shell behavior can evolve without collapsing architectural boundaries
- transcript rendering aligns with the persisted session model
- feature work can build on stable runtime and shell interfaces

## Related

- `docs/adrs/0002-storage-paths.md`
- `docs/features/`
