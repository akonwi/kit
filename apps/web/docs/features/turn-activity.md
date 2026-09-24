# Inline turn activity

Tool activity appears in the transcript as collapsed summary rows for adjacent tool work. Opening a row expands its activity directly beneath the summary instead of opening a separate workspace pane. Assistant prose remains visible in the main transcript throughout a turn.

## Grouping

- Opening, intermediate, and final assistant prose remain normal transcript entries.
- Adjacent tool-bearing and tool-only messages are grouped into activity windows.
- Activity windows may repeat associated assistant prose for context.
- Grouping remains stable when a turn finishes streaming.

## Interaction

- Activity windows are collapsed by default, including during active turns.
- Selecting the summary row toggles the inline window.
- The window has a fixed 12-row viewport and scrolls independently when its content exceeds that height.
- Selecting a tool row expands its existing detail presentation without automatically moving the row.
- Activity windows do not install keyboard navigation bindings.
