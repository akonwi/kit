# Decision: Storage and Path Conventions

- Status: Accepted
- Date: 2026-03-24

## Context

pi-kit needs clear conventions for where app state lives on disk, balancing Pi compatibility with app-native config.

## Decision

### Two roots

| Root | Path | Purpose |
|------|------|---------|
| **Pi compatibility** | `~/.pi/agent/` | Shared state that Pi can also read/write |
| **Pi-kit app** | `~/.kit/` | App-native config and state |

### Pi compatibility root (`~/.pi/agent/`)

pi-kit reads and writes to this directory for:

- **Sessions** (`sessions/`) — `.jsonl` session files, shared format with Pi
- **Auth** (`auth.json`) — API key credentials, managed by Pi's `AuthStorage`
- **Agents** (`agents/`) — user-level agent `.md` definitions
- **Settings** (`settings.json`) — Pi settings, used as fallback

pi-kit does **not** create its own session directory. Sessions remain in `~/.pi/agent/sessions/` to preserve full interoperability with Pi.

### Pi-kit app root (`~/.kit/`)

pi-kit owns this directory for app-specific state:

- **Settings** (`settings.json`) — kit settings, takes precedence over Pi settings
- **Notifications** (`notifications.json`) — bell/speech preferences

### Settings precedence

1. `~/.kit/settings.json` (app-native, wins when present)
2. `~/.pi/agent/settings.json` (Pi fallback)
3. Built-in defaults

This makes it easy to migrate to more pi-kit-specific settings in the future, since the settings structure can mirror Pi's where applicable and diverge where needed.

### Single source of truth

All resolved paths are centralized in `src/compat/paths.ts` via `getPiKitPaths()`. No other module should hardcode `~/.pi/agent` or `~/.kit` paths directly.

### Project-local state

Project-local discovery (`.pi/agents/`, `.pi/prompts/`, `.pi/skills/`, `AGENTS.md`) is handled by Pi's `createAgentSession` resource loader. pi-kit does not add its own project-local conventions.

## Consequences

- Notification config moves from `~/.pi/agent/kit.json` to `~/.kit/notifications.json`
- Future app-specific state (e.g., UI preferences, theme selection) should go in `~/.kit/`
- Pi session compatibility is preserved without pi-kit needing to own the session storage path
