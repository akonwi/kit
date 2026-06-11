---
name: design
description: Kit's UI design language and visual standards. Use when building or modifying any UI component, view, or screen. Covers color palette, overlay hierarchy, layout patterns, and component conventions.
---

# Kit Design Language

Kit's aesthetic is **utilitarian, pleasant, and intuitive**. The UI should feel invisible — it communicates information clearly without drawing attention to itself. Every visual choice serves comprehension, not decoration.

## Color Palette

### Grayscale

| Token         | Hex       | Usage                        |
|---------------|-----------|------------------------------|
| `black`       | `#0a0a0a` | Primary background           |
| `nearBlack`   | `#171717` | Surface/elevated background  |
| `darkGray`    | `#262626` | Muted background, borders    |
| `midGray`     | `#404040` | Accent background, placeholders |
| `gray`        | `#a1a1a1` | Muted text, secondary borders |
| `lightGray`   | `#d4d4d4` | Secondary text               |
| `offWhite`    | `#fafafa` | Primary text                 |

### Accent Colors

| Color    | Hex       | Semantic Role                                    |
|----------|-----------|--------------------------------------------------|
| `blue`   | `#6cb6ff` | Primary — user messages, links, focused borders   |
| `green`  | `#7ee787` | Success — tool calls, completed actions           |
| `red`    | `#ff6467` | Error — failures, removed code                    |
| `amber`  | `#ffb86a` | Warning — caution states, constants               |
| `purple` | `#8a6bbd` | Debug — keywords, review text                     |
| `cyan`   | `#7dcfff` | Metadata — line counts, expandable indicators     |
| `rose`   | `#e78a9a` | Attachments — code reviews, images in transcript  |

### Rules

- All colors must be defined in `src/shell/theme.ts`. Never hardcode hex values in components.
- Name tokens by semantic purpose, not by color (e.g., `metaText` not `cyanText`).
- When a new element needs color, first check if an existing token fits. Add a new token only when the semantic role is genuinely different.
- The context percentage text color should match the progress bar color and change dynamically (blue < 80%, amber 80-90%, red > 90%).

## Overlay Hierarchy

Three tiers, each with a distinct visual signature:

### Tier 1 — Transient (picker, toast)
- No border, `pickerBg` background
- No backdrop
- Float on top of existing content
- Highest interaction frequency, lowest visual weight

### Tier 2 — Dialog (modal, settings, session explorer, guided questions)
- `borderDefault` outer border on the content box
- Semi-transparent backdrop (`RGBA(10, 10, 10, 180)`)
- Centered, sized to content with percentage width and fixed height
- `flexShrink={0}` on header, tabs, and hint bar to prevent content squishing
- Note: OpenTUI's `drawBox` renders border cells with the box's `backgroundColor`, producing an inset look on filled overlays. This is acceptable — the border still reads as a visible frame.

### Tier 3 — Screen (main shell, pager, review)
- Full-viewport takeover, no outer border
- Uses `ScreenLayout` and `ScreenHeader` components from `src/shell/`
- Follows the **header / content / footer** pattern:
  - **Header**: `ScreenHeader` — bordered, left/right content slots, optional progress bar overlay
  - **Content**: `flexGrow={1}`, scrollable
  - **Footer**: `flexShrink={0}`, typically a `HintBar` or input area

## Layout Patterns

### Screen Layout (`ScreenLayout`)
All full-screen views use `ScreenLayout` with named `header`, `footer`, and `children` (content) props. The layout handles flex rules internally — header and footer are fixed, content fills remaining space.

```tsx
<ScreenLayout header={...} footer={...}>
  {/* scrollable content */}
</ScreenLayout>
```

### Screen Header (`ScreenHeader`)
Bordered header bar with left/right content slots and an optional progress bar overlay on the top border.

```tsx
<ScreenHeader
  left={<text>Title</text>}
  right={<text>metadata</text>}
  progress={42}
  progressColor={color}
/>
```

### Inline Panel
A panel mounted alongside the transcript or another primary surface — part of the layout, not floating. Unlike an overlay, an inline panel coexists with the underlying view rather than taking it over. Used for the turn activity sidebar on wide terminals.

Chrome conventions:
- **Edge border**: `border={["left"]}` or `border={["right"]}` on the inward-facing edge as a separator from the adjacent surface. Use `borderDefault`.
- **Header strip**: top of the panel, `paddingX={1} paddingY={0}` with `border={["bottom"]}`. Left content (title in `textPrimary`), right content (metadata in `textMuted`). One row tall.
- **Body**: `flexGrow={1}`, a `scrollbox` for the content with theme-styled scrollbar.
- **Hint bar**: `bordered` — it is the outermost structural element at the bottom of the panel.

