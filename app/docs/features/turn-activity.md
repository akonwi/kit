# Turn activity panel

Turn activity details open in the shell's persistent secondary workspace panel. The panel presents assistant prose, tool calls and results, bash activity, and handoff summaries while leaving the transcript available.

## Responsive behavior

- When both workspace surfaces meet their minimum useful widths, transcript and activity render side by side with the shared draggable divider.
- On narrower terminals, the workspace switches to Transcript/Activity tabs rather than opening a modal dialog.
- `F6` and `Shift+F6` move focus between the transcript and activity surfaces.
- Live turns follow new activity at the bottom; completed turns open at the top.

## Secondary-surface restoration

Activity is a temporary detail surface. If code review or another secondary panel is already open, activity is pushed over it. Closing activity restores that previous panel. The stack is intentionally limited to one return surface: opening another activity item replaces the current activity details instead of growing an arbitrary panel stack.
