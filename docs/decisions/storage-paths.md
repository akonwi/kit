# Decision: Storage and Path Conventions

- Status: Accepted
- Date: 2026-03-24
- Updated: 2026-04-07

## Context

`kit` needs clear app-owned conventions for where state lives on disk.

Earlier plans kept a split between Pi-compatible storage and app-native storage.
That is no longer the architecture.

## Decision

`kit` stores its state under a single app-owned root:

```text
~/.kit/
  auth.json
  settings.json
  sessions/
    <id>.json
```

## Conventions

### Settings

Path:

```text
~/.kit/settings.json
```

This is the only settings source.

### Auth

Path:

```text
~/.kit/auth.json
```

This is the primary credential store for authenticated providers.

### Sessions

Path:

```text
~/.kit/sessions/<id>.json
```

Sessions are stored as one JSON file per session.

The current session shape is turn-first rather than flat-message-first:

```json
{
  "id": "uuid",
  "version": 1,
  "cwd": "/path/to/project",
  "name": "optional display name",
  "model": "optional model id",
  "createdAt": "iso8601",
  "updatedAt": "iso8601",
  "turns": [
    {
      "id": "turn-id",
      "messages": [
        { "role": "user", "turnId": "turn-id", "content": "..." }
      ]
    }
  ]
}
```

## Single source of truth

Path resolution should come from the app's own path/settings/session modules.
No module should assume Pi storage roots.

## Consequences

- there is no `~/.pi/agent` fallback
- there is no Pi session directory compatibility target
- session persistence can evolve around the shell/runtime model
- explicit turns are a first-class persistence concept

## Decision

All app-owned state lives under `~/.kit/`.
