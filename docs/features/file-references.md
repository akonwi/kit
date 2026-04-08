# File References

## Status

Available now.

## Current UX

Users can reference files directly from the composer.

1. Type `@`
2. A filterable file picker opens
3. Select a file
4. The composer inserts an `@path/to/file` reference at the cursor

The picker displays plain relative paths, while the inserted composer token uses
`@` as the actual reference marker.

## Current behavior

File references are backed by a lazy file index.

- scanning happens on demand rather than at app startup
- results respect ignore rules
- the picker shows files and directories from the current project tree
- selecting an item replaces the current `@...` token in the composer

## Design notes

The file index is intended to:

- scan lazily rather than eagerly indexing the whole tree up front
- respect built-in excludes, `.gitignore`, and `.pi-ignore`
- invalidate after file-affecting tool activity so suggestions stay fresh

## Relevant modules

- `src/features/files/file-index.ts`
- `src/features/files/scan-files.ts`
- `src/features/files/score.ts`
- `src/shell/composer-controller.ts`

## Source

`src/features/files/`
