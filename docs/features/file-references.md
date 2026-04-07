# File References

## Status

Foundation exists, but the inline `@` reference UX is not fully wired in the
current minimum loop.

## Goal

Allow users to reference files from the composer via an `@`-triggered picker.

## Current foundation

The codebase already includes:

- lazy file scanning
- fuzzy scoring helpers
- file index invalidation after tool activity

Relevant modules:

- `src/features/files/file-index.ts`
- `src/features/files/scan-files.ts`
- `src/features/files/score.ts`

## Intended UX

1. User types `@` in the composer
2. A filterable picker opens with file suggestions
3. Selecting a file inserts a file reference at the cursor
4. On submit, the referenced file is resolved into agent-visible context

## Current caveat

The current composer controller only wires slash-command palette behavior. File
reference trigger handling still needs to be rebuilt on top of the current shell
and palette architecture.

## Design notes

The file index is intended to:

- scan lazily rather than eagerly indexing the whole tree up front
- respect ignore rules
- invalidate periodically after tool completions so file suggestions stay fresh

## Source

`src/features/files/`
