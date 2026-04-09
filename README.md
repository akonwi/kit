# kit

Standalone `kit` application with:

- its own session storage under `~/.kit/sessions`
- its own auth storage under `~/.kit/auth.json`
- its own settings under `~/.kit/settings.json`
- a custom terminal shell built with OpenTUI Solid

## Requirements

Requires [Bun](https://bun.sh) (>= 1.3.0).

## Install for normal use

```bash
bun install
bun run build

# Symlink the compiled binary into your PATH
ln -sf "$(pwd)/dist/kit" ~/.bun/bin/kit
```

Then run `kit` from any directory.

```bash
kit              # opens most recent session for the current directory
kit -s abc123    # opens a specific session by ID
```

## Development

Run from source:

```bash
bun run dev
```

Build the distributed binary:

```bash
bun run build
```

Typecheck:

```bash
bun run typecheck
```

## Notes

- the compiled binary is the intended non-development entry point
- the old wrapper-based launcher is no longer the recommended install path
- Kit session, auth, and settings state live under `~/.kit`

## Architecture

See:

- `docs/architecture/custom-shell.md`
- `AGENTS.md`
