# File References

Allows referencing files in the composer via `@` trigger.

## Trigger

Type `@` in the composer to open a filterable file picker.

## How it works

1. `@` trigger detects the prefix before the cursor
2. Opens a fuzzy-scored picker with lazy file scanning
3. Respects `.gitignore` and `.pi-ignore` patterns
4. On selection, inserts `@filename ` at cursor position
5. On submit, the `@filename` is passed to the agent which resolves it to the file content

## File Index

- Lazy file scanner that indexes files on demand
- Fuzzy scoring: exact match > prefix > substring > subsequence
- Auto-invalidates every 5 tool completions to refresh index

## Source

`src/features/files/`
