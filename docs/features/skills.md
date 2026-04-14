# Skills

## Status

Available now.

## What they do

Skills are markdown knowledge files that provide specialized instructions for
specific tasks. They are passive content — the model activates them on demand
via the `activate_skill` tool when a task matches a skill's description.

## Discovery

Kit scans these directories for skills (first-loaded wins on name collisions):

1. `~/.kit/skills/` — user-global
2. `.agents/skills/` — project-local (relative to session cwd)
3. `~/.pi/agent/skills/` — Pi compatibility

### Directory structure

A skill is a directory containing a `SKILL.md` file:

```
.agents/skills/
  my-skill/
    SKILL.md          # Required — entry point with frontmatter
    references/       # Optional — additional reference files
      guide.md
```

If a directory contains `SKILL.md`, it is treated as a skill root and Kit does
not recurse further into it. Otherwise, Kit recurses into subdirectories to find
`SKILL.md` files.

Dotfiles, `node_modules`, and paths matching `.gitignore` / `.ignore` are skipped.

## SKILL.md format

YAML frontmatter followed by markdown content:

```markdown
---
name: my-skill
description: What this skill does
disable-model-invocation: false
---

# My Skill

Instructions the model follows when this skill is activated.
```

### Frontmatter fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | No | Skill name. Must be lowercase `a-z`, `0-9`, hyphens. Defaults to parent directory name. |
| `description` | Yes | What the skill does. Max 1024 characters. |
| `disable-model-invocation` | No | If `true`, skill is hidden from the system prompt and cannot be activated by the model. Default `false`. |

### Name validation (per Agent Skills spec)

- Must match the parent directory name
- Max 64 characters
- Lowercase `a-z`, `0-9`, hyphens only
- Must not start or end with a hyphen
- Must not contain consecutive hyphens

## How activation works

1. At startup, the `SkillsPlugin` discovers all skills and lists them in the
   system prompt as XML (`<available_skills>`)
2. The model sees each skill's name, description, and file location
3. When a task matches, the model calls `activate_skill({ name: "my-skill" })`
4. The tool returns the full `SKILL.md` content
5. The model follows the skill's instructions, using `read` for any referenced
   sub-files the skill points to

## Debugging

Run `/debug` to see all discovered skills with their source and file path.

## Source

- `src/features/skills/discovery.ts`
- `src/features/skills/format.ts`
- `src/features/skills/tool.ts`
- `src/features/skills/index.ts`
