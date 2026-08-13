# Scratchpad

The scratchpad is a persistent, session-scoped Markdown file shown in the shell's secondary workspace panel. Its contents and absolute file path are included as context for agent turns, and edits made in the panel are autosaved through the scratchpad controller.

## Agent updates

The active scratchpad is stored at `~/.kit/sessions/<session-id>.scratchpad.md`. Agents use `edit_scratchpad` for targeted replacements. The tool has the same exact-match and overlap validation as `edit`, but accepts no path and resolves the active session scratchpad when each call begins. It creates a missing scratchpad and accepts one edit with an empty `oldText` to initialize empty content. This prevents inherited tool history from directing a forked or handed-off session back to its parent's scratchpad. Agents can still use `read` to inspect the file or `write` when a full replacement is necessary.

The controller creates the file even when the scratchpad is empty. Normal file operations targeting it are routed through the scratchpad's guarded mutation layer: pending panel edits are flushed first, and successful reads or writes immediately synchronize the panel and subsequent agent context. Session forks continue to copy the parent scratchpad into an empty child scratchpad.

## Responsive behavior

- When both workspace surfaces meet their minimum useful widths, transcript and scratchpad render side by side with the shared draggable divider.
- On narrower terminals, the workspace switches to Transcript/Scratchpad tabs. The scratchpad does not open as a modal dialog.
- Workspace focus commands move between the transcript and scratchpad. The scratchpad keymap is active only while its surface owns focus.

## Surface restoration

Opening the scratchpad over another secondary surface temporarily replaces that surface. Closing the scratchpad restores the previous surface. When activity details temporarily cover the scratchpad, the scratchpad remains mounted so its textarea cursor and editing state are preserved.
