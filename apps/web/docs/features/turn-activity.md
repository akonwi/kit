# Inline turn activity

Tool activity appears in the transcript as one collapsed summary row per turn. Opening the row expands its activity directly beneath the summary instead of opening a separate workspace pane.

## Grouping

- Opening assistant prose remains a normal transcript entry.
- Intermediate or pending assistant prose between tool batches appears inside the activity window.
- Final assistant prose remains a normal transcript entry.
- Tool calls from the turn are consolidated into one activity window.

## Interaction

- Activity windows are collapsed by default, including during active turns.
- Selecting the summary row toggles the inline window.
- The window has a fixed 12-row viewport and scrolls independently when its content exceeds that height.
- Selecting a tool row expands its existing detail presentation without automatically moving the row.
- Activity windows do not install keyboard navigation bindings.
