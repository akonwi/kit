# Browser-backed Code Review

Kit provides two different diff-review surfaces:

- `/diff` — a terminal-native diff viewer for quick inspection
- `/code-review` — a browser-backed review flow for richer comment authoring

`/code-review` launches the system browser and connects that browser UI back to Kit through a localhost review host.

Current behavior:

- Kit gathers the current review state for the active session
- Kit starts a localhost-backed review host
- Kit opens the system browser
- the browser UI connects back to Kit over WebSocket
- the browser UI can fall back to HTTP state loading when live connection is unavailable
- submitting a review sends a structured review payload back to Kit

When the user submits a review from the browser:

- Kit receives the structured review payload
- Kit stores that review as a pending attachment
- the submitted review is not appended as its own standalone user message
- the next user message can include that review attachment as structured context

Current review submissions include file comments and same-side line-range comments.

Some surrounding UX is still evolving, including richer transcript rendering and more polished attachment presentation.

## How to access it

Run:

```text
/code-review
```

Use `/diff` instead if you want to stay entirely in the terminal and inspect the current working-tree diff there.
