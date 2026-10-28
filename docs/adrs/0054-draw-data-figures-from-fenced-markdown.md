# 0054: Draw data figures from fenced Markdown

## Status

Accepted

## Context

Agents report numbers, progress, choices, and structure in prose, lists, and
tables. Read as text, a set of totals, a release's progress, or a choice
between options takes effort to scan. Drawn as a small figure (large numbers,
bars, a rail of steps), the same content reads at a glance.

Kit already draws Mermaid diagrams from fenced code blocks with its own
renderer ([ADR 0046](./0046-draw-mermaid-diagrams-with-kits-own-renderer.md)):
an Ard reference in the terminal client, a Swift port in the macOS client,
held together by shared fixtures. Data figures face the same constraints:
the output is untrusted model text, it streams, it must read the same in both
clients, and anything not drawn must stay readable.

Markdown hosts that draw figures use one of three spellings: a fenced code
block with a language, a directive (`::name{…}`), or an MDX tag (`<Name>`).
Kit parses Markdown with goldmark in the terminal client and swift-markdown
in the macOS client. Neither knows directives or MDX. A directive's closing
`::` becomes part of its last list item, and a tag without blank lines
collapses its body into one raw HTML paragraph, so a client that doesn't
draw the figure shows something broken. A fenced code block is parsed
cleanly by both, and shows as code where it isn't drawn.

GitHub's alerts (`> [!WARNING]`) already give Markdown a spelling for callouts
that other hosts render as a plain quote.

## Decision

Kit's clients draw **figures**: fenced code blocks whose language names a
figure kind, drawn by Kit's own renderer under the rules of ADR 0046. Callouts
use GitHub's alert syntax.

### Authoring

A figure is a fenced code block. The info string is the figure's name followed
by attributes:

````markdown
```bars title="Routes"
- /docs: 12,400
- **/install: 4,100**
- /plot: 860
```
````

- **Names.** Figure names are Kit's own and bare: `stat`, `bars`, `meter`,
  `delta`, `timeline`, `steps`, `decision`, `spec`, `tree`, `compare`,
  `score`, `stack`, `plot`, `heatmap`, and `gantt`. There are no aliases. Any
  other language is an ordinary code block.
- **Attributes.** `key="value"` only, in double quotes without escapes, each
  key at most once. Every figure takes `title`; each figure documents the
  rest. An unknown or repeated key stops the figure from drawing.
- **Body grammar.** Every figure reads the same small grammar:
  - one item per list line;
  - `**…**` marks the item to read, which takes the accent;
  - `*…*` makes an item recede;
  - ` — note` adds a muted note;
  - `label: value` splits at the first colon followed by a space;
  - an indented paragraph after an item is its body;
  - `x*n` repeats a value.

  Each figure documents which of these it reads and how many bold items it
  allows. [Figures](../figures.md) is the reference grammar for each kind.
- **Values as written.** Numbers are parsed strictly (an optional sign,
  digits with optional `,` groups and `.` fraction, a unit; finite and
  bounded) and used only for scale. They are shown exactly as the author
  wrote them. A figure computes nothing it shows unless an attribute asks
  for it, such as `delta`'s `change="percent"`.
- **Callouts.** `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, and
  `[!CAUTION]`, with optional title text after the marker, are drawn as
  callouts. Other kinds remain quotes.

The system prompt lists the figure kinds and their grammar, so agents write
what Kit draws.

### Rendering

The reference renderer is written in Ard beside the Mermaid renderer in
`apps/cli/tui` and draws the terminal client's figures. `internal/markdown`
passes a code block's whole info string through; the reference parses the
attributes and body into a figure model and lays it out. The macOS client
draws with a Swift port of the parser and layout rules.

- **Reviewed grammar.** Each figure accepts exactly its documented grammar
  and rejects anything else with a reason, rather than drawing a guess.
- **Source fallback.** A figure that is malformed or over its limits shows its
  source as a code block with the reason it wasn't drawn. A fence that hasn't
  closed shows its source until it closes, so bars never rescale mid-stream.
- **Bounded input.** Each figure caps the body's bytes, items, and nesting,
  and the length of its labels. Labels lose control characters and are
  measured in terminal cells. Figures run no timers, fetch nothing, and
  evaluate no expressions.
- **Bounded synchronous rendering.** Within those caps a figure draws in well
  under ADR 0046's fifteen-millisecond budget, on the UI thread when its
  source changes, and is cached by source and theme.
- **Shared fixtures.** `testdata/figures` holds figure sources and, for each,
  the reference's parsed model and terminal drawing, or the reason it isn't
  drawn. The terminal client's tests check the reference and the macOS
  client's tests check the port. A grammar change regenerates the fixtures
  and updates the port in the same change.
- **Clients that don't know a figure** show its fence as an ordinary code
  block, which remains readable.

### Presentation

Both clients follow one visual rule: a single accent marks the bold item,
everything else is text, muted, or border, and colours come from existing
theme roles (accent, success, warning, danger). A figure introduces no new
theme tokens.

- **Terminal.** A figure is a single border with its title set into the top
  edge. Charts draw with block and line characters. A chart row never wraps;
  a figure wider than its pane scrolls sideways. Prose inside a figure (a
  step's body, a timeline note) wraps within the frame. A callout keeps the
  quote rail, without a frame, with its kind's colour and label.
- **macOS.** A figure is a fence card, like code and Mermaid: raised fill,
  rounded corners, a border, the title as the card's label, and a Copy
  button that copies the fenced source. Inside, the figure is drawn natively
  in the interface font with tabular figures. Bars are capsules on a track,
  timeline and steps are dots or numbered circles on a rail, and a meter's
  target is a tick. In a narrow tile, stat tiles fall to fewer columns,
  bars' label lanes truncate, and prose wraps. A callout is a tinted card
  without a label or Copy: its wash and edge mix the kind's colour into the
  surface, and Note and Important share the accent, told apart by icon and
  label.

## Consequences

- Agents can show numbers, progress, choices, and structure in a form that
  reads at a glance, and the source stays legible wherever Kit doesn't draw
  it: on GitHub, in another client, or when a figure fails.
- Kit's figure syntax is its own. Content written for hosts using directives
  or MDX tags, or other names for similar figures, shows as code or text
  until rewritten.
- The renderer exists twice, as Mermaid's does. Every grammar change is made
  in Ard and Swift together, and the shared fixtures fail whichever side
  falls behind.
- Supported figures grow only as Kit implements and reviews each grammar.
  The system prompt must list only what both clients draw.
- Both clients' Markdown models keep the whole info string of a code block,
  not just its first word. This is a small change to `internal/markdown`,
  which the terminal client consumes, and to the macOS client's code-block
  content.
