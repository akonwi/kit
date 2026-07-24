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
- Check for any related context in `backlog/backlog.md` or `docs/`

### 2. Activate Relevant Skills

Based on what the changes touch, activate the corresponding skills for context:

| Changes involve | Activate skill |
|---|---|
| TUI components, views, rendering | `opentui` |
| UI layout, colors, shell components | `design` |
| Component architecture, prop APIs | `composition-patterns` |
| Release preparation | `release` |

### 3. Review Against Project Standards

Check for these common issues, organized by category:

#### TypeScript & Correctness
- Type errors (run `bun run typecheck`)
- `any` types that could be narrowed
- Non-null assertions (`!`) that could be avoided with proper narrowing
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
- Colors sourced from `src/shell/theme.ts`, no hardcoded hex
- Glyphs from `src/shell/glyphs.ts`, no inline Unicode
- Proper use of `ScreenLayout`, `ScreenHeader`, `HintBar`
- Correct overlay tier (Transient / Dialog / Screen)
- Consistent text hierarchy (`textPrimary`, `textSecondary`, `textMuted`)
- HintBar usage over bare keyboard hint text

#### Architecture (when relevant)
- Follows modular layers: `runtime/`, `shell/`, `features/`
- Renderer-specific assumptions isolated from domain logic
- Changes follow established interfaces and abstractions
- Design decisions captured in `docs/` when appropriate
- Outstanding work captured in `backlog/`

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
2. Run pre-commit checks:
   - `bun run typecheck` — zero TypeScript errors
   - `bun run check` — auto-fix formatting and safe lint fixes (run from the `app/` directory)
   - Address remaining Biome warnings (suppress with `// biome-ignore <rule>: <reason>`)
   - Re-run `bun run typecheck` after Biome changes
   - For one-shot/headless changes: `bun run smoke:one-shot` from repo root
3. Prepare a summary of what was changed

## References

- Project guidance: `AGENTS.md` (root)
- Design language: `.agents/skills/design/SKILL.md`
- Pre-commit checklist: AGENTS.md Pre-commit checklist section
