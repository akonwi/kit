# 0005: Context files

## Status
Accepted

## Context

Kit needs a predictable way to discover project guidance and compose it into the system prompt.

## Decision

Kit loads guidance from context files and appends that guidance to the agent system prompt.

## Discovery rules

### Global

Kit supports one global context file:

- `~/.kit/AGENTS.md`

### Project walk-up

Starting from the session cwd and walking up through ancestor directories, Kit loads at most one context file per directory:

1. prefer `AGENTS.md`
2. otherwise fall back to `CLAUDE.md`

If both exist in the same directory, only `AGENTS.md` is used.

### Nested monorepo context

Kit also checks the immediate child directories of the session cwd for `AGENTS.md` files.

This is intended to pick up package-level guidance without doing a recursive subtree scan.

## Ordering

Context files are appended in this order:

1. `~/.kit/AGENTS.md`
2. ancestor directory context files from outermost to innermost or current
3. `AGENTS.md` files found in immediate child directories of the session cwd

This ensures more local project guidance appears later in the composed prompt.

## Related

- `AGENTS.md`
- `docs/features/context-files.md`
