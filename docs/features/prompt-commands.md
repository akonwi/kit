# Prompt Commands

Prompt commands are markdown files that become slash commands. Each `.md` file
in a prompts directory registers a `/filename` command. When invoked, the file's
content is expanded with any arguments and submitted as a user message.

## Discovery

Kit scans these directories for `.md` files (non-recursive, first-loaded wins
on name collisions):

1. `~/.kit/prompts/` — user-global
2. `.agents/prompts/` — project-local (relative to session cwd)
3. `~/.pi/agent/prompts/` — Pi compatibility

## File format

Optional YAML frontmatter followed by the template body:

```markdown
---
description: Review recent code changes
---
Review the following and provide feedback: $@
```

### Frontmatter fields

| Field | Description |
|-------|-------------|
| `description` | Shown in the slash command picker. If omitted, the first line of the body is used. |

### Argument substitution

The template body supports bash-style argument placeholders:

| Placeholder | Description |
|-------------|-------------|
| `$1`, `$2`, ... | Positional arguments (1-indexed) |
| `$@` | All arguments joined with spaces |
| `$ARGUMENTS` | Same as `$@` |
| `${@:N}` | Arguments from Nth onwards (1-indexed) |
| `${@:N:L}` | L arguments starting from Nth |

Arguments are parsed respecting quoted strings:

```
/review "the auth module" carefully
```

- `$1` → `the auth module`
- `$2` → `carefully`
- `$@` → `the auth module carefully`

## Example

### `.agents/prompts/review.md`

```markdown
---
description: Review recent code changes
---
Review the recent code changes and provide feedback. $@
```

Usage: `/review focus on error handling`

Submits: `Review the recent code changes and provide feedback. focus on error handling`

## Debugging

Run `/debug` to see all discovered prompt commands with their source and file path.
