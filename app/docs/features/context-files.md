# Guidance Sources

Kit can incorporate guidance from several different sources.

The main guidance source types are:

- context files such as `AGENTS.md` and `CLAUDE.md`
- skills
- prompt command files

## Context files

Context files provide persistent filesystem-based guidance that Kit appends to the base system prompt for the active session.

Kit loads context guidance from:

- global: `~/.kit/AGENTS.md`
- project walk-up from the session cwd:
  - `AGENTS.md` if present in a directory
  - otherwise `CLAUDE.md`
- `AGENTS.md` files in immediate child directories of the session cwd

Only one file is loaded per directory.

Files are composed in this order:

1. `~/.kit/AGENTS.md`
2. ancestor directories from outermost to innermost or current
3. `AGENTS.md` files in immediate child directories of the session cwd

Context files are attached to the system prompt, not inserted into the transcript. The active file list is visible in `/debug`, and switching sessions recomputes context files using that session's cwd.

## Skills

Skills are task-specific instruction bundles discovered from skill directories.

They are not automatically appended as general context. Instead, the model can activate a skill when a task matches that skill's description.

## Prompt command files

Prompt command files are markdown templates that become slash commands.

They are not part of the always-on system prompt guidance. Instead, they provide reusable user-invoked prompt templates.

## How to access it

Context files are automatic.

To use them, place guidance in files such as:

- `~/.kit/AGENTS.md`
- project `AGENTS.md`
- project `CLAUDE.md`

Skills and prompt command files are documented separately in:

- `docs/features/skills.md`
- `docs/features/prompt-commands.md`
