# pi-kit v2

Standalone `pi-kit` application with:

- Pi session compatibility
- Pi-core-compatible backend behavior
- a custom terminal shell built outside Pi interactive mode
- an OpenTUI Solid-based UI scaffold

## Installation

Requires [Bun](https://bun.sh) (>= 1.3.0).

```bash
bun install

# Symlink the executable into your PATH
ln -sf "$(pwd)/bin/pi-kit" ~/.bun/bin/pi-kit
```

Then run `pi-kit` from any directory.

```bash
pi-kit              # opens most recent session for the current directory
pi-kit -s abc123    # opens a specific session by ID
```

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
