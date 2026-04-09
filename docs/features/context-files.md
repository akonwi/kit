# Context Files

## Status

Available now.

## What they do

Context files provide persistent guidance that Kit appends to the base system
prompt for the active session.

This is intended for:

- repository conventions
- coding standards
- workflow reminders
- environment-specific guidance

## Discovery rules

Kit loads context guidance from:

- global: `~/.kit/AGENTS.md`
- project walk-up from the session cwd:
  - `AGENTS.md` if present in a directory
  - otherwise `CLAUDE.md`

Only one file is loaded per directory.

## Ordering

Files are composed in this order:

1. `~/.kit/AGENTS.md`
2. ancestor directories from outermost to innermost/current

## Notes

- context files are attached to the system prompt, not inserted into the transcript
- no startup toast is shown when context files are loaded
- the active file list is visible in `/debug`
- switching sessions recomputes context files using that session's cwd

## Source

- `src/context/agents.ts`
- `src/runtime/agent-runtime.ts`
- `src/features/commands/session.ts`
