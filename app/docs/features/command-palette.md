# Commands

Commands are exposed through the **command palette**, a centered modal overlay.

The command surface is composed from built-in commands, plugin-registered commands, prompt commands discovered from prompt directories, and Claude Code compatibility commands discovered from `.claude/commands/`.

Core commands currently include:

- `cd [path]`
- `speech`
- `settings`
- `pager`
- `zen`
- `code-review`
- `handoff [message]`
- `login`
- `model`
- `name`
- `new`
- `reload`
- `debug`
- `sessions`
- `tree`
- `thinking`
- `quit`

Additional prompt commands can appear at runtime based on discovered prompt files.

Claude Code compatibility commands can also appear at runtime from project-local `.claude/commands/*.md` files. These are exposed with a `cc:` prefix, for example:

- `.claude/commands/draft-pr.md` → `cc:draft-pr`

## How to access it

Press `Ctrl+P` by default to open the command palette (or `/` while focused on the message input).

Commands can also be bound directly by command id. For example, bind `/code-review` with the `code-review` keybinding id. Direct command keybindings run with empty args.

## Behavior

1. A centered modal opens with a filterable list of all commands
2. The filter matches on the command name; text after the first space is treated as command args
3. Press `Tab` to complete the currently focused command in place and keep the palette open
4. Press `Enter` to run the selected command
5. Press `Esc` to close the palette
6. Commands that need additional input (e.g. model picker, session list) push sub-palettes onto the same modal stack
