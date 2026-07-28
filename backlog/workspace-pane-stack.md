# Workspace pane stack limitations

The workspace secondary surface keeps a single `returnPane` slot, so the
pane "stack" is one level deep. Known consequences, accepted for now:

- Pushing a third pane (e.g. scratchpad over activity over sub-agents)
  silently drops the oldest pane from the stack.
- On a session switch, retention prefers the visible pane; a retained
  pane (scratchpad or sub-agents) sitting in the return slot is dropped
  when the other retained kind is visible, losing its local UI state
  (sub-agents roster selection/view/scroll).
- When the sub-agents plugin's panel data clears while the pane is
  minimized with a return slot, the shell clears both rather than
  promoting the return pane.

If multi-pane retention becomes a hard requirement, replace the single
`returnPane` slot with a real stack in `shell/workspace-state.ts` and
revisit the session-switch retention rules in `AppShell`.
