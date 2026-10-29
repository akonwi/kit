---
name: kit-figures
description: >-
  Syntax for the data figures and callouts Kit draws in replies: stat, bars,
  meter, timeline, steps, decision, delta, spec, tree, compare, score and
  stack fences, and GitHub-alert callouts. Load it before writing any of
  them, and before a reply with a Markdown table of numbers or options, an
  ASCII file tree, or several numbers, a change, progress, a decision or
  ratings, even if the user didn't ask for a chart. Figures draw only in Kit,
  not in text to paste elsewhere.
---

# Kit figures

Kit draws a fenced block as a figure when its info string is a figure name
followed by attributes. Anything that doesn't match the grammar below is not
drawn: the reader sees the fence's source and a one-line reason instead. Write
figures exactly as shown here.

Three rules that are easy to break:

1. **Figures go only in your reply.** They draw nowhere but Kit. Never put
   one in a file, a commit message, a pull request, an issue, or text the
   user will paste into chat or email; write plain Markdown there, and don't
   offer a figure version alongside it.
2. **Bold is not emphasis.** It marks the one item to read and must wrap the
   whole item: `- **Docs: 2.5/5**`, never `- Docs: **2.5/5**`. Most figures
   allow one bold item, so don't bold every value of a `stat` or the best
   score in each row.
3. **One figure per point.** One or two in a reply is usual. Put a whole
   comparison in one figure rather than one figure per criterion or option.

## Choose a figure

Use a figure when it makes the answer easier to read than prose or a plain
table. A figure for one or two numbers is noise; put them in the sentence.

| To show | Use |
|---|---|
| 1–4 headline numbers, optionally with a trend | `stat` |
| Quantities to compare, one per row | `bars` |
| Progress toward a whole, or rows against their own targets | `meter` |
| Dated events, with where things stand now | `timeline` |
| An ordered procedure and the current step | `steps` |
| Options weighed and the one chosen | `decision` |
| Changes per item, or values before and after | `delta` |
| Labelled properties (settings, a type scale, a config) | `spec` |
| A file or module hierarchy | `tree` |
| Options against the same criteria | `compare` |
| Ratings out of the same whole | `score` |
| Each row split into the same named parts | `stack` |
| A remark that needs attention | a callout |

## Rules for every figure

- Info string: the name, then `key="value"` attributes in double quotes.
  Only the keys listed for that figure are allowed, each once. Every figure
  takes `title` (plain text, cut at 48 characters); it is optional.
- One item per list line: `- ` (or `1. ` for steps).
- `label: value` splits at the first colon followed by a space, so
  `14:11: rolled back` has the label `14:11`.
- `**…**` around a whole item marks the one item to read; it takes the accent
  and, in bars, score and stack, the other rows are muted. Most figures allow
  zero or one bold item. `*…*` around a whole item makes it recede.
- ` — note` (an em dash with spaces, or ` -- `) adds a muted note to an item.
- Numbers: optional sign, optional `$`, digits with optional `,` groups and
  a `.` fraction, then an optional unit of 1–4 letters or `%`, with or
  without a space (`38ms`, `84 kb`, `-$1,200.50`, `12k`). Nothing else:
  not `€`, `°C`, `req/s`, `1e6` or `~40`. Numbers are shown as written.
- Labels past 32 characters are cut with `…`, and rows never wrap. Keep
  labels short.

## Figures

### stat

```stat title="This week"
- **12,400** docs views — +18% vs last week
    4 5 5 6 8 7 9
- 4,100 copies
- 860 shipped
```

- 1–4 items of `VALUE label [— note]`. VALUE is the first word (or the bold
  span), at most 12 characters, shown verbatim.
- An indented line of 2–60 numbers under an item draws its sparkline.

### bars

```bars title="Routes" percent="total"
- /docs: 12,400
- **/install: 4,100**
- /plot: 860
```

- 1–20 rows of `label: value`, values >= 0, drawn in the order written. Sort
  them yourself.
- Keys: `title`, `max="N"` (fixed scale), `percent="first"` (share of the
  first row, for funnels) or `percent="total"` (share of the sum).

### meter

```meter title="Coverage"
67% — of lines covered by tests
```

```meter title="Budget"
- Design: 42 / 40
- Motion: 18 / 24
- **Docs: 9 / 12**
```

- Either one line that is not a list item (`67%`, `0.67` or `a / b`), or
  1–12 rows of `label: value / target`, every row with a target.
- A value over its target turns warning. Keys: `title`.

### timeline

```timeline title="Shipped"
- Mar 12: CLI copies the files
- **Mar 18: Docs and live previews**

    Blocked on the registry index.

- Apr 02: Registry listed
```

- 1–20 items of `when: event [— note]`. An indented paragraph under an item
  is its body.
- The bold item is now; items before it are done and items after it are
  upcoming. Keys: `title`.

### steps

```steps title="Release"
1. Freeze the tokens
2. **Ship the registry JSON**

   Waiting on the CDN purge

3. Write the postmortem
```

