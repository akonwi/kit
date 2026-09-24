---
name: design
description: Kit's UI design language and visual standards. Use when building or modifying any UI component, view, screen, overlay, or workspace pane. Covers themes, surface hierarchy, workspace layout, interaction, and component conventions.
---

# Kit Design Language

Kit's UI is **utilitarian, pleasant, and intuitive**. It should communicate state and available actions clearly without drawing attention to decoration. Every visual choice should improve comprehension, navigation, or feedback.

The legacy reference UI lives in `apps/web/src/`; the native v2 TUI implements the same semantic language with `vaxis/ui`. Browser-specific presentations may reuse the principles while using renderer-appropriate primitives.

## Root Shell Boundary

The v2 TUI is **viewport-native**. The terminal viewport is the application's
outer boundary; the root shell must not draw a complete enclosing border.

- Render global header, content, composer, and status regions edge to edge.
- Use full-width horizontal separators where adjacent regions need structure.
- Do not spend the first/last terminal columns or rows on decorative outer
  `│`, corner, top, or bottom border cells.
- Paint the full viewport background so edge-to-edge presentation looks
  intentional when the Kit theme differs from the terminal background.
- Dialogs, focused controls, and local semantic regions may still be framed.
  Viewport-native applies only to the root application boundary.
- Center root empty/auth content within the measured content region, not the
  raw viewport, so optional workspace regions do not offset it.
- Keep the root boundary decision in the shell wrapper. Child screens and
  panes must not recreate a full-screen frame.

This lets Kit inhabit a terminal or multiplexer pane without drawing a second
box inside its existing boundary, preserves two columns and rows, and reduces
border junction and resize artifacts.

## Root Shell Chrome

Preserve the established shell information layout while removing only the
outer frame:

- The top-left header owns the attached session name.
- The top-right header owns model and thinking settings, context usage, and
  conditional global contributions such as update or new-release indicators.
- Present context usage as a bare percentage inside the model-information
  cluster, such as `41%`; do not draw the main-branch colored progress bar.
- Do not add an elapsed-turn timer to the header.
- The bottom-left footer owns transient status and guidance, such as queue,
  retry, compaction, bash mode, or other actionable run state.
- The bottom-right footer owns the current working directory and Git/VCS
  information.
- Workspace hints remain inside the workspace pane that owns them; do not move
  them into global header metadata.
- Global chrome describes only the attached session. Subagent status remains in
  Agent activity, notifications, the modal subagent picker, and explicitly
  opened conversation tabs.
- Width-aware contribution packing may hide lower-priority items, but it must
  preserve these ownership roles and use labeled overflow.

Show context percentage when session context exists and the value is available;
omit an empty-session `0%` or unavailable value. Use `progressNormal`,
`progressWarning`, and `progressCritical` on the percentage text at the existing
thresholds. The header separator remains structural and does not visualize
context progress.

## Theme System

Kit does not have a fixed application palette. The system theme is derived at runtime from the terminal's foreground, background, cursor, and ANSI colors. User themes may override semantic tokens from `~/.kit/themes/`.

The relevant sources are:

- `apps/web/src/shell/themes/types.ts` — `ThemeTokens`, `SyntaxPalette`, and user-theme shapes
- `apps/web/src/shell/themes/system.ts` — terminal-derived system theme and fallback palette
- `apps/web/src/shell/theme.ts` — reactive `theme` store, `syntaxStyle()`, and `scrollbarStyle()`

### Semantic token roles

Use semantic tokens rather than assuming a literal color:

| Role | Common tokens |
|---|---|
| Base surfaces | `bg`, `bgSurface`, `bgMuted`, `bgAccent`, `bgTransparent` |
| Borders | `borderDefault`, `borderFocused`, `borderAccent` |
| Text hierarchy | `textPrimary`, `textSecondary`, `textMuted`, `textPlaceholder` |
| Status | `toolText`, `errorText`, `warningText`, `metaText` |
| Feature identity | `reviewText`, `subagentText`, `attachmentText`, `debugLabel` |
| Pickers | `pickerBg`, `pickerFocusedBg`, `pickerFocusedText`, `pickerItemText` |
| Progress | `progressNormal`, `progressWarning`, `progressCritical` |
| Diffs | the `diffAdded*`, `diffRemoved*`, and `diffCursor*` families |

