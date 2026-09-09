# Skills

Skills are Markdown instruction bundles that Kit advertises to the model and loads on demand through `activate_skill`.

## Discovery locations and precedence

For each session runtime, Kit discovers skills in this order:

1. the embedded, reserved `kit-customization` skill;
2. user-global skill directories under the resolved Kit home's `skills/` directory; and
3. project skill directories under `<session-cwd>/.agents/skills/`.

During v2 development the default global directory is `~/.kit-v2/skills/`. `KIT_HOME` changes that location. Project discovery uses the session's explicit cwd and never process-global cwd.

The first definition of a name wins. A project skill cannot override a global skill, and no filesystem skill can override `kit-customization`. Omitted collisions produce reload diagnostics.

## Directory and file format

A skill is a directory containing an exact `SKILL.md` file. Discovery recursively visits ordinary subdirectories in deterministic name order, skipping hidden directories, `node_modules`, and symlinks. Once a directory containing `SKILL.md` is found, Kit treats it as a skill root and does not recurse below it.

A discovered `SKILL.md` uses YAML frontmatter:

```markdown
---
name: review
description: Review recent code changes for correctness
disable-model-invocation: false
---

# Review instructions
...
```

`name` defaults to the skill directory's name and must match that directory. Names use lowercase letters, digits, and single hyphens. `description` is required. Setting `disable-model-invocation: true` keeps the skill out of the model-visible catalog.

The catalog contains each visible skill's absolute `SKILL.md` location. When activated, Kit returns the complete file. Relative paths in its instructions are resolved against the skill directory.

## Lifecycle and diagnostics

Discovery occurs when a session runtime is first loaded and on explicit reload. Add, remove, or edit skills, then run **reload** from the native command palette to update both the advertised catalog and `activate_skill` atomically. Reload remains available during active work; an in-flight provider request keeps its captured catalog and the next request uses the replacement. Changing the session cwd retargets relative filesystem tools but does not implicitly replace the skill registry; reload after moving when project skills from the destination should apply.

Unreadable, malformed, invalid, duplicate, reserved, oversized, escaped, and over-limit definitions are omitted with bounded structured diagnostics. One skill file is limited to 256 KiB, a registry to 128 skills including the embedded skill, and the model-visible catalog to 256 KiB.
