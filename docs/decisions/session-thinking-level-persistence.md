# Session Thinking Level Persistence

- Status: Accepted
- Date: 2026-04-08

## Decision

Kit should persist the active thinking level in session state and restore it when
reopening, reloading, switching, or handing off sessions.

## Restore behavior

When restoring a session, Kit should:

1. load the saved thinking level from the session
2. clamp it against the restored model's supported thinking levels
3. apply the clamped level to the runtime agent

If a session has no saved thinking level, Kit may fall back to the runtime
default.

## Why clamp on restore

A session may have been saved with a thinking level that is no longer valid for
its restored model, for example after model availability changes or when a
session is reopened under a different model capability set.

Restore should prefer preserving the user's intent while still yielding a valid
runtime state.

## Scope

This decision is limited to session persistence and restore semantics for the
current app-owned session format.
