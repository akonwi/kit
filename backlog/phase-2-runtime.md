# Phase 2 Runtime Backlog

These are the highest-priority items needed to move from a read-only session viewer to a functioning coding-agent app.

## Immediate next milestone: continue a real session

- [ ] Keep the loaded `SessionManager` available in app/runtime state
- [ ] Track composer text in app state
- [ ] On submit, append a Pi-compatible user message to the active session
- [ ] Regenerate transcript from the updated session branch
- [ ] Clear composer after successful submit

## Command path

- [ ] Detect slash commands before normal message submission
- [ ] Add initial command handler scaffold
- [ ] Support a small first set of commands:
  - [ ] `/new`
  - [ ] `/session`
  - [ ] `/model`
  - [ ] `/thinking`

## Agent runtime

- [ ] Define backend-facing runtime abstraction for running a turn
- [ ] Send the active session context to the model
- [ ] Show live runtime activity for thinking/tool events in the panel above the composer without streaming assistant prose
- [ ] Commit the final assistant message atomically once complete
- [ ] Append tool calls / tool results into the session in Pi-compatible form
- [ ] Update footer/runtime state from real session/runtime data

## Session UX

- [ ] Session picker / recent session list
- [ ] Switch the active session without restarting the app
- [ ] Branch navigation and branch switching
- [ ] Surface session metadata in a cleaner way than raw debug inspection
