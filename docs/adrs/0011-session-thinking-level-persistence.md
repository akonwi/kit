# 0011: Session thinking level persistence

## Status
Accepted

## Context

Kit needs session restore behavior that preserves the user's intended thinking level without producing an invalid runtime state.

## Decision

Kit persists the active thinking level in session state and restores it when reopening, reloading, switching, or handing off sessions.

## Restore behavior

When restoring a session, Kit:

1. loads the saved thinking level from the session
2. clamps it against the restored model's supported thinking levels
3. applies the clamped level to the runtime agent

If a session has no saved thinking level, Kit may fall back to the runtime default.

## Why clamp on restore

A session may have been saved with a thinking level that is no longer valid for its restored model, for example after model availability changes or when a session is reopened under a different model capability set.

Restore should preserve user intent while still yielding a valid runtime state.

## Scope

This decision is limited to session persistence and restore semantics for the current session format.

## Related

- `docs/adrs/0004-turn-based-session-model.md`
