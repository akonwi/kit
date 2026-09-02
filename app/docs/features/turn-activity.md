# Turn activity panel

Turn activity details open in the shell's persistent secondary workspace panel. The panel presents assistant prose, tool calls and results, bash activity, and handoff summaries while leaving the transcript available. Assistant prose remains visible in the main transcript throughout a turn; activity drawers primarily consolidate tool work and may repeat associated intermediate prose for context.

## Responsive behavior

- When both workspace surfaces meet their minimum useful widths, transcript and activity render side by side with the shared draggable divider.
- On narrower terminals, the workspace switches to Transcript/Activity tabs rather than opening a modal dialog.
- `Tab` and `Shift+Tab` cycle through Transcript and open workspace tabs.
- Live turns follow new activity at the bottom; completed turns open at the top.

## Workspace tab behavior

Activity uses one singleton workspace tab. Opening a tool drawer selects that tab and updates it to the requested activity, even when another workspace tab is active. Other tabs remain open and retain their state. Opening another activity item replaces the singleton tab's details rather than creating another Activity tab.
