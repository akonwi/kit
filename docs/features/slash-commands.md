# Slash Commands

## Status

Partially available.

The command system is active, but the current minimum working loop only exposes a
small command set.

## Available now

The commands currently registered in `src/features/commands/index.ts` are:

| Command | Description |
|---------|-------------|
| `/login` | Authenticate a provider |
| `/model` | Switch model |
| `/quit` | Exit the application |

## Command UX

1. Type `/` in the composer
2. A filterable palette opens
3. Select a command with Enter or mouse
4. The command runs its own logic using the runtime and palette manager

## Command model

Each command is a `Command` object with:

- `name`
- `description`
- `execute(ctx)`

`ctx` currently provides:

- `runtime`
- `palette`

## Additional command code in the repo

There are more command modules in `src/features/commands/`, including commands
for sessions, handoff, steering, pager, and others.

However, many of those are **not currently registered in the active command
list** while the app is being rebuilt on the new architecture.

## Source

- `src/features/commands/index.ts`
- `src/features/commands/types.ts`
- `src/shell/composer-controller.ts`
