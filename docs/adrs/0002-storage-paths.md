# 0002: Storage paths

## Status
Accepted

## Context

Kit needs clear and stable conventions for where app state lives on disk.

Without an explicit decision, path handling can drift across modules or become inconsistent between settings, auth, sessions, and other app-owned files.

## Decision

All app-owned state lives under a single storage root:

```text
~/.kit/
  auth.json
  mcp-auth.json
  mcp-cache.json
  mcp.json
  notifications.json
  settings.json
  sessions/
    <id>.json
  themes/
    <name>.json
```

## Conventions

### Settings

```text
~/.kit/settings.json
```

### Auth

```text
~/.kit/auth.json
```

### Notifications

```text
~/.kit/notifications.json
```

### MCP

```text
~/.kit/mcp.json
~/.kit/mcp-cache.json
~/.kit/mcp-auth.json
```

`mcp.json` is a Kit-owned MCP override config.

`mcp-cache.json` stores cached MCP server tool metadata.

`mcp-auth.json` stores persisted MCP OAuth client and token state.

### Sessions

```text
~/.kit/sessions/<id>.json
```

Sessions are stored as one JSON file per session.

The session format is turn-based. At a high level, a session contains top-level metadata plus an ordered list of turns, and each turn contains its messages.

### Themes

```text
~/.kit/themes/<name>.json
```

## Implementation rule

Path resolution must come from the app's path utilities and related storage modules.

Do not duplicate storage-root assumptions across unrelated modules.

## Consequences

- settings, auth, sessions, notifications, and themes share one predictable storage root
- path handling stays centralized instead of being re-derived ad hoc
- session persistence can evolve around Kit's runtime and shell model
- turn-based sessions remain a first-class storage concept

## Related

- `src/paths.ts`
- `docs/features/settings.md`
