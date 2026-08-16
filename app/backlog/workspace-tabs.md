# Workspace tabs

Status: TUI implemented; web implemented for Activity, Code Review, and Scratchpad. Remaining TUI surfaces are tracked separately.

## Summary

Treat files, reviews, scratchpads, activity, and other auxiliary views as workspace
surfaces. The transcript remains the primary surface.

The same workspace state has two responsive presentations:

- on wide terminals, workspace surfaces appear as tabs in a collapsible secondary
  drawer beside the transcript
- on narrow terminals, the drawer disappears and Transcript plus every open
  workspace surface become peers in one top-level selector

The workspace owns navigation and presentation. Each surface owns its content and
any optional context/header row below the tab strip. The typed extension contract
is documented in [`../docs/features/workspace-panes.md`](../docs/features/workspace-panes.md).

See the accepted mockups:

- [`workspace-tabs-design.html`](./workspace-tabs-design.html) — terminal UI
- [`workspace-tabs-web-design.html`](./workspace-tabs-web-design.html) — web UI

## Lifetime and identity

- Every opened tab remains available until explicitly closed, except Code Review
  and Scratchpad, which remain process-persistent after first opening.
- Reopening an existing surface identity selects that tab instead of duplicating
  it.
- Tab state lasts only for the lifetime of the Kit process; it is not serialized
  into the session.
- Switching sessions resets the workspace tabs.
- Activity is one singleton tab. It follows the selected turn and updates silently
  while inactive.
- Closing a tab discards its local view state.
- Current editable surfaces autosave or retain drafts in memory, so there is no
  universal dirty state or close confirmation.

## Wide presentation

- Show the tab strip whenever at least one workspace tab exists.
- Keep tabs in opening order; selection does not reorder them.
- The tab track scrolls horizontally rather than collapsing tabs into a picker.
- Selecting an off-screen tab scrolls it into view.
- Fixed directional indicators such as `‹ 2` and `3 ›` report tabs hidden beyond
  each edge and scroll the strip when activated.
- The drawer can be collapsed without closing its tabs.
- The expanded tab strip has an explicit collapse control.
- A collapsed drawer rail is visible from startup. Before any tabs exist, its
  expand action opens Scratchpad as the default workspace surface.
- The rail is the secondary panel's narrowest flex-layout width, not an overlay
  on the transcript. Dragging the divider to the panel's minimum useful width
  snaps it into this collapsed rail without replacing the last committed
  expanded width. The rail centers its expand affordance
  vertically and highlights the full target on hover.
- Opening any workspace surface expands the drawer and selects its tab.

## Narrow presentation

- Transcript is pinned first and workspace tabs follow in opening order.
- The selected surface occupies the main view; there is no separate drawer.
- Tabs that fit remain direct selectors.
- Tabs that do not fit collapse behind `+N`, which opens a searchable surface
  picker.
- Selecting Transcript does not close or reset workspace surfaces.
- Crossing the responsive breakpoint reprojects the same state without resetting
  tab order, selection, drafts, scroll positions, or drawer state.

## Closing

- `×` is the explicit close action for closable tabs.
- Code Review and Scratchpad do not expose `×` and ignore
  `workspace.close-tab`; selecting Transcript or collapsing the drawer hides them
  without discarding their state.
- Closing the final closable workspace tab removes the wide drawer or returns
  narrow mode to Transcript when no persistent tabs remain.
- Add a `workspace.close-tab` command with no default keybinding so users can bind
  it or invoke it from the command palette for closable tabs.
- `Escape` returns to Transcript without closing a tab.

## Keyboard navigation

- `Tab` selects the next surface.
- `Shift+Tab` selects the previous surface.
- Navigation wraps through Transcript and the open workspace tabs in display
  order.
- Pickers, the command palette, and modal interactions may capture Tab locally.
- Review keeps next/previous change-group commands, with `]` and `[` as their
  defaults instead of Tab and Shift+Tab.
- F6 is no longer the default workspace-cycling binding.

## Web presentation

The browser uses the same workspace state and responsive model with pointer-first
controls:

- use larger tab, close, overflow, collapse, and edge-handle targets with clear
  hover and pointer states
- support horizontal wheel/trackpad scrolling in the wide drawer tab strip
- keep simple directional overflow indicators fixed at the tab-strip edges; unlike
  the TUI, web indicators do not show hidden-tab counts
- use the collapsed right-edge handle as a full-height click target
- on narrow screens, open `+N` as a searchable touch-friendly surface sheet
- keep tab order stable when selecting a surface from the sheet
- use standard ARIA tab semantics and roving focus
- preserve normal browser Tab focus traversal; focused tablists use arrow keys
  rather than the TUI's global Tab/Shift+Tab behavior
- expose close actions as real labeled buttons and keep resize controls keyboard
  accessible even though pointer interaction is primary

## Non-goals for the first implementation

- disk persistence across Kit restarts
- preserving tabs across session switches
- drag reordering
- a universal dirty-tab protocol
- multiple Activity tabs
