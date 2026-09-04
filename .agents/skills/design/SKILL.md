---
name: design
description: Kit's UI design language and visual standards. Use when building or modifying any UI component, view, screen, overlay, or workspace pane. Covers themes, surface hierarchy, workspace layout, interaction, and component conventions.
---

# Kit Design Language

Kit's UI is **utilitarian, pleasant, and intuitive**. It should communicate state and available actions clearly without drawing attention to decoration. Every visual choice should improve comprehension, navigation, or feedback.

This skill describes the terminal UI in `app/src/`. Browser-specific presentations may reuse the same principles while using renderer-appropriate primitives.

## Theme System

Kit does not have a fixed application palette. The system theme is derived at runtime from the terminal's foreground, background, cursor, and ANSI colors. User themes may override semantic tokens from `~/.kit/themes/`.

The relevant sources are:

- `app/src/shell/themes/types.ts` — `ThemeTokens`, `SyntaxPalette`, and user-theme shapes
- `app/src/shell/themes/system.ts` — terminal-derived system theme and fallback palette
- `app/src/shell/theme.ts` — reactive `theme` store, `syntaxStyle()`, and `scrollbarStyle()`

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
- Context usage uses `progressNormal` below 80%, `progressWarning` from 80% through 90%, and `progressCritical` above 90%. Percentage text and progress indicator use the same token.

## Surface Taxonomy

Choose a surface by interaction scope and presentation, not by its component name. A picker may be transient or modal depending on what it searches and how much context it owns.

### Transient overlay

Examples: `InlinePicker`, compact overflow pickers, toast notifications.

- Floats over existing content without replacing its context
- No modal backdrop
- Low visual weight and short-lived interaction
- Transient pickers commonly use `pickerBg` and avoid extra framing when placement already establishes the boundary
- Toasts are the exception: use `theme.bg` with a semantic variant-colored border and matching status icon/text

### Dialog

Examples: settings, login, guided questions, session exploration, command palette, workspace file finder.

- Centered over a themed modal backdrop
- Uses `Dialog.Root` when its structure fits
- Content box has a `borderDefault` outer border and `bgSurface` background
- Width and height are bounded for the terminal rather than tied to one assumed viewport
- Header, tabs, and footer use `flexShrink={0}` so the body owns compression and scrolling
- Avoid nested borders unless an inner border conveys a distinct interactive state

OpenTUI paints border cells with the box background, which can create an inset appearance on filled surfaces. Do not add decorative inner borders to compensate.

Compose picker behavior with `Picker.Root`, `Picker.Header`, `Picker.Body`, and `Picker.Footer`. Put that composition inside `InlinePicker` for a transient picker or `Dialog.Root` for a modal picker; do not fork picker interaction and selection styling.

### Interaction dock

Examples: model- or plugin-invoked confirmation, short input, selection, and guided questions.

- Replaces the composer while input is required and spans the primary transcript column
- Uses a top `borderAccent` separator and `bgSurface` without a modal backdrop
- Keeps the transcript visible and mouse-scrollable while the dock owns keyboard focus
- Caps its height relative to the terminal and windows or scrolls long option content internally
- Uses `InteractionDock.Root`, `InteractionDock.Header`, `InteractionDock.Body`, and `InteractionDock.Footer`
- Queues concurrent requests rather than stacking multiple visible docks
- Public plugin `confirm`, `input`, and `select` primitives use this surface; arbitrary custom plugin UI remains a dialog

### Full-screen takeover

Examples: Pager and fatal-error presentation.

- Occupies the viewport without an outer border
- Prefer `ScreenLayout` for absolute takeover surfaces with header/content/footer slots
- Prefer `ScreenHeader` for structured screen headers
- Content owns scrolling; header and footer remain fixed
- This category does not include Code Review or other workspace panes

Root application states may own their root layout directly when they are not overlays. Do not force the main shell into `ScreenLayout` merely for visual consistency.

### Workspace surface

Examples: Code Review, files, scratchpad, activity, releases, MCP, diagrams, and sub-agent panes.

A workspace surface is persistent layout, not an overlay tier. It coexists with the transcript or replaces it in narrow mode and is retained across tab switches.

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

## Workspace Tabs and Panes

Workspace architecture is documented in `app/docs/features/workspace-panes.md`.

### Registry contract

Pane descriptors are plain data in `WorkspacePane`. Each entry in `WORKSPACE_PANE_DEFINITIONS` owns:

- `identity` — stable deduplication identity
- `label` — tab title and disambiguation
- `closable` — close policy
- `available` — optional runtime availability
- `render` — retained pane body

