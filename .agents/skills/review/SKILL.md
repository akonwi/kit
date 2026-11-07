---
name: review
description: Review recent code changes, provide structured feedback, and optionally fix issues found. Use when asked to review code, review a diff, review recent changes, or audit the working tree.
---

# Review Skill

Review recent code changes and provide structured, actionable feedback. Follow the process below for consistent, high-quality reviews.

## When to Activate

Activate this skill when the user says any variation of:
- "review these changes" / "review the diff"
- "review my code" / "review recent changes"
- "audit" / "code review"
- "check this PR" / "review these commits"

## Review Process

### 1. Inspect the Changes

- Check `git status`, `git diff` (staged and unstaged), and recent commits
- Understand the scope: what files changed, what the change intends to do
- Check for any related context in GitHub issues (`gh issue list`) or `docs/`

### 2. Activate Relevant Skills

Based on what the changes touch, activate the corresponding skills for context:

| Changes involve | Activate skill |
|---|---|
| TUI components, views, rendering | `opentui` |
| UI layout, colors, shell components | `design` |
| Component architecture, prop APIs | `composition-patterns` |
| Release preparation | `release` |
| Concurrent Go: goroutines, locks, channels, shutdown, timeouts | `go-concurrency` |

### 3. Review Against Project Standards

Check for these common issues, organized by category:

#### Correctness
- Build, type-check, and vet errors in the changed languages
- Unchecked casts, force unwraps, or ignored errors that proper handling would avoid
- Unused imports, variables, or parameters
- Missing error handling or edge cases

#### Code Quality
- Dead code or commented-out code — remove it
- Duplicate logic that should be extracted
- Clear naming and consistent patterns
- Overly complex conditionals — simplify where possible
- Magic numbers or strings — prefer named constants

#### UI/TUI (when relevant)
- Follows the design language in `design` skill
- Colors from semantic theme tokens, no hardcoded colors
- Glyphs used with the meanings the design skill lists
- Correct overlay tier (Transient / Dialog / Screen)
- Consistent text hierarchy (`textPrimary`, `textSecondary`, `textMuted`)

#### Architecture (when relevant)
- Follows the ownership rules in AGENTS.md's Architecture rules
- Renderer-specific assumptions isolated from domain logic
- Changes follow established interfaces and abstractions
- Design decisions captured in `docs/` when appropriate
- Outstanding work captured as GitHub issues

#### Commit Quality
- Changes are logically grouped (not too large, not too granular)
- Commit messages follow Conventional Commits format
- Message accurately reflects what changed and why

### 4. Present Feedback

Structure your feedback clearly:
- **Summary** — what changed and what it accomplishes
- **Positive** — what was done well
- **Issues** — problems that need fixing, ordered by severity
- **Suggestions** — optional improvements

### 5. Offer to Fix

After presenting the review, ask the user if they want you to address the issues found.

If they confirm:
1. Fix each issue, keeping changes focused
2. Run the validation AGENTS.md lists for each area changed (Go, `apps/cli`,
   the macOS and iOS apps)
3. Prepare a summary of what was changed

## References

- Project guidance: `AGENTS.md` (root)
- Design language: `.agents/skills/design/SKILL.md`
- Pre-commit checklist: AGENTS.md Pre-commit checklist section
