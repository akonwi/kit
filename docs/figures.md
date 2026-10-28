# Figures

Kit draws data figures from fenced Markdown, as decided in
[ADR 0054](adrs/0054-draw-data-figures-from-fenced-markdown.md). This is the
reference grammar for each figure. A figure is drawn only once both clients
implement it; until then its fence shows as an ordinary code block.

## Shared rules

A figure is a fenced code block whose info string is the figure's name
followed by attributes: ```` ```name key="value" … ````.

- **Attributes:** `key="value"` only, in double quotes with no escapes, each
  key at most once. Every figure takes `title` (plain text, at most 48 cells).
  An unknown or repeated key means the figure isn't drawn, and the reason
  names the key.
- **Items:** one per list line (`- ` or `1. `). Blank lines between items are
  fine.
- **Bold:** `**…**` around a whole item (or a stat's value) marks the item to
  read, and it takes the accent. Each figure says how many bold items it
  allows. Most allow zero or one, `tree` allows any number, and `delta`
  allows bold only on its last row. More than allowed is an error.
- **Italic:** `*…*` around a whole item makes it recede (muted).
- **Notes:** ` — note` (an em dash with spaces; ` -- ` also works) adds a
  muted note.
- **Labels:** in `label: value`, the label is everything before the first `: `
  (a colon followed by a space), so `14:11: rolled back` has the label
  `14:11`. Other inline Markdown in labels (code, links) is shown as plain
  text.
- **Numbers:** an optional sign, digits with optional `,` groups, an optional
  `.` fraction, and an optional unit (`%`, `ms`, `kb`, or a `$` prefix). They
  must be finite with |x| <= 1e15. They are shown exactly as written; parsing
  is only for scale.
- **Runs:** `x*n` expands into n copies of a value (n <= 200).
- **Limits:** a body of at most 8 KB. Labels are cut at 32 cells with `…`
  unless a figure says otherwise, control characters are stripped, and
  widths are measured in terminal cells. Chart rows never wrap; a figure
  that doesn't fit scrolls sideways. Prose (step and timeline bodies,
  callouts) wraps inside the frame.
- **Failure:** a figure that isn't drawn shows its source with a one-line
  reason, as Mermaid diagrams do. While a message streams, a figure shows its
  source until its fence closes.

## Callouts

Callouts are GitHub alerts, not fences.

```markdown
> [!WARNING] Optional title
> Any Markdown body.
```

Kinds: NOTE, TIP, IMPORTANT, WARNING, CAUTION (marker is case-insensitive).
Any other kind is a plain quote. No Obsidian names, no fold markers.

## Figures

### stat

````markdown
```stat title="This week"
- **12,400** docs — +18%
    4 5 5 6 8 7 9
- 4,100 copies
- 860 shipped
```
````

- 1–4 items: `VALUE LABEL [— note]`. VALUE is the first token (or the bold
  span), shown verbatim, at most 12 cells. It is not parsed as a number.
- An optional indented line of numbers under an item is its sparkline
  (2–60 points). The last point gets the accent.

### bars

````markdown
```bars title="Routes"
- /docs: 12,400
- **/install: 4,100**
- /plot: 860
```
````

- 1–20 rows: `label: value [— note]`. One value per row; values must be >= 0.
- Row order is kept as written. The author sorts.
- `max="N"` fixes the scale; by default the largest value fills the track.
- `percent="first"` adds each row's share of the first row (a funnel);
  `percent="total"` adds each row's share of the sum.
- With a bold row, it gets the accent and the others are muted. With no bold
  row, every bar is neutral.

### meter

Single:

````markdown
```meter title="Coverage"
67% — of lines covered by tests
```
````

One line: `N%` (0–100), a fraction `0.67`, or `a / b`. Over 100% clamps and
turns warning.

Rows against a target:

````markdown
```meter title="Budget"
- Design: 42 / 40
- Motion: 18 / 24
- **Docs: 9 / 12**
```
````

- 1–12 rows: `label: value / target [— note]`; every row needs a target.
- Each row is scaled to its own target, so every tick lands in one column.
  The track runs past the tick to 125%, and a value beyond that clamps.
- A row over its target turns warning.

### timeline

````markdown
```timeline title="Shipped"
- Mar 12: CLI copies the files
- **Mar 18: Docs, live previews**

    Indented paragraph: the item's body.