### Rules

- Read colors from the reactive `theme` store inside JSX. Do not hardcode resolved color values in components.
- Name new tokens by semantic purpose, not hue.
- Reuse an existing token when its role fits. Add a token only for a genuinely distinct semantic role.
- Add new tokens to `ThemeTokens` and provide a system-theme value in `buildSystemTheme`; account for both dark and light terminal backgrounds.
- Use `syntaxStyle()` for syntax-highlighted code.
- Every `scrollbox` should use `style={scrollbarStyle()}` unless it deliberately has no visible scrollbar.
- Header context usage uses `progressNormal` below 80%, `progressWarning` from 80% through 90%, and `progressCritical` above 90%.

## Surface Taxonomy

Choose a surface by interaction scope and presentation, not by its component name. A picker may be transient or modal depending on what it searches and how much context it owns.

### Transient overlay

Examples: `InlinePicker`, compact overflow pickers, toast notifications.

- Floats over existing content without replacing its context
- No modal backdrop
- Low visual weight and short-lived interaction
- Transient pickers commonly use `pickerBg` and avoid extra framing when placement already establishes the boundary
- Toasts are the exception: use `theme.bg` with a semantic variant-colored border and matching status icon/text

### Interaction dock

Examples: model and plugin confirmation, short input, selection, and guided-question requests.

- Replaces the composer and pending-status row while an interaction is active
- Spans the full content width without a modal backdrop
- Leaves the selected Agent/workspace surface visible, selectable, and mouse-scrollable
- Owns keyboard focus and provides explicit submit and cancel actions
- Uses a measured, bounded share of terminal height and windows long content
- Preserves and restores the composer draft, cursor, attachments, and queued follow-ups
- Shows the oldest server-owned pending request and identifies queue position when needed
- Must not cancel a server-owned request merely because this client switches sessions or detaches

### Dialog

Examples: settings, login, session exploration, command palette, workspace file finder.

- Centered above the current screen without dimming or recoloring the background
  unless it is a picker-style dialog
- Picker-style dialogs such as the command palette and session explorer share a
  top-quarter anchor and a 20-row minimum height, clamped to the viewport
- Uses a trapped focus scope so the undimmed background does not remain keyboard-active
- Uses `Dialog.Root` when its structure fits
- Content box has a `borderDefault` outer border and uses the surrounding `bg` background; do not tint the whole dialog when an undimmed shell remains visible behind it
- Width and height are bounded for the terminal rather than tied to one assumed viewport
- Header, tabs, and footer use `flexShrink={0}` so the body owns compression and scrolling
- Picker-style dialog footers stay fixed at the bottom behind a full-width top
  divider; use the shared picker dialog structure rather than feature-local chrome
- Header metadata must add useful task context. Omit obvious counts such as `1 option`; they are noise when the list itself communicates its size.
- Avoid nested borders unless an inner border conveys a distinct interactive state

OpenTUI paints border cells with the box background, which can create an inset appearance on filled surfaces. Keep dialog and border-cell backgrounds continuous; do not add decorative inner borders to compensate.

Compose picker behavior with `Picker.Root`, `Picker.Header`, `Picker.Body`, and `Picker.Footer`. Put that composition inside `InlinePicker` for a transient picker or `Dialog.Root` for a modal picker; do not fork picker interaction and selection styling.

### Full-screen takeover

Examples: Pager and fatal-error presentation.

- Occupies the viewport without an outer border
- Prefer `ScreenLayout` for absolute takeover surfaces with header/content/footer slots
- Prefer `ScreenHeader` for structured screen headers
- Content owns scrolling; header and footer remain fixed
- This category does not include File, Diff, or other workspace tabs

Root application states may own their root layout directly when they are not overlays. Do not force the main shell into `ScreenLayout` merely for visual consistency.

### Workspace surface

Examples: files, diffs, scratchpad, activity, releases, MCP, diagrams, and explicitly opened subagent conversations.

A workspace surface is persistent layout, not an overlay tier. Agent and retained workspace panes are full-width peer tabs at every viewport size, with the composer fixed below them.

## Screen Layout Primitives

### `ScreenLayout`

Use `ScreenLayout` for full-screen takeover surfaces that follow the header/content/footer pattern:

