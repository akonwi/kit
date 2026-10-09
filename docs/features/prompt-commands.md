# Prompt commands

Prompt commands are Markdown templates contributed to the native command palette. Selecting one expands its arguments and submits the result as the user prompt. The user message records the invocation alongside its expansion.

## Discovery and precedence

The server discovers non-recursive `*.md` files from:

1. the resolved Kit home's `prompts/` directory;
2. `<session-cwd>/.agents/prompts/`; then
3. `<session-cwd>/.claude/commands/`, the project's Claude Code custom commands.

The default global directory is `~/.kit/prompts/`. `KIT_HOME` changes that
location; set it to an explicit isolated directory for development and tests. The first definition of a name wins, so global prompts win over project prompts, and Kit prompts win over Claude Code commands. Built-in palette commands keep their names if a prompt file collides with one.

### Claude Code commands

Kit reads only the top level of `<session-cwd>/.claude/commands/`. It does not read personal commands in `~/.claude/commands/`, `CLAUDE_CONFIG_DIR`, or namespaced commands in subdirectories. Claude Code commands use the same template format and placeholders as Kit prompts, with these differences:

- Only the `description` and `argument-hint` frontmatter keys are read, as literal text. Other keys, such as `allowed-tools` and `model`, are ignored, and frontmatter that is not valid YAML, such as `argument-hint: [pr-number] [priority]`, is accepted.
- `` !`command` `` and `@path` are sent to the model as literal text. Kit never runs the command or reads the file.
- Kit parses arguments as described below, so quotes are removed and `$ARGUMENTS` is the parsed arguments joined by spaces. Claude Code substitutes the raw argument text.

Set `readClaudeConfigs` to `false` in `$KIT_HOME/settings.json` to stop Kit reading Claude Code configuration:

```json
{ "readClaudeConfigs": false }
```

The setting defaults to `true`. A non-boolean value produces a settings warning and keeps the default. A change applies when a session runtime next discovers prompt commands, when it loads or reloads. Kit never reads `CLAUDE.md`, whatever the setting.

Discovery belongs to the session runtime on the server. Remote clients receive only bounded command metadata and do not read server-side files themselves. Add, change, or remove files and run **reload** to replace the command snapshot atomically; reload remains available during an active turn. Changing the session cwd retargets relative filesystem tools but does not implicitly replace project prompt commands; reload after moving when commands from the destination should apply.

## Template format

The filename without `.md` is the command name. Optional YAML frontmatter supplies its picker description and argument hint:

```markdown
---
description: Review recent changes
argument-hint: <scope>
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

The command palette treats text after the first space as arguments. Prompt commands are available only while the session is idle and can be run with Enter or the primary mouse button.

## Invocation record

The user message of a prompt command stores the command name, its raw arguments, its source, and the expansion made at submission. Later edits to the template do not change recorded history. The model, compaction, and plugin turn projections receive only the expanded text, exactly as for a typed prompt.

Transcripts expose the message as a `promptCommand` content block with `name`, `arguments`, `source`, and `text`. The live user message event, the follow-up queue preview, and a restored follow-up show the invocation, `/<name> <arguments>`. Submitting a restored invocation runs the command again with its current template. Messages recorded before invocation records existed remain plain text.

Discovery is bounded to 128 commands, 1,024 entries per prompt directory, 128 KiB per template, 1,024 bytes per description, and 4 KiB per absolute source location. Traversal is constrained to the resolved Kit home or explicit session cwd; symlinked prompt files and search directories are omitted.
