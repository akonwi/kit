# Slash Commands

## Status

Available now.

The command system is active and exposed through the composer slash picker.

## Available now

The commands currently registered in `src/features/commands/index.ts` are:

| Command | Description |
|---------|-------------|
| `/bells` | Toggle audible notification sounds |
| `/speech` | Toggle speech notifications |
| `/handoff [message]` | Fork the current session into a linked child session |
| `/login` | Authenticate a provider |
| `/model` | Switch model |
| `/name` | Set the current session name |
| `/new` | Start a new session |
| `/debug` | Show runtime and session debug details |
| `/sessions` | Browse, switch, or delete sessions |
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

## Notes

- Unknown slash-prefixed text currently falls back to normal composer submission
- Some additional command modules still exist in `src/features/commands/`, but
  only the commands registered in `src/features/commands/index.ts` are active

## Source

- `src/features/commands/index.ts`
- `src/features/commands/types.ts`
- `src/shell/composer-controller.ts`
- `src/shell/InlinePicker.tsx`
