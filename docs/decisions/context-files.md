# Context Files

- Status: Accepted
- Date: 2026-04-08

## Decision

Kit loads project guidance from context files and appends that guidance to the
agent system prompt.

## Discovery rules

### Global

Kit supports one global context file:

- `~/.kit/AGENTS.md`

There is no global `CLAUDE.md` support.

### Project walk-up

Starting from the session cwd and walking up through ancestor directories, Kit
loads at most one context file per directory:

1. prefer `AGENTS.md`
2. otherwise fall back to `CLAUDE.md`

If both exist in the same directory, only `AGENTS.md` is used.

### Nested monorepo context

For monorepo-style repositories, Kit also checks the immediate child
directories of the session cwd for `AGENTS.md` files.

This is intended to pick up package-level guidance without doing a full
recursive subtree scan.

## Ordering

Context files are appended in this order:

1. `~/.kit/AGENTS.md`
2. ancestor directory context files from outermost to innermost/current
3. `AGENTS.md` files found in immediate child directories of the session cwd

This means more local project guidance appears later in the composed prompt.

## Non-goals for now

Kit does not currently implement Pi's broader resource-loader model for:

- `SYSTEM.md`
- `APPEND_SYSTEM.md`
- package-provided context files

## Visibility

Loaded context file paths should be visible in `/debug`, but should not produce
startup toasts.