```tsx
<ScreenLayout header={header} footer={footer}>
  <ScrollableContent />
</ScreenLayout>
```

Do not place a second full-height framed box inside it.

### `ScreenHeader`

`ScreenHeader` supports two variants:

- `framed` (default) — complete border for a top-level screen header
- `strip` — compact fixed-height row with only a bottom separator

Both support left/right content and an optional progress overlay. Keep right-side metadata concise and non-wrapping where possible.

```tsx
<ScreenHeader
  variant="strip"
  left={<text>Title</text>}
  right={<text>metadata</text>}
  progress={42}
  progressColor={theme.progressNormal}
/>
```

## Session Multiplexing and Subagent Oversight

The native architecture supports many concurrent sessions, but the TUI presents
one attached session at a time. Do not add global session strips, session rails,
cross-session running counts, or other ambient multiplexing chrome. Users who
want simultaneous session views compose Kit clients with terminal tabs, panes,
tmux, or another native terminal workflow.

- Keep session exploration and switching available on demand through the
  universal command palette and session explorer; it is not persistent shell
  navigation.
- Global chrome describes only the attached session and its active run.
- The daemon's multi-session capability should make attach/detach and terminal
  multiplexing safe without demanding permanent visual presence.
- Subagents belong to their owning session.
- A modal picker presents the session's roster and status; there is no retained
  Subagents roster tab.
- Selecting a conversation from the picker, Agent activity, a notification, or
  another explicit entry point creates or activates one retained workspace tab
  for that durable conversation.
- Starting or activating background subagent work does not automatically create
  a tab, change tab order, or steal focus.
- Individual subagent tabs show that conversation's activity. Reopening the same
  conversation focuses the existing tab rather than duplicating it.

## Workspace Tabs and Panes

Native v2 workspace architecture is documented in
`docs/adrs/0018-retained-native-workspace-shell.md`. The main-worktree
`apps/web/docs/features/workspace-panes.md` remains a feature reference, not the v2
host-layout contract.

### Registry contract

Pane descriptors are plain data in `WorkspacePane`. Each entry in `WORKSPACE_PANE_DEFINITIONS` owns:

- `identity` — stable deduplication identity
- `label` — tab title and disambiguation
- `closable` — close policy
- `available` — optional runtime availability
- `render` — retained pane body

Do not store JSX, components, or callbacks in pane descriptors. Identity reflects the resource: mutable files use workspace incarnation plus canonical path, review-pinned files use review target plus path, diffs use their authoritative logical working-tree/commit/branch target, and subagent tabs use durable conversation identity. Line, range, and hunk anchors are navigation input rather than identity. Do not create retained tabs for transcript-recorded diffs or arbitrary historical snapshots.

### Chrome ownership

- `WorkspacePaneHost` owns the full-width Agent/workspace tab strip, labeled overflow, modal pane picker, selection, and retention.
- Agent is permanently first and contains the existing transcript presentation unchanged; omit the strip while no secondary pane is open.
- The composer remains fixed and available beneath every selected tab. `Tab` and `Shift+Tab` move focus between the selected Agent/workspace content and composer; active modal or interaction layers trap those focus intents locally.
- Directory browsing uses the modal workspace file picker, not a retained Explorer tab. Opening a file creates or selects its File tab.
- Annotations render inline at their anchored lines or ranges and project immediately as synchronized structured chips above the fixed composer on every tab. Activating a chip reveals its resource and anchor. Review is a target-scoped workflow over annotations across File and Diff tabs, not a Review tab; target, changed-file, draft-summary, and submission flows use bounded modals.
- The subagent roster uses a modal status picker. Only explicitly opened durable conversations become retained tabs.
- Pane bodies must not add another outer edge border.
- The tab owns the pane title.
- Use `WorkspacePanelLayout` for the shared optional-header/body/footer structure.
- Add `WorkspacePanelHeader` only when the pane has useful scope or live metadata beyond its tab label.
- In a context strip, place scope or identity on the left and live metrics/status on the right. A lone context value aligns left.
- `WorkspacePanelLayout` owns the footer's top separator. Its hint bar is borderless.

### Tab strip

