# Browser-backed code review (`/code-review`)

## Status

Prototype started

## Decision

Split diff inspection from full review:

- `/diff` stays terminal-native and focuses on browsing the current uncommitted diff
- `/code-review` should become a browser-backed review experience with richer hunk navigation and annotation UI

## Why

The current OpenTUI diff surface works well for lightweight inspection, but it is a poor fit for richer review interactions like:

- clear visual framing around the current hunk
- inline annotations/comments
- sticky local headers while scrolling patch content
- richer review layout and future commenting workflows

A browser-rendered SPA removes most of those rendering constraints.

## Product shape

### `/diff`

Terminal-native modal for:

- quickly viewing current uncommitted diffs
- file-by-file accordion browsing
- hunk navigation while focused on a patch
- staying entirely inside the terminal

### `/code-review`

Browser-backed review UI for:

- richer annotation affordances
- hunk-focused review
- clearer visual anchoring
- future structured code review workflows

## Architecture direction

Kit should remain the orchestrator:

1. gather the current diff review payload
2. launch a localhost-backed review page
3. open the browser
4. exchange state/results with the SPA
5. receive structured review output back into Kit

Current prototype direction:

- localhost HTTP server owned by Kit
- browser page opened directly with the system browser
- WebSocket bridge for browser ↔ Kit communication
- a real SPA entrypoint served by Kit
- client-side diff rendering in the SPA using `@pierre/diffs`

Likely later refinements:

- localhost HTTP + WebSocket for live review state
- localhost HTTP + POST for one-shot submit or export flows
- richer client-side review state and persistence

## Inspiration

- `pi-diff-review`
- `glimpse`

These are still useful references for the browser review UX and host/UI split, even though the current prototype direction is to use the system browser directly rather than a Glimpse window.

## Current prototype scope

The current prototype establishes the browser foundations:

- `/code-review` opens a localhost-backed SPA
- the SPA connects back to Kit over WebSocket
- Kit sends diff/session state to the SPA
- the SPA renders the selected patch client-side with `@pierre/diffs`
- the browser shell uses Kit-aligned theme tokens instead of ad hoc debug chrome

This is now a real browser diff surface, but it is still missing richer review interactions like annotations, hunk selection state, and structured submission.
