# Skills

Skills are markdown knowledge files that provide specialized instructions for specific tasks.

They are passive content: the model activates them on demand with the `activate_skill` tool when a task matches a skill's description.

Kit discovers skills from these locations, with first-loaded wins on name collisions:

1. `~/.kit/skills/`
2. `.agents/skills/`
3. `~/.pi/agent/skills/`

A skill is a directory containing a `SKILL.md` file. If a directory contains `SKILL.md`, it is treated as a skill root and Kit does not recurse further into it.

`SKILL.md` uses YAML frontmatter plus markdown body content. The frontmatter can define fields such as:

- `name`
- `description`
- `disable-model-invocation`

Current behavior:

- discovered skills are listed in the system prompt
- the model sees each skill's name, description, and file location
- when a task matches, the model can call `activate_skill({ name })`
- the tool returns the full `SKILL.md` content
- the model can then follow that skill's instructions and read referenced files if needed

Discovered skills are visible in `/debug`.

## How to access it

Skills are accessed through skill discovery and model activation.

To add a skill, create a skill directory containing `SKILL.md` in one of the supported skill locations.
