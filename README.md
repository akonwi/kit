# pi-kit v2

Standalone `pi-kit` application with:

- Pi session compatibility
- Pi-core-compatible backend behavior
- a custom terminal shell built outside Pi interactive mode
- an OpenTUI Solid-based UI scaffold

## Status

Early scaffold.

The current focus is establishing:

- app structure
- compatibility layer boundaries
- shell boundaries
- config/session resolution rules

## Compatibility

### Pi compatibility root

By default, the app reads Pi-compatible state from:

- `~/.pi/agent`

This is intended to preserve compatibility with existing Pi sessions and baseline Pi-managed state.

### pi-kit settings

The app's own settings live at:

- `~/.pi-kit/settings.json`

If both Pi baseline settings and pi-kit settings exist, **pi-kit settings win**.

## Development

```bash
bun install
bun run dev
```

Typecheck:

```bash
bun run typecheck
```

## Architecture

See:

- `docs/architecture/custom-shell.md`
- `AGENTS.md`
