# 0046: Draw Mermaid diagrams with Kit's own renderer

## Status

Accepted

## Context

Agents explain systems with Mermaid diagrams in fenced `mermaid` code blocks.
Read as source, a diagram's structure is hard to follow; drawn, it reads at a
glance. The terminal client renders Markdown in Ard with Cooper
([ADR 0035](./0035-build-the-terminal-client-with-ard-and-cooper.md)), so
diagrams are drawn as text cells that keep their shape, colors, and selection
like the rest of the transcript.

Mermaid's reference renderer is JavaScript that lays out SVG. Go libraries
that draw Mermaid as text exist, but they cover their own subsets, accept
malformed input by drawing something plausible, can panic, and do not bound
their running time. Their output is plain text, so it cannot take the theme's
semantic colors.

## Decision

The terminal client draws Mermaid diagrams with its own renderer, written in
Ard in `apps/cli/tui/mermaid`. It has no third-party dependency.

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
added as separately reviewed subsets.

### Layered layout

Flowcharts use a layered layout in `apps/cli/tui/mermaid/layers.ard`, which
other graph-shaped diagrams can share:

- **Layers.** A node's layer is one past its deepest predecessor; a start node
  moves down to sit just above its nearest successor. An edge that skips
  layers passes through a placeholder in each layer between.
- **Order.** Layers are reordered by the mean position of each node's
  neighbors in alternating downward and upward sweeps, keeping the order with
  the fewest crossings.
- **Placement.** Nodes are pulled toward their neighbors without overlapping,
  then each long edge's placeholders are lined up in one column so the edge
  runs straight, pushing later nodes in a layer aside when needed.
- **Routing.** Between two layers runs a channel of tracks. An edge leaves its
  node's bottom border, runs sideways on a track, and drops into its target's
  top border. Edges from one node, or into one node, share a track so they
  branch or merge; other tracks are assigned so their spans never touch. A
  drop lines up with a predecessor's stem when that column falls inside the
  target's box, and a stem never shares a column with a drop into an
  unrelated node.
- **Labels.** Edge text sits only where its edge runs alone: on the drop into
  a node no other edge enters, on the stem from a node no other edge leaves,
  beside the layers the edge skips, or on a sideways run of its own. Nodes and
  placeholders carrying text are spaced to make room for it, and a diagram
  whose text would cover another line shows its source.

## Consequences

- Diagrams follow the theme and the client's selection and layout rules, and
  their tests assert exact rows like the rest of the terminal client.
- Supported syntax grows only as Kit implements it. Diagrams using other
  syntax show their source, which remains readable.
- Layout quality is Kit's responsibility. Flowchart layout in particular
  needs its own design for layering, crossing reduction, edge routing, and
  cycles.