- Apr 02: Registry listed
```
````

- 1–20 items: `when: event [— note]`, with an optional indented body.
- The bold item is now: accent `●`. Items before it are `●`; items after it
  are muted `○`. With no bold item, every item gets a plain `●`.

### steps

````markdown
```steps title="Release"
1. Freeze the tokens
2. **Ship the registry JSON**

   Waiting on the CDN purge

3. Write the postmortem
```
````

- 1–20 items in an ordered list, each a title with an optional indented body.
  Numbering starts at the list's first number.
- The bold item is current (accent number, bold title). Items before it are
  done (muted `✓`). With no bold item, every step is numbered and none is done.

### decision

````markdown
```decision title="Database" status="accepted" date="Mar 12"
- **Postgres** — we already run it
- *Mongo* — no joins we trust
- SQLite — fine until the second writer

Revisit if writes pass 2k a second.
```
````

- `status`: `proposed`, `accepted`, `rejected` or `superseded`; `date` is free
  text of at most 20 cells. Both are optional and appear on the first row.
- 2–8 options: `option [— reason]`. Bold is the choice (accent `●`), italic
  is ruled out (muted `×`), plain was considered (`○`). Zero or one bold.
- One paragraph after the list is a closing note that wraps.

### delta

Changes, with an optional total:

````markdown
```delta title="Bundle"
- vendor: 84 kb
- app: +31 kb
- sourcemaps: -12 kb
- **shipped: 103 kb**
```
````

Before and after:

````markdown
```delta title="Traffic" from="2025" to="2026" change="percent" better="up"
- docs: 8,200 → 12,400
- copy: 5,100 → 4,100
```
````

- 1–20 rows. Every row uses the same form: either `label: value` (a leading
  `+` or `-` marks added or removed, and an unsigned value is kept) or
  `label: from → to` (`->` works too). A mix of the two is an error.
- Bold is allowed only on the last row, which makes it the total, drawn
  under a rule. Totals are shown as written, not computed.
- `from`/`to` are column headings for the before-and-after form.
- `change="percent"` or `change="absolute"` adds a computed change column to
  the before-and-after form. By default there is none.
- `better="up"` or `better="down"` colours changes success or error. Without
  it, changes keep the text colour and only the sign glyphs show direction,
  because a bigger number isn't always better.

### spec

````markdown
```spec title="Type"
- Family: Geist Mono
- **Size: 14 / 21**
- Tracking: +0.02em

    An indented paragraph is a muted note under the value.
```
````

- 1–30 rows of `label: value`. Labels form one aligned column, capped at 24
  cells. Values are prose: they wrap within the value column.
- Zero or one bold row.

### tree

````markdown
```tree title="Registry"
- registry/default
  - graph-frame
    - graph-frame.tsx — ui
    - graph-motion.ts — lib
  - graph-tree
    - **graph-tree.tsx** — ui
```
````

- Nesting is by indentation (two spaces per level; tabs or uneven indents
  are an error). Depth at most 8, nodes at most 200, and more than one root
  is allowed.
- Notes line up in one muted column.
- Unlike other figures, any number of nodes may be bold (changed files, say).

### compare

````markdown
```compare title="Plans"
| | Solo | **Studio** |
| --- | --- | --- |
| Registry | yes | yes |
| Private source | no | yes |
| Price | $0 | $24 |
```
````

- A GFM table: the first column holds row labels, then 1–6 option columns,
  with at most 30 rows.
- `yes` draws `✓` and `no` draws a muted `–`; anything else is shown as written.
- A bold column heading is the pick: that column gets the accent. Zero or one.
- How this differs from a plain table: glyphs, a pick column, the frame, and
  cells that never wrap.

### score

````markdown
```score title="Review"
- Performance: 4/5
- Accessibility: 5/5
- **Docs: 2.5/5**
```
````

- 1–20 rows of `label: N/M`. M is the same on every row, from 1 to 10. N is a
  multiple of 0.5 from 0 to M.
- Drawn as `●●◐○○` with the written value. Zero or one bold row.

### stack

````markdown
```stack title="Bundle"
- marketing: 48 js, 22 css, 30 images
- **docs: 28 js, 18 css, 54 images**
```
````

- 1–12 rows of `label: value name, value name, …`. Every row has the same
  segment names in the same order, 2–4 of them. Values must be >= 0.
- Segments are shaded `█ ▓ ▒ ░` in order, with a legend under the rows.
- Each row fills the track by default (shares). `scale="shared"` makes the
  largest total fill the track, so the rows' lengths compare.
- Bold row gets the accent and the others are muted, the same as bars.

### plot, heatmap, gantt

These figures are named but their grammar isn't specified yet. Until it is,
their fences show as code.
