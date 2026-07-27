# Scratchpad

The scratchpad is a persistent, session-scoped notes editor in the shell's secondary workspace panel. Its contents are included as read-only context for agent turns and autosaved through the scratchpad controller.

## Responsive behavior

- When both workspace surfaces meet their minimum useful widths, transcript and scratchpad render side by side with the shared draggable divider.
- On narrower terminals, the workspace switches to Transcript/Scratchpad tabs. The scratchpad does not open as a modal dialog.
- Workspace focus commands move between the transcript and scratchpad. The scratchpad keymap is active only while its surface owns focus.

## Surface restoration

Opening the scratchpad over another secondary surface temporarily replaces that surface. Closing the scratchpad restores the previous surface. When activity details temporarily cover the scratchpad, the scratchpad remains mounted so its textarea cursor and editing state are preserved.
