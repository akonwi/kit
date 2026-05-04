# Alternative split-diff view for in-TUI review

## Context

The current in-TUI review flow uses a unified diff view for whole-file browsing and line/range comments.

Unified diff is compact and works well for linear scanning, but some review tasks are easier in a split-diff presentation where deletions and additions are shown side by side.

## Why explore it

A split-diff mode could help with:

- clearer before/after comparison for modified lines
- easier visual mapping between removed and added code
- reduced ambiguity in mixed change groups
- more intuitive comment targeting when thinking in terms of old-vs-new sides

This may be especially useful for:

- refactors with nearby add/delete blocks
- code motion or rewrite-heavy changes
- review flows that want stronger side awareness for comments

## Open questions

- should split diff be an alternate toggle inside `/diff` or a separate review mode?
- should unified remain the default?
- how should line/range comments map into split presentation while keeping the same attachment schema?
- how should saved comment gutter markers work in split mode?
- what is the best layout in narrow terminals?

## Suggested direction

Treat this as an optional alternate presentation, not a replacement for unified diff.

A likely first step:

1. keep unified diff as default
2. add a toggle for split view when terminal width is sufficient
3. preserve the same review draft model and attachment output
4. compare usability for line/range comments before expanding further

## Non-goals

- do not replace the existing unified diff flow by default without clear evidence
- do not introduce a second review attachment model
- do not require split mode in narrow terminal widths where it harms readability
