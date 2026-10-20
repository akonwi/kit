# 0046: Draw Mermaid diagrams with Kit's own renderer

## Status

Accepted

## Context

Agents explain systems with Mermaid diagrams in fenced `mermaid` code blocks.
Read as source, a diagram's structure is hard to follow; drawn, it reads at a
glance. The terminal client renders Markdown in Ard with Cooper
([ADR 0035](./0035-build-the-terminal-client-with-ard-and-cooper.md)), so
diagrams are drawn as text cells that keep their shape, colors, and selection
like the rest of the transcript. The macOS client renders Markdown natively in
SwiftUI, and a diagram should read the same in both clients: the same syntax
drawn, the same reasons for what isn't, and the same layout.

Mermaid's reference renderer is JavaScript that lays out SVG. Go libraries
that draw Mermaid as text exist, but they cover their own subsets, accept
malformed input by drawing something plausible, can panic, and do not bound
their running time. Their output is plain text, so it cannot take the theme's
semantic colors.

## Decision

Kit's clients draw Mermaid diagrams with Kit's own renderer, which has no
third-party dependency. The reference renderer is written in Ard in
`apps/cli/tui/mermaid` and draws the terminal client's diagrams. The macOS
client draws with a Swift port of it, held to the reference's output by shared
fixtures (see [The macOS client](#the-macos-client)).

- **Reviewed subset.** Each diagram type supports a documented subset of
  Mermaid's syntax. A parser accepts exactly that subset and rejects anything
  else, rather than drawing a guess.
- **Source fallback.** A diagram that is unsupported, malformed, or over its
  limits shows its source as a code block with the reason it wasn't drawn.
  A fence that hasn't closed yet, such as streaming output, shows its source
  without a reason until it closes.
- **Bounded synchronous rendering.** Limits on source size, statements,
  participants, and drawing size keep the worst case to about fifteen
  milliseconds, so a diagram is drawn on the UI thread when its source changes
  and cached with its styled text until the source or theme changes.
- **Semantic cells.** Layout draws on a grid whose cells record the role they
  depict, such as frame, arrow, or label, and the directions their lines
  connect. Junctions resolve to the matching box-drawing character, and the
  view maps roles to theme colors.
- **Shape over wrapping.** A drawn diagram never wraps. A diagram wider than
  its pane scrolls sideways, as tables do.

Sequence diagrams are the first supported type. Flowcharts and other types are
added as separately reviewed subsets, to the reference and the port together.

### The macOS client

The macOS client draws diagrams with `KitMermaid`, a Swift module in
`apps/macos/Sources/KitMermaid` that ports the reference renderer. It depends
on nothing beyond Foundation.

- **Same drawing.** The port accepts exactly the reference's syntax, rejects
  everything else with the same reason, applies the same limits, and lays out
  the same canvas: cell for cell, the same text, role, and boldness. It
  measures text in the same terminal cell widths and, where the reference
  works on a string's UTF-8 bytes, so does the port.
- **Shared fixtures.** `testdata/mermaid` holds diagram sources and, for each,
  the reference's drawing as rows of text and the role of each cell, or the
  reason it isn't drawn. The terminal client's tests check the reference
  against the fixtures and the macOS client's tests check the port. A change
  to the renderer regenerates the fixtures from the reference and updates the
  port in the same change, so neither client draws a diagram the other
  doesn't.
- **Native drawing of the same layout.** The layout is the reference's: every
  node, participant, line, and piece of text keeps the place the terminal
  gives it, on a grid of cells as wide as the app's monospaced font. Beyond
  the cells, the port records the figures it draws (nodes in their shapes,
  participants, notes, and blocks) so the app can draw each whole: nodes and
  participants as filled shapes lifted from the card by a soft shadow, a
  database as a cylinder, a decision as a hexagon, notes tinted with the
  accent, and blocks as rounded frames whose keyword is an accent tag. Lines
  are strokes through the cells' centers that bend in curves; lifelines run
  unbroken, and an edge's text sits on a pill over its line where the
  terminal breaks the line around it. Text is set in the interface font,
  centered where the terminal centers it. Figures are presentation data only:
  they never change a cell, so the fixtures still describe both clients.
- **Same presentation rules.** A diagram is drawn when it's first shown, and
  its drawing is cached by source, so a streaming message doesn't draw its
  diagrams again with each token. It never wraps; a diagram wider than its pane scrolls
  sideways. Its source can be copied. A diagram that isn't drawn shows its
  source as a code block with the reason, and a fence that hasn't closed
  shows its source until it does.

### Layered layout

Flowcharts use a layered layout in `apps/cli/tui/mermaid/layers.ard`, which
other graph-shaped diagrams can share:

- **Loops.** A depth-first walk from each node in declaration order marks the
  edges that return to a node still being walked. Layout uses the remaining
  edges, and each marked edge leaves its source through the channel below,
  climbs its own column past the right edge, and drops into its target
  through the channel above, adding a channel at either end when needed.
  Shorter loops take the inner columns, chosen once routing has stopped
  moving vertices so no vertex lands on one. A loop's text sits beside its
  climb on the row nearest the middle that no other line crosses.
- **Layers.** A node's layer is one past its deepest predecessor, or two past
  it along a labeled edge whose source and target both have other edges; a
  start node moves down to sit just above its nearest successor. An edge
  that skips layers passes through a placeholder in each layer between,
  drawn in the edge's stroke.
- **Order.** Layers are reordered by the mean position of each node's
  neighbors in alternating downward and upward sweeps, keeping the order with
  the fewest crossings.
- **Placement.** Nodes are pulled toward their neighbors without overlapping,
  then each long edge's placeholders are lined up in one column so the edge
  runs straight, pushing later nodes in a layer aside when needed.
- **Ports.** An edge leaves its node's bottom border through a stem port and
  enters its target's top border through a drop port. Edges from one node
  with the same stroke share a stem port, and edges into one node with the
  same stroke and head share a drop port, so a shared line is drawn as each
  of its edges is. Lines joined through shared ports must not suggest edges
  that don't exist, so a set of joined lines is kept only when each of its
  stem ports leads to each of its drop ports; otherwise each of its edges
  gets its own drop port, or its own stem port when that takes fewer. An
  edge that skips layers, or a loop, joins lines in two channels, so it
  shares a port at one end only: when both are shared, its last drop gets
  its own. A node's ports sit two cells apart in the order of their other
  ends, and its box grows to fit them.
- **Routing.** Between two layers runs a channel of tracks. An edge leaves its
  stem port, runs sideways on a track, and drops into its drop port. Edges
  sharing a port share a track so they branch or merge. Taken from the left,
  each other set of lines joins the track, or opens a new one at the place
  among them, where the fewest lines cross its run, and shares a track only
  with sets whose spans never touch. A node's drops shift to line one up
  with its predecessor's stem when they all stay inside the box, and a stem
  never shares a column with an unrelated drop.
- **Labels.** Edge text sits only where its edge runs alone: on the drop into
  a node no other edge enters, on the stem from a node no other edge leaves,
  or beside the layers the edge skips. An edge with text whose source and
  target both have other edges spans at least two layers, as in Mermaid's
  own layout, so its text has a placeholder of its own. Nodes and
  placeholders carrying text are spaced to make room for it, and a diagram
  whose text would cover another line shows its source.
- **Directions.** Layout works in top-down terms: layers stack along the
  flow and members of a layer spread across it. Each node's extent along the
  flow is its box's height when flowing down or up and its box's width when
  flowing sideways, where boxes are three rows tall and spaced a row apart;
  a layer is as thick as its thickest member, and a shorter member's line
  runs on to the layer's end. A pen maps every line, arrowhead, and corner
  from layout coordinates onto the drawing for the diagram's direction, so
  junctions resolve the same way in all four, while boxes and text are placed
  directly on the drawing so text always reads left to right. Flowing
  sideways, labels sit on their edge's line and their bands are as long as
  the label.

### Subgraphs

A subgraph is not drawn as a frame inside its flowchart. Nesting frames
would need clustered layering and ordering, frame-aware spacing and routing,
and a combined layout for subgraphs that flow in their own direction, and a
large frame in a narrow pane is hard to read. Instead the flowchart is drawn
in sections, one under another:

- The flowchart comes first, with each top-level subgraph drawn as one
  dashed box marked `▸` with its title. Edges to and from the nodes inside a
  subgraph attach to its box, and an edge may name a subgraph directly.
- Each subgraph follows in the order it is declared, under its title, with
  its own nodes and the edges among them. A subgraph inside it is again one
  box, drawn in its own section after.
- An edge that crosses a subgraph's boundary also appears in that
  subgraph's section, reaching a dashed box for the node outside, named with
  the subgraph that holds it, so each section shows exactly which nodes
  connect beyond it. Only a subgraph inside a section is marked `▸`; a
  subgraph outside is named with where it is, and an edge naming a subgraph
  reaches an `all of …` box in that subgraph's own section.
- Edges between the same boxes collapse into one unless their strokes,
  heads, or text differ, and each section flows in its subgraph's
  `direction`, else its parent's.

As in Mermaid, a subgraph takes the nodes its own statements mention when
its `end` is reached, unless a subgraph that ended earlier took them, so
inner subgraphs take their nodes first. An edge can appear in each section
along its way, so the sections are bounded together: each counts as the
square of its nodes and edges, since layout grows faster than a graph's
size, and together they may cost no more than the largest flowchart without
subgraphs.

## Consequences

- Diagrams follow the theme and the client's selection and layout rules, and
  their tests assert exact rows like the rest of the terminal client.
- The renderer exists twice. Every change to it is made in Ard and Swift
  together, and the shared fixtures fail whichever side falls behind.
- The macOS client draws only what the terminal client draws, rather than
  the wider syntax a graphical Mermaid renderer would accept.
- Supported syntax grows only as Kit implements it. Diagrams using other
  syntax show their source, which remains readable.
- Layout quality is Kit's responsibility. Flowchart layout in particular
  needs its own design for layering, crossing reduction, edge routing, and
  cycles.
