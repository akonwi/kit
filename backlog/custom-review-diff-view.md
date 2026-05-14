# Custom review diff viewer

## Context

`/code-review` previously delegated visual diff rendering to OpenTUI's built-in `diff` renderable. That made basic unified/split rendering easy, but constrained review-specific UX such as comment lanes, cursor placement, hunk controls, richer gutters, and future inline review affordances.

## Direction

Build Kit's review diff surface from Pierre's parsed diff model (`@pierre/diffs`) and Kit-owned row data instead of treating diff rendering as an opaque widget.

Initial goals:

- preserve current unified and split review flows
- keep cursor/range/comment marker positioning deterministic
- render line numbers, signs, backgrounds, hunk headers, and skipped context with Kit theme tokens
- keep raw-patch fallback for metadata-only changes

Future improvements to evaluate:

- inline add/edit comment affordances in the gutter
- richer hunk headers and per-hunk actions
- better split-view alignment for complex change groups
- optional syntax highlighting without giving up row ownership
- mouse support for selecting/commenting review lines
