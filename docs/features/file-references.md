# File References

Kit lets users reference files directly from the composer.

Current behavior:

1. type `@`
2. a filterable file picker opens
3. select a file or directory
4. the composer inserts an `@path/to/file` reference at the cursor

The picker displays plain relative paths, while the inserted composer token uses `@` as the actual reference marker.

File references are backed by a lazy file index:

- scanning happens on demand rather than at app startup
- results respect built-in excludes and configured ignore rules, including `.gitignore` and `.kitignore`
- the picker shows files and directories from the current project tree
- selecting an item replaces the current `@...` token in the composer
- the index can be invalidated after file-affecting tool activity so suggestions stay fresh

## How to access it

Type `@` in the composer.
