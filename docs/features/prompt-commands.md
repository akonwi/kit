# Prompt commands

Prompt commands are Markdown templates contributed to the native command palette. Selecting one expands its arguments and submits the result as an ordinary user prompt.

## Discovery and precedence

The server discovers non-recursive `*.md` files from:

1. the resolved Kit home's `prompts/` directory; then
2. `<session-cwd>/.agents/prompts/`.

During v2 development, the default global directory is `~/.kit-v2/prompts/`. `KIT_HOME` changes that location. Global definitions win project name collisions. Built-in palette commands keep their names if a prompt file collides with one.

Discovery belongs to the session runtime on the server. Remote clients receive only bounded command metadata and do not read server-side files themselves. Add, change, or remove files and run **reload** to replace the command snapshot atomically; reload remains available during an active turn. Changing the session cwd retargets relative filesystem tools but does not implicitly replace project prompt commands; reload after moving when commands from the destination should apply.

## Template format

The filename without `.md` is the command name. Optional YAML frontmatter supplies its picker description:

```markdown
---
description: Review recent changes
---
Review $1 carefully. Additional context: $@
```

If `description` is omitted, Kit uses the first non-empty body line, truncated to 60 characters when necessary.

Templates support:

| Placeholder | Expansion |
|---|---|
| `$1`, `$2`, … | One-based positional argument |
| `$@` | All parsed arguments joined by spaces |
| `$ARGUMENTS` | Same as `$@` |
| `${@:N}` | Arguments from position N onward |
| `${@:N:L}` | L arguments beginning at position N |

Single and double quotes group arguments:

```text
/review "auth module" carefully
```

Here `$1` is `auth module`, `$2` is `carefully`, and `$@` is `auth module carefully`.

The command palette treats text after the first space as arguments. Prompt commands are available only while the session is idle and can be run with Enter or the primary mouse button. The expanded template is persisted and rendered as the user message.

Discovery is bounded to 128 commands, 1,024 entries per prompt directory, 128 KiB per template, 1,024 bytes per description, and 4 KiB per absolute source location. Traversal is constrained to the resolved Kit home or explicit session cwd; symlinked prompt files and search directories are omitted.