- The active tab is visually distinct without adding a second framed container around its pane.
- Use `CHEVRON_LEFT` / `CHEVRON_RIGHT` for strip navigation and `TIMES` for closable tabs.
- Preserve tab order when revealing an active tab. Keep Agent and the selected tab visible where width permits and use a labeled count such as `⋯ 3 more` for overflow.
- The pane-list and overflow action opens the shared picker as a modal dialog; do not replace the selected content surface with an ad hoc list.
- Pane labels should stay concise. Disambiguate resource labels only when duplicates are present.

### Body and lifecycle

- The body uses `flexGrow={1}` and `overflow="hidden"`; put scrolling on the specific content region that needs it.
- Hidden pane bodies remain mounted when local editor, selection, or scroll state must survive tab switches.
- Gate keyboard layers, mouse interaction, focus, polling, and expensive watchers with the pane's `active` state where appropriate.
- Call `onFocusRequest` before a mouse action assumes keyboard ownership.
- Terminal resizing preserves the selected tab and retained state; width changes only label packing, overflow, wrapping, and feature-owned internal layout.

### In-pane drawers

Use an in-pane drawer for navigation that belongs to one pane, such as a repository tree in a file viewer.

- Use `WorkspaceSidebarToggle` for the compact leading toggle rail.
- The toggle is visually borderless, horizontally centered, and must provide hover feedback.
- Start secondary navigation collapsed unless the feature specifically benefits from persistent visibility.
- Use a named, tested width threshold to choose split presentation versus full-width drawer presentation.
- Keep drawer focus and keybindings separate from the content body's focus.

## Headers, Footers, and Hints

### `HintBar`

`HintBar` renders `key action · key action · ...` and is **bordered by default**.

- Use the default border when the hint bar is the outermost screen footer.
- Pass `borderless` inside dialogs, pickers, and `WorkspacePanelLayout`, where the parent already provides structure.

```tsx
<HintBar bindings={bindings} />
<HintBar borderless bindings={bindings} />
```

### `KeymapHintBar`

Prefer `KeymapHintBar` for registered commands. It reads active bindings from the keymap registry, so user customizations appear automatically.

```tsx
<KeymapHintBar borderless group="file-viewer" />
```

Use `prefixBindings` or `suffixBindings` for ad-hoc actions that are not keyboard commands, such as `Click comment`. Use plain `HintBar` only for small, intentionally non-rebindable binding sets.

## Spacing and Structure

- `paddingX={1}` is the standard horizontal breathing room.
- Prefer borders and separators for vertical structure; avoid routine `paddingY` around single-line rows.
- Use `gap` between flex children rather than accumulating child margins.
- Header and footer regions use `flexShrink={0}`; the main content region uses `flexGrow={1}` and owns overflow.
- Named semantic dimensions are allowed for minimum widths, dialog bounds, toggle rails, and responsive thresholds. Measure available space and avoid assumptions about a fixed terminal width.

## Transcript Layout

The native TUI transcript mirrors the macOS transcript's reading layout:

- Transcript rows keep at least `transcriptMinMargin` (2 cells) on each
  side and otherwise span the available width. Header, composer, and footer
  chrome stay edge to edge.
- Items within the column are separated by one blank row.
- User messages use the accent wash as a card: two cells of horizontal padding
  and half-block (`▄`/`▀`) top and bottom edges drawn in the wash color over
  the transcript background.
- When the latest message is scrolled entirely out of view, a compact
  `↓ Latest` shortcut floats at the transcript's bottom-right corner, one row
  above the composer separator. A tall final message keeps it hidden until the
  viewport scrolls past the message. Activating it re-pins follow and hides the
  shortcut. Retained subagent conversation tabs use the same shortcut.
- While a tall assistant message occupies the viewport, a one-row section strip
  is pinned to the top of the main transcript: the current heading title
  (leading non-heading content is Overview), `n / total`, then the previous and
  next arrows together. Clicking the title opens the section list over that
  title, dropping down through the section; choosing one, or the arrows, jumps
  there and unpins follow. Retained subagent conversation tabs use the same strip.
- Assistant prose and tool-group headers share the column's leading edge. Tool
  rows use the header's indicator column for their state icons, so tool titles
  align with the group label; interleaved thinking and prose indent to the same
  title column.

## Component Conventions

### Borders and reusable surfaces

