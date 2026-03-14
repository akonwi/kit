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

- [x] Settings loading with precedence:
  - [x] `~/.pi-kit/settings.json`
  - [x] fallback Pi settings
  - [x] built-in defaults
- [x] Load Pi sessions from `~/.pi/agent`
- [x] Map session entries into transcript items
- [x] Load specific sessions via `--session` / `-s`
- [ ] Append composer submissions into the active session
- [ ] Keep transcript state in sync after session mutation
- [ ] Add session switching UX beyond CLI boot arg
- [ ] Add branch/tree navigation UX
- [ ] Add basic command/runtime bootstrap
- [ ] Define storage/path conventions beyond current compatibility helpers

## Phase 3 — Feature migration

- [ ] Pager
- [ ] Wizard / questionnaire flow
- [ ] Thread references
- [ ] Handoff
- [ ] Ignore-file workflows

## Phase 4 — Product refinement

- [ ] Model/session UX
- [ ] Richer overlays and picker flows
- [ ] Improved command palette
- [ ] Review flows and other custom affordances
- [ ] Better footer/status/runtime telemetry
- [ ] Better transcript rendering for distinct message content types

## Phase 5 — Optional extension architecture

- [ ] Internal extension points
- [ ] Evaluate whether a public UI extension model is justified
