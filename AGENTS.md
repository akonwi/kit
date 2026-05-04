# AGENTS.md

## Project identity

Kit is a standalone terminal-first coding agent.

## Design language

Kit's UI design language is documented in `.agents/skills/design/SKILL.md`. All UI work — new components, views, screens, and modifications to existing ones — must follow the conventions defined there. This covers the color palette, overlay hierarchy, layout patterns (ScreenLayout, ScreenHeader, HintBar), and component conventions.

## Implementation guidance

- Favor modular layers such as:
  - `runtime/`
  - `shell/`
  - `features/`
- Extract reusable logic from older Pi-era code where useful, but do not carry over extension-era structure unless it still serves the standalone app.
- Keep renderer-specific assumptions isolated from domain and compatibility logic.
- Do not take architectural shortcuts that bypass an agreed interface or design just to get something working quickly.
- Once we have settled on an interface or abstraction, implement through that interface unless we explicitly re-open the design decision.
- When we make an architectural or feature design decision, capture it under `docs/`.
- When we identify outstanding work or defer something, capture it under `backlog/`.

## Commit conventions

- Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages.
- Preferred format: `type(scope): summary`
- Scope is optional when it does not add clarity: `type: summary`
- Keep summaries concise and imperative.
- Common types in this repo:
  - `feat`
  - `fix`
  - `refactor`
  - `docs`
  - `test`
  - `chore`
  - `perf`
  - `build`
  - `ci`

## Pre-commit checklist

Before asking to commit, always:

1. **`bun run typecheck`** — zero TypeScript errors required
2. **`bun run check`** — auto-fix formatting and safe lint fixes
3. **Address remaining biome warnings** — fix or suppress with `// biome-ignore <rule>: <reason>`
   - `noExplicitAny`: replace with proper types where possible
   - `noNonNullAssertion`: prefer Solid child accessor pattern or type narrowing over `!`
   - `noUnusedImports` / `noUnusedVariables`: remove dead code
   - Do **not** suppress warnings without a clear reason in the comment
4. **Re-run `bun run typecheck`** after biome changes to confirm nothing broke
