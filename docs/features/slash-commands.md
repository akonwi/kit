# Slash Commands

Slash commands are exposed through the composer slash picker.

The command surface is composed from built-in commands, plugin-registered commands, and prompt commands discovered from prompt directories.

Core commands currently include:

- `/bells`
- `/speech`
- `/settings`
- `/pager`
- `/code-review`
- `/handoff [message]`
- `/login`
- `/model`
- `/name`
- `/new`
- `/reload`
- `/diff`
- `/debug`
- `/sessions`
- `/tree`
- `/thinking`
- `/quit`

Additional prompt commands can appear at runtime based on discovered prompt files.

Current behavior:

1. type `/` in the composer
2. a filterable slash-command picker opens
3. the picker filters on the command token only, meaning the text before the first space
4. any text after the first space is treated as command args
5. press `Tab` to complete the currently focused command in place and keep the picker open
6. press `Enter` to run the selected command

Unknown slash-prefixed text currently falls back to normal composer submission.

## How to access it

Type `/` in the composer.
