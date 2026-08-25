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
- the initial `@` is a provisional picker trigger and is not retained in the composer
- typing `@@` during the grace period cancels the pending picker and leaves one literal `@`; the same escape works after the picker opens
- opening has a 250ms escape grace period, measured from the trigger press, so a quick `@@` never flashes the picker; file loading happens concurrently and text typed during that period becomes the initial filter
- whitespace restores the provisional `@` as literal text instead of opening the picker
- changing focus or opening another picker cancels the pending reference cleanly
- the double-trigger escape also works while the file index is still loading
- the index can be invalidated after file-affecting tool activity so suggestions stay fresh

## How to access it

Type `@` in the composer. Type `@@` to cancel the reference interaction.
