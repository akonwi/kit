# Phase 2 Runtime Backlog

## Immediate next milestone: continue a real session [done]

- [x] Keep the loaded `SessionManager` available in app/runtime state
- [x] Track composer text in app state
- [x] On submit, append a Pi-compatible user message to the active session
- [x] Regenerate transcript from the updated session branch
- [x] Clear composer after successful submit

## Command path [done]

- [x] Detect slash commands before normal message submission
- [x] Add initial command handler scaffold
- [x] Support a small first set of commands:
  - [x] `/new`
  - [x] `/switch`
  - [x] `/model` (cycles to next available model)
  - [x] `/thinking` (cycles or sets by name)
  - [x] `/name <name>` (set session display name)
  - [x] `/sessions:manage` (rename/delete sessions)
  - [x] `/quit`

## Agent runtime [done]

- [x] Define backend-facing runtime abstraction for running a turn
- [x] Send the active session context to the model
- [x] Show live runtime activity for thinking/tool events in the panel above the composer without streaming assistant prose
- [x] Commit the final assistant message atomically once complete
- [x] Append tool calls / tool results into the session in Pi-compatible form
- [x] Update footer/runtime state from real session/runtime data
- [x] Emit `tool_completed` events for file index invalidation

## Session UX [done]

- [x] Switch the active session via `/switch` command
- [x] Session management via `/sessions:manage` (rename/delete)
- [x] Session metadata in composer border + status bar
- ~~Branch navigation~~ — replaced by `/handoff` (see feature-migration.md)
