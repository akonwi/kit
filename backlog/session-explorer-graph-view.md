# Session explorer graph view

## Status

- [ ] deferred

## Context

A graph/branch-map alternate view was prototyped inside `/tree`, but the navigation model did not behave reliably enough to justify keeping it in the product yet.

The linear tree explorer remains the preferred and working session-navigation UI.

## Why deferred

- Graph navigation semantics were unclear in practice
- The visual model did not consistently show enough useful branch structure
- The extra complexity was not yet earning its keep over the linear tree

## If we revisit this

Potential next attempt should likely:

- define graph selection state independently from tree state
- choose one clear interaction model first:
  - lineage-only navigation, or
  - explicit branch-node navigation
- decide whether branch nodes are actionable or decorative
- ensure the graph exposes more useful local subtree context than the linear tree
- validate against real handoff-heavy session trees before shipping again
