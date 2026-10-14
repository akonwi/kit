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

## Consequences

- Diagrams follow the theme and the client's selection and layout rules, and
  their tests assert exact rows like the rest of the terminal client.
- Supported syntax grows only as Kit implements it. Diagrams using other
  syntax show their source, which remains readable.
- Layout quality is Kit's responsibility. Flowchart layout in particular
  needs its own design for layering, crossing reduction, edge routing, and
  cycles.
