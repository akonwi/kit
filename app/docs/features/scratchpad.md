# Scratchpad

The scratchpad is a persistent, session-scoped notes editor in the shell's secondary workspace panel. Its contents are included as read-only context for agent turns and autosaved through the scratchpad controller.

## Agent updates

Interactive agents can propose changes with the built-in `update_scratchpad` tool. The tool supports appending content (the default) or replacing the complete scratchpad, including clearing it. Every effective change opens a scrollable confirmation showing the exact proposed content and defaults to keeping the current scratchpad. The controller saves the change only after the user approves it.

Agent-proposed updates are limited to a 32,000-character resulting scratchpad. Approved appends rebase onto edits made while approval is open; replacements are rejected if the scratchpad changed. Session switches, cancelled turns, and plugin reloads cancel pending updates.

The tool is unavailable in headless mode because approval requires the terminal UI. Agents should use it only for information worth retaining across later turns in the current session; the tool handles approval itself, so a separate confirmation is unnecessary.

## Responsive behavior

- When both workspace surfaces meet their minimum useful widths, transcript and scratchpad render side by side with the shared draggable divider.
- On narrower terminals, the workspace switches to Transcript/Scratchpad tabs. The scratchpad does not open as a modal dialog.
- Workspace focus commands move between the transcript and scratchpad. The scratchpad keymap is active only while its surface owns focus.

## Surface restoration

Opening the scratchpad over another secondary surface temporarily replaces that surface. Closing the scratchpad restores the previous surface. When activity details temporarily cover the scratchpad, the scratchpad remains mounted so its textarea cursor and editing state are preserved.
