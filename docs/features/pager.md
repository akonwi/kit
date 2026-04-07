# Pager

## Status

Partially implemented, not restored to the active shell flow yet.

## Goal

Provide a focused long-form review surface for substantial agent output, with
section-based navigation and note/feedback capture.

## Current foundation

The repo includes pager-specific modules:

- `src/features/pager/index.ts`
- `src/features/pager/pager-controller.ts`
- `src/features/pager/split-sections.ts`
- `src/shell/PagerView.tsx`
- `src/features/commands/pager.ts`

## Intended behavior

The pager is meant to:

- activate for substantial assistant output
- split content into sections
- let the user move section-by-section
- capture per-section notes
- optionally feed structured feedback back into the agent

## Current caveat

The active shell path has been simplified while the standalone runtime was being
rebuilt. Pager code exists, but it is not currently part of the critical-path
loop.

Any pager reintroduction should be done against the current runtime/state/shell
architecture rather than by reviving older assumptions wholesale.
