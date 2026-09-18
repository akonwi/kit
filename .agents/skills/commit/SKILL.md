---
name: commit
description: Assess staged/uncommitted changes, validate them, and create a git commit following Conventional Commits format
---

# Commit Skill

Assess the current git changes and prepare a commit following the project's standards.

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

If the changes complete or substantially address an item from `backlog/README.md`, update that backlog file before committing.

### 4. Run Validation Checks

- Read the repository guidance and run every validation required for the changed areas
- Prefer targeted checks while iterating, then run the full required checks before committing
- Verify formatting, static analysis, builds, and tests according to the project's documented commands
- Run `git diff --check` before staging the final commit

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

- Validation requirements: `AGENTS.md` Validation section
- Commit conventions: `AGENTS.md` Commit conventions section
