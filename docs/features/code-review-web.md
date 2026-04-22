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

Likely later refinements:

- localhost HTTP + WebSocket for live review state
- localhost HTTP + POST for one-shot submit or export flows
- static asset serving instead of inline prototype HTML

## Inspiration

- `pi-diff-review`
- `glimpse`

These are still useful references for the browser review UX and host/UI split, even though the current prototype direction is to use the system browser directly rather than a Glimpse window.

## Current prototype scope

The first prototype is intentionally small:

- `/code-review` opens a browser page on localhost
- the page connects back to Kit over WebSocket
- Kit sends session state and runtime events to the page
- the page can send test messages back to Kit

This is only a transport and shell prototype. It is not yet a real diff review UI.
