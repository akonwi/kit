---
name: commit
description: Review staged/uncommitted changes, validate them, and create a git commit following Conventional Commits format
---

# Commit Skill

Review the current git changes and prepare a commit following the project's standards.

## When to Activate

Activate this skill when the user asks to commit changes, prepare a commit, or review what to commit.

## Commit Process

### 1. Inspect the Changes

- Run `git status` and `git diff` (staged and unstaged) to understand what changed
- Check for unrelated changes and ask before including them if the working tree is mixed
- If there is nothing to commit, say so

### 2. Summarize What Changed

Provide a clear summary of the changes in the working tree.

### 3. Update Backlog if Needed

If the changes complete or substantially address an item from `backlog/backlog.md`, update that backlog file before committing.

### 4. Run Validation Checks

Run the required checks for this repo:

- `bun run typecheck` — zero TypeScript errors required
- `bun run check` — auto-fix formatting and safe lint fixes
- Address remaining Biome warnings (fix or suppress with `// biome-ignore <rule>: <reason>`)
- Re-run `bun run typecheck` after Biome changes to confirm nothing broke
- For one-shot/headless changes: `bun run smoke:one-shot` from repo root

### 5. Fix Issues

If checks fail, fix the issues when appropriate and re-run the necessary checks until everything passes.

### 6. Create a Commit Message

Write a concise, accurate [Conventional Commit](https://www.conventionalcommits.org/) message:

- Preferred format: `type(scope): summary`
- Use `type: summary` when scope does not add clarity
- Keep the summary imperative and specific
- Common types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `build`, `ci`

### 7. Commit

Commit the changes with the prepared message.

## References

- Pre-commit checklist: `AGENTS.md` Pre-commit checklist section
- Commit conventions: `AGENTS.md` Commit conventions section