- Borders communicate structure or state, not decoration.
- Avoid more than two or three visible nested border layers.
- Default/inactive structure uses `borderDefault`.
- Focused or editing controls use `borderFocused` or `borderAccent` according to interaction strength.
- Reuse shared surfaces when two features should look identical. Review and full-file comments use `ReviewNoteBox`; do not recreate that box styling locally.

### Text hierarchy

1. `textPrimary` — labels, content, and active values
2. `textSecondary` — supporting text and descriptions
3. `textMuted` — metadata, inactive labels, and hints
4. `textPlaceholder` — placeholders and tertiary guidance

Do not describe these tokens by assumed light/dark colors; user and terminal themes may invert their resolved values.

### Markdown

- Parse native TUI Markdown into renderer-neutral semantic blocks; do not render HTML or prebuilt ANSI strings into vaxis.
- Keep fenced code as explicit language/source blocks so syntax tokenization can be added independently of Markdown parsing and mapped into reactive theme styles.
- Cache parsed documents by stable content identity. Rebuild width- and theme-dependent widgets rather than caching painted output.
- Render headings through weight and semantic emphasis rather than literal heading markers. Use hanging indents for wrapped list items and a quiet left border for block quotes.
- Render labeled Markdown links using their label only, without appending the raw destination. Bare URLs remain visible as URLs; empty link labels fall back to the destination. Retain the full destination in OSC 8 metadata and activation behavior only for validated `http`, `https`, and `mailto` targets.
- Fenced code uses a subtle surface, preserves selectable source order, expands tabs consistently, and wraps rather than silently clipping in transcript-width layouts.
- Stream the latest thinking line as muted Markdown in the fixed one-row pending slot. Keep full thinking as Markdown evidence when a tool-backed Activity source exists, but do not create a transcript drawer for thinking alone.
- Buffer assistant text deltas by message identity and reveal the completed Markdown atomically. Do not render pending assistant prose in Transcript or Activity; thinking and tool activity remain live.
- Live tool groups auto-expand through five calls and collapse at six. Manual expansion/collapse overrides that default. A collapsed active group keeps the shared spinner, including between tool calls; completed groups use the normal collapsed presentation.

### Interactive elements

- **Focused row:** use a background highlight such as `bgMuted`; do not add a decorative row border.
- **Picker selection:** use `pickerFocusedBg` with `pickerFocusedText`.
  Hover and selection backgrounds cover the entire row, including text,
  metadata, gaps, and padding. Let the row surface own its background rather
  than painting an idle background over it in child text spans.
- **Searchable modal pickers:** use the shared `>` search marker and input
  spacing, a scrollbar for overflow, and keep keyboard selection visible when
  navigating, filtering, or resizing.
- **Selected-text copying:** acknowledge Kit-handled copies with a brief, theme-derived pulse of the copied selection's background rather than a success toast. Preserve text color, selection, and focus; repeated copies restart the bounded pulse. This indicates that Kit issued the copy, not that the terminal acknowledged clipboard storage. Terminal-native copying outside Kit has no in-app feedback.
- **Disabled command:** keep stable command catalogs visible and searchable. Render
  unavailable rows with disabled/muted text plus a concise reason such as
  `⊘ idle only`; disable pointer activation. Keyboard activation of a selected
  unavailable command keeps the palette open and reports the reason with a
  warning toast. Ephemeral action feedback belongs in a toast, not by replacing
  a picker’s fixed navigation footer.
- **Input:** transparent background with `borderDefault` when idle, `borderFocused` when focused, and `borderAccent` while editing when those states are distinct.
- A single-line input that owns a row fills all available horizontal space by default. Compact intrinsic-width inputs must be an explicit exception. In the native TUI, use Kit's `textInput` primitive for normal inputs; it is full-width by default. Use a bare `ui.TextField` only when an intentionally compact intrinsic-width control is required.
- Presentation tests for new row-owning inputs must render a value longer than the toolkit's intrinsic minimum width and assert that the complete value remains visible at a representative viewport width. This catches accidentally shrink-wrapped fields.
- **Toggle:** use the established four-cell track and two-cell knob; active track uses `toggleOn`.
- **Compact clickable control:** show immediate hover feedback, commonly `bgMuted` plus `textPrimary`. Communicate focus with the control background instead of decorative brackets around its label. A terminal pointer shape is optional supplemental feedback, never the only feedback, and must be reset on mouse-out.
- **Navigable URL:** underline the displayed link text and attach OSC 8 hyperlink metadata when the target is safe. Labeled Markdown links display only their label; bare URLs display their literal target. When the TUI has mouse reporting enabled, also handle activation explicitly because terminal-native clicks may be delivered to the app. Do not replace useful URLs with opaque `click here` copy.
- Handle only the primary mouse button for activation. Prevent propagation when the action should not also select or focus an ancestor.

