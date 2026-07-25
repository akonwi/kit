# UI mockups

These standalone HTML artifacts capture design directions for reference. They are exploratory, not implementation specifications. The accepted shell direction is documented in [`../shell-workspace-plan.md`](../shell-workspace-plan.md).

## Persistent code review pane

[`code-review-pane.html`](./code-review-pane.html) demonstrates:

- a resizable transcript and review workspace
- stacked files and diff at normal wide widths
- side-by-side file tree and diff when the review pane has enough room
- a tabbed transcript/review presentation at narrow widths
- minimized review affordance, updated shell header, and status footer

Open it in a browser or with:

```sh
glimpse open --name kit-review-pane --replace docs/mockups/code-review-pane.html
```
