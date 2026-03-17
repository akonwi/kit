# Roadmap

## Phase 0 — Architecture and app boundary

- [x] Define compatibility contract
- [x] Define shell model
- [x] Establish app-first source layout
- [x] Document config/storage precedence
- [x] Capture future mutable-cwd refinement as a deferred design item

## Phase 1 — Minimal app shell

- [x] Standalone entrypoint
- [x] Fixed dock + scrollable transcript shell
- [x] Basic transcript rendering
- [x] Basic composer input surface
- [x] Initial shell styling direction aligned with Amp-like target
- [x] Debug inspection panel for transcript items

## Phase 2 — Pi compatibility baseline

- [x] Settings loading with precedence
- [x] Load Pi sessions from `~/.pi/agent`
- [x] Map session entries into transcript items
- [x] Load specific sessions via `--session` / `-s`
- [x] Append composer submissions into the active session
- [x] Keep transcript state in sync after session mutation
- [x] Add basic command/runtime bootstrap
- [x] Session switching via `/switch` command
- [x] Session management via `/sessions:manage`
- [x] Session metadata display (composer border + status bar)
- [ ] Define storage/path conventions beyond current compatibility helpers

## Phase 3 — Feature migration

- [x] Thread references — `@@` picker, `[[thread:id]]` expansion on submit
- [x] File references — `@` picker with lazy file scanning, `.gitignore`/`.pi-ignore` support
- [ ] Bash execution (`!` / `!!` prefix)
- [ ] Handoff
- [ ] Pager
- [ ] Wizard / questionnaire flow

## Phase 4 — Product refinement

- [x] Model/session UX — `/model`, `/thinking`, `/name`, `/switch`, `/sessions:manage`, `/new`, `/quit`
- [x] Command palette with filterable picker and native input fields
- [x] Reactive session metadata updates via runtime events
- [x] Markdown rendering with syntax-highlighted code blocks (tree-sitter)

- [ ] Richer overlays and picker flows
- [ ] Review flows and other custom affordances
- [ ] Better footer/status/runtime telemetry
- [ ] Better transcript rendering for tool calls and thinking

## Phase 5 — Optional extension architecture

- [ ] Internal extension points
- [ ] Evaluate whether a public UI extension model is justified
