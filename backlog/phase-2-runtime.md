# Phase 2 Runtime Backlog

These are the highest-priority items needed to move from a read-only session viewer to a functioning coding-agent app.

## Immediate next milestone: continue a real session

- [x] Keep the loaded `SessionManager` available in app/runtime state
- [x] Track composer text in app state
- [x] On submit, append a Pi-compatible user message to the active session
- [x] Regenerate transcript from the updated session branch
- [x] Clear composer after successful submit

## Command path

- [x] Detect slash commands before normal message submission
- [x] Add initial command handler scaffold
- [x] Support a small first set of commands:
  - [x] `/new`
  - [x] `/session`
  - [x] `/model` (cycles to next available model)
  - [x] `/thinking` (cycles or sets by name)
  - [x] `/name <name>` (set session display name)

## Agent runtime

- [x] Define backend-facing runtime abstraction for running a turn
- [x] Send the active session context to the model
- [x] Show live runtime activity for thinking/tool events in the panel above the composer without streaming assistant prose
- [x] Commit the final assistant message atomically once complete
- [x] Append tool calls / tool results into the session in Pi-compatible form
- [x] Update footer/runtime state from real session/runtime data

## Session UX

- [ ] Session picker / recent session list
- [ ] Switch the active session without restarting the app
- [ ] Branch navigation and branch switching
- [ ] Surface session metadata in a cleaner way than raw debug inspection