Do not store JSX, components, or callbacks in pane descriptors. Identity should reflect the resource being opened; for example, file panes deduplicate by repository and resolved path.

### Chrome ownership

- `WorkspacePaneHost` owns the primary/secondary separator, pane sizing, wide/narrow switching, tab strip, collapse behavior, and retention.
- Pane bodies must not add another outer edge border.
- The tab owns the pane title.
- Use `WorkspacePanelLayout` for the shared optional-header/body/footer structure.
- Add `WorkspacePanelHeader` only when the pane has useful scope or live metadata beyond its tab label.
- In a context strip, place scope or identity on the left and live metrics/status on the right. A lone context value aligns left.
- `WorkspacePanelLayout` owns the footer's top separator. Its hint bar is borderless.

### Tab strip

- The active tab is visually distinct without adding a second framed container around its pane.
- Use `CHEVRON_LEFT` / `CHEVRON_RIGHT` for strip navigation and `TIMES` for closable tabs.
- Preserve tab order when revealing an active tab; use the shared overflow picker when the strip cannot show every item.
- Pane labels should stay concise. Disambiguate resource labels only when duplicates are present.

### Body and lifecycle

- The body uses `flexGrow={1}` and `overflow="hidden"`; put scrolling on the specific content region that needs it.
- Hidden pane bodies remain mounted when local editor, selection, or scroll state must survive tab switches.
- Gate keyboard layers, mouse interaction, focus, polling, and expensive watchers with the pane's `active` state where appropriate.
- Call `onFocusRequest` before a mouse action assumes keyboard ownership.
- When the primary and secondary surfaces cannot both meet their minimum useful widths, use labeled narrow-mode tabs rather than a dialog fallback.

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

### Interactive elements

- **Focused row:** use a background highlight such as `bgMuted`; do not add a decorative row border.
- **Picker selection:** use `pickerFocusedBg` with `pickerFocusedText`.
- **Input:** transparent background with `borderDefault` when idle, `borderFocused` when focused, and `borderAccent` while editing when those states are distinct.
- **Toggle:** use the established four-cell track and two-cell knob; active track uses `toggleOn`.
- **Compact clickable control:** show immediate hover feedback, commonly `bgMuted` plus `textPrimary`. A terminal pointer shape is optional supplemental feedback, never the only feedback, and must be reset on mouse-out.
- Handle only the primary mouse button for activation. Prevent propagation when the action should not also select or focus an ancestor.

### Transcript images

- Render an image in the main transcript only when message content or an explicit presentation tool identifies it as an image; do not infer images from paths mentioned in prose.
- Reserve fixed rows before loading an inline preview so decoding cannot shift surrounding transcript content unexpectedly.
- Keep restored image history collapsed, allow at most one expanded transcript preview, and mount the image renderable only while expanded to bound native image memory.
- Open clicked transcript images in a workspace preview pane by default; native application launch is an explicit pane action.
- Use OpenTUI image protocol `auto` so Kitty, Sixel, and Unicode-block fallback follow renderer capabilities.
- Keep image rows minimally framed. The transcript entry and disclosure row establish context; do not add a decorative preview border.

### Diffs and inline comments

- Use the `diffAdded*`, `diffRemoved*`, and `diffCursor*` token families rather than custom tints.
- Keep line-number gutters visually distinct from content while preserving one cursor state across the row.
- Use named glyphs such as `DASHED_VERTICAL` and `DIAMOND` for diff markers.
- Render a saved annotation immediately after its anchored line or range.
- Use `ReviewNoteBox` for saved review/file annotations and `MessageComposer` for inline editing.
- Changed-file and normal-file notes must share the same annotation surface, padding, border, and text hierarchy.

## Glyphs

All reusable UI glyphs live in `app/src/shell/glyphs.ts`. Import named constants rather than scattering inline Unicode literals. The source file is the complete inventory; this list records important semantics, not every available glyph.

- `CHECK` — success or current selection
- `CROSS` — error or failure
- `CIRCLE_SLASH` — aborted or cancelled
- `CIRCLE_FILLED` / `CIRCLE_EMPTY` — active/dirty versus inactive/clean state
- `TRIANGLE_UP` — warning status
- `TRIANGLE_RIGHT` / `TRIANGLE_DOWN` — inline collapsed/expanded state
- `CHEVRON_LEFT` / `CHEVRON_RIGHT` — directional navigation or opening/closing adjacent detail
- `TIMES` — close or dismiss
- `MIDDLE_DOT` — inline metadata separator
- `SPINNER_FRAMES` — standard 80ms loading spinner

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
