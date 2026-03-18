# Slash Commands

Built-in commands accessible via `/` in the composer.

## Available Commands

| Command | Description |
|---------|-------------|
| `/new` | Start a new session |
| `/model` | Switch model |
| `/thinking` | Cycle thinking level (off, minimal, low, medium, high, xhigh) |
| `/name` | Rename current session |
| `/switch` | Switch to another session |
| `/sessions:manage` | Browse, create, delete sessions |
| `/quit` | Exit the application |
| `/handoff` | Transfer context to a new session |

## How it works

1. Type `/` to trigger command palette
2. Filter commands by typing
3. Select with Enter or click
4. Some commands open pickers (model, sessions) or prompts (name)

## Command Structure

Each command is a `Command` object with:
- `name` — command identifier
- `description` — shown in palette
- `execute(ctx)` — receives runtime and palette manager

## Source

`src/features/commands/`
