# Slash Commands

## Status

Available now.

The command system is active and exposed through the composer slash picker.

## Available now

The command surface is composed from built-in commands plus plugin-registered commands.

The currently available built-in and core plugin commands are:

| Command | Description |
|---------|-------------|
| `/bells` | Toggle audible notification sounds |
| `/speech` | Toggle speech notifications |
| `/settings` | Open the in-app settings modal |
| `/pager` | Open pager for the last long assistant response |
| `/handoff [message]` | Fork the current session into a linked child session |
| `/login` | Authenticate a provider |
| `/model` | Switch model |
| `/name` | Set the current session name |
| `/new` | Start a new session |
| `/reload` | Reload the current session and rediscover context |
| `/diff` | View the current uncommitted diff in a terminal modal |
| `/debug` | Show runtime and session debug details |
| `/sessions` | Browse, switch, or delete sessions |
| `/tree` | Browse the current session tree in a modal explorer |
| `/thinking` | Change the current thinking level |
| `/quit` | Exit the application |

## Command UX

1. Type `/` in the composer
2. A filterable slash-command picker opens
3. The picker filters on the command token only (text before the first space)
4. Any text after the first space is treated as command args
5. Press `Tab` to complete the currently focused command in-place and keep the picker open
6. Press `Enter` to run the selected command

For example:

- `/handoff` — run handoff with no args
- `/handoff continue with the refactor` — run handoff with `continue with the refactor` as `args`

Commands with inline args can expose a muted argument hint in the picker, such
as `/handoff [message]`.

## Command model

Each command is a `Command` object with:

- `name`
- `description`
- `argName?`
- `execute(ctx)`

`ctx` currently provides:

- `runtime`
- `palette`
- `args`
- `openCustomOverlay`

## Notes

- Unknown slash-prefixed text currently falls back to normal composer submission
- Prompt templates discovered from prompt directories are also registered as slash commands at runtime
- Some commands are registered by plugins during initialization rather than directly in `src/features/commands/index.ts`

## Source

- `src/features/commands/index.ts`
- `src/features/commands/types.ts`
- `src/shell/composer-controller.ts`
- `src/shell/InlinePicker.tsx`
