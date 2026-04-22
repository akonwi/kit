# Browser-backed code review (`/code-review`)

## Status

Proposed

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

Likely implementation options:

- localhost HTTP + WebSocket
- localhost HTTP + POST for one-shot submit

## Inspiration

- `pi-diff-review`
- `glimpse`

These are good references for the browser review UX and the general split between host app orchestration and browser rendering.