### Diffs and inline comments

- Present changed files as titled sections in one continuous scroll document.
  File/hunk shortcuts jump within that document, not between isolated file views.
  Keep loading, continuation, retry, and non-text states local to their section;
  preserve loaded evidence and viewport position as other sections load.
- Use the `diffAdded*`, `diffRemoved*`, and `diffCursor*` token families rather than custom tints.
- Keep line-number gutters visually distinct from content while preserving one cursor state across the row.
- File and Diff viewers share the same code-facing `+` gutter action on the
  hovered or keyboard-current source line. Clicking comments on that line;
  dragging in the gutter selects a bounded range. Source text remains selectable,
  and hovering must not relocate an active comment editor. Read-only or stale
  evidence must not offer an actionable comment button.
- Use named glyphs such as `DASHED_VERTICAL` and `DIAMOND` for diff markers.
- Render a saved annotation immediately after its anchored line or range and project the same comment as a structured attachment chip above the composer.
- Keep inline annotations and chips synchronized through edit, anchor navigation, removal, stale state, submission, and successful consumption.
- Use `ReviewNoteBox` for saved review/file annotations and `MessageComposer` for inline editing.
- Changed-file and normal-file notes must share the same annotation surface, padding, border, and text hierarchy.

## Glyphs

All reusable UI glyphs live in `apps/web/src/shell/glyphs.ts`. Import named constants rather than scattering inline Unicode literals. The source file is the complete inventory; this list records important semantics, not every available glyph.

- `CHECK` — success or current selection
- `CROSS` — error or failure
- `CIRCLE_SLASH` — aborted or cancelled
- `CIRCLE_FILLED` / `CIRCLE_EMPTY` — active/dirty versus inactive/clean state
- `TRIANGLE_UP` — warning status
- `TRIANGLE_RIGHT` / `TRIANGLE_DOWN` — inline collapsed/expanded state
- `CHEVRON_LEFT` / `CHEVRON_RIGHT` — directional navigation or opening/closing adjacent detail
- `TIMES` — close or dismiss
- `MIDDLE_DOT` — inline metadata separator
- `SPINNER_FRAMES` — standard 80ms loading spinner. Native loading surfaces mount the shared spinner widget; feature-specific state must not own or duplicate its frames, cadence, or lifecycle.

## Empty States

Distinguish prominent application empty states from local list empties.

### Primary or first-run empty state

- Center vertically and horizontally in the available content area.
- Use `k i t` in `textPrimary` with `HEAVY_LINE.repeat(11)` in `borderAccent` when the Kit wordmark is appropriate.
- Put instruction text in `textSecondary` and command guidance in `textPlaceholder`.

### Local empty state

- Keep it quiet and contextual.
- Use concise `textMuted` copy such as `No changed files` or `No results`.
- Do not repeat the Kit wordmark inside drawers, lists, or nested panels.

## Anti-patterns

- **Fixed palette assumptions:** hardcoded hex values or descriptions that require a dark background.
- **Duplicate chrome:** pane bodies adding host-owned edge borders, tab titles, or footer separators.
- **Too much chrome:** decorative inner borders or more than two or three nested frames.
- **Local copies of shared UI:** recreating note boxes, headers, hint bars, or picker selection styling instead of using their shared primitives.
- **Hardcoded glyphs:** inline Unicode affordances when a named glyph exists.
- **Viewport magic numbers:** dimensions tied to an assumed terminal size instead of measured layout and named semantic bounds.
- **Stale keyboard hints:** bare hint text or manual binding arrays for commands represented in the keymap registry.
- **Inactive retained work:** hidden panes consuming input, stealing focus, polling, or running expensive watchers without need.
- **Oversized list rows:** inconsistent heights or unbounded secondary text where truncation would preserve scanability.