- 1–20 items of a numbered list, each a title with an optional indented body.
- The bold step is current and the ones before it are done. Keys: `title`.

### decision

```decision title="Database" status="accepted" date="Mar 12"
- **Postgres** — we already run it
- *Mongo* — no joins we trust
- SQLite — fine until the second writer

Revisit if writes pass 2k a second.
```

- 2–8 options of `option [— reason]`: bold is the choice (zero or one),
  italic is ruled out, plain was considered. No indented lines.
- At most one paragraph after the list, as a closing note. Nothing may
  follow it.
- Keys: `title`, `status` (`proposed`, `accepted`, `rejected` or
  `superseded`), `date` (free text, at most 20 characters).

### delta

```delta title="Bundle"
- vendor: 84 kb
- app: +31 kb
- sourcemaps: -12 kb
- **shipped: 103 kb**
```

```delta title="Traffic" from="2025" to="2026" change="percent" better="up"
- docs: 8,200 → 12,400
- copy: 5,100 → 4,100
```

- 1–20 rows, all in one form: `label: value` (a leading `+` or `-` marks
  added or removed), or `label: before → after`. Never mix the two forms.
- Only the last row may be bold, and it becomes a total under a rule. Kit
  does not compute it: write the correct sum.
- A row's numbers share one unit, `$` included.
- Keys: `title`, and for the before-and-after form only: `from` and `to`
  (column headings), `change="percent"` or `change="absolute"` (adds a
  computed change column), `better="up"` or `better="down"` (colours
  changes green or red; leave it out when bigger isn't clearly better).

### spec

```spec title="Type"
- Family: Geist Mono
- **Size: 14 / 21**
- Tracking: +0.02em

    An indented paragraph is a muted note under the value.
```

- 1–30 rows of `label: value`. Values are prose and wrap. Zero or one bold
  row. Keys: `title`.

### tree

```tree title="Registry"
- registry/default
  - graph-frame
    - graph-frame.tsx — ui
    - graph-motion.ts — lib
  - graph-tree
    - **graph-tree.tsx** — changed
```

- Every line is a `- ` node with a name, indented exactly two spaces per
  level, one level at a time. No tabs. At most 8 levels and 200 nodes;
  several roots are fine.
- Any number of nodes may be bold, for example the files a change touched.
  Keys: `title`.

### compare

```compare title="Plans"
| | Solo | **Studio** |
| --- | --- | --- |
| Registry | yes | yes |
| Private source | no | yes |
| Price | $0 | $24 |
```

- A table with the `| --- |` divider as its second row: row labels in the
  first column, then 1–6 option columns, at most 30 rows. Every row starts
  with `|` and fills every column.
- Write lowercase `yes` and `no`, not ✓ or ✗: `yes` draws ✓ and `no` a muted
  –; anything else is shown as written. Bold the heading of the option you
  recommend, if you recommend one.
- Cells never wrap, so keep them short. For prose cells, use a plain table.
  Keys: `title`.

### score

```score title="Review"
- Performance: 4/5
- Accessibility: 5/5
- **Docs: 2.5/5**
```

- 1–20 rows of `label: N/M`. M is the same on every row, 1–10. N is a whole
  number or ends in `.5`, from 0 to M. Keys: `title`.

### stack

```stack title="Bundle" scale="shared"
- marketing: 48kb js, 22kb css, 30kb images
- **docs: 28kb js, 18kb css, 54kb images**
```

- 1–12 rows of `label: value name, value name, …`. Each segment splits at
  its first space, so write a unit without a space: `48 kb js` draws a
  segment named `kb js`.
- Every row has the same 2–4 segment names in the same order. Values are
  >= 0 and share one unit.
- Kit computes and shows each row's total; don't add a total row or segment.
- Keys: `title`, `scale="shared"` (rows compare by total length; by default
  each row fills the track to show shares).

## Callouts

Callouts are GitHub alerts, not fences:

> [!WARNING] Optional title
> Any Markdown body.

Kinds: `NOTE`, `TIP`, `IMPORTANT`, `WARNING`, `CAUTION`. Any other kind is a
plain quote.

## Gotchas

- A number has one unit. Durations like `4m10s` or `1:30` aren't numbers:
  convert to one unit (`250s`).
- Single-quoted or unquoted attributes, unknown keys (`subtitle`, `unit`,
  `sort`, `color`), and a key used twice all stop the figure from drawing.
- A single hyphen is not a note separator: `- app: 31 kb - from deps` makes
  `31 kb - from deps` the value, which isn't a number. Use ` — `.
- Bold on more items than a figure allows is an error. Only `tree` allows
  several.
- `plot`, `heatmap` and `gantt` aren't drawn yet; don't use them.
- To show figure syntax without drawing it, put it inside a four-backtick
  ` ````markdown ` fence.
- If the user reports a figure shown as source with a reason, fix what the
  reason names and resend the figure.