Width and visibility:
- Derived from the viewport (e.g., `max(40, 40% of width)`).
- Gated by a minimum-viewport threshold; auto-collapse to a Tier 2 dialog fallback when the viewport drops below it.
- Mounted inside the shell layout as a flex sibling of the primary column, so absolutely-positioned children (e.g., InlinePicker) of the primary column don't bleed across it.

### Hint Bar (`HintBar`)
Standardized keyboard shortcut bar. Renders bindings as `key action · key action · ...`.

- **Borderless by default** — used in Tier 2 dialogs and Tier 1 pickers where the surrounding container already provides framing.
- **`bordered`** — use at Tier 3 screen footers (pager, auth gate, main transcript, review) and inline panel footers, where the hint bar is the outermost structural element.

```tsx
// Dialog / picker footer (default, borderless)
<HintBar bindings={bindings} />

// Screen footer (bordered)
<HintBar bordered bindings={bindings} />
```

Extract binding arrays into top-level constants keyed by mode (see `threads.tsx` for the pattern).

### Spacing and Padding
- `paddingX={1}` is the standard horizontal breathing room
- Borders provide vertical structure — avoid excessive `paddingY`
- Use `gap` between flex children rather than padding on individual items

## Component Conventions

### Borders
- Use borders to create structure, not decoration
- Reduce nested borders — the outermost container (dialog or screen) does the framing; interior elements can be quieter
- Focused elements: `borderAccent` (blue) or `borderFocused` (gray), depending on context
- Default/inactive: `borderDefault` (dark gray)

### Text Hierarchy
1. `textPrimary` (offWhite) — labels, content, active items
2. `textSecondary` (lightGray) — supporting text, descriptions
3. `textMuted` (gray) — hint text, inactive items, metadata
4. `textPlaceholder` (midGray) — input placeholders, tertiary hints

### Interactive Elements
- **Toggle**: 4-char wide track with 2-char knob. Track uses `toggleOn` when active.
- **Focused row**: Background highlight (`bgMuted`) only — no border color change on the row itself.
- **Input fields**: Transparent background, border changes with state: `borderDefault` (idle), `borderFocused` (row focused), `borderAccent` (editing).
- **Picker/list selection**: Inverted colors (`pickerFocusedBg`/`pickerFocusedText`).

### Status Indicators

All glyphs are defined in `src/shell/glyphs.ts`. Import named constants; never use inline Unicode strings in components.

- `CHECK` (`✓`) — success, current selection
- `CROSS` (`✗`) — error, failure
- `CIRCLE_SLASH` (`⊘`) — aborted/cancelled
- `CIRCLE_FILLED` (`●`) — on, active, dirty
- `CIRCLE_EMPTY` (`○`) — off, inactive, clean, idle
- `TRIANGLE_RIGHT`/`TRIANGLE_DOWN` (`▸`/`▾`) — collapsed/expanded **inline**
- `CHEVRON_RIGHT` (`›`) — focused item indicator, or "open detail view" affordance when clicking drills into a separate panel/dialog
- `TIMES` (`×`) — close/dismiss
- `MIDDLE_DOT` (`·`) — inline metadata separator
- `SPINNER_FRAMES` — braille spinner at 80ms

### Empty States
- Centered vertically and horizontally in the content area
- Wordmark banner: letter-spaced `k    i    t` in `textPrimary` with `borderAccent` underline (`━━━━━━━━━━━`)
- Instruction text in `textSecondary`
- Command hints in `textPlaceholder`

## Anti-patterns

- **Too much chrome**: If a view has more than 2-3 layers of nested borders visible at once, strip the inner ones. Let the outermost container do the framing.
- **Hardcoded colors**: Every color string must come from `theme.ts`. No inline hex values.
- **Hardcoded glyphs**: Every Unicode glyph must come from `src/shell/glyphs.ts`. No inline Unicode strings in components.
- **Hardcoded widths**: Use `width="100%"` or measured refs, never magic numbers like `"─".repeat(80)`.
- **Inconsistent hint bars**: Always use the `HintBar` component. Never render keyboard hints as bare text.
- **Oversized settings rows**: Settings rows should have uniform fixed height with truncated descriptions.
