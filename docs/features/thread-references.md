# Thread References

Allows referencing content from other sessions via `@@` trigger.

## Trigger

Type `@@` in the composer to open a filterable thread picker.

## How it works

1. `@@` trigger detects the double-@ prefix before the cursor
2. Opens a fuzzy-scored picker showing recent sessions
3. On selection, inserts `[[thread:sessionid]] ` at cursor position
4. On submit, the token is expanded: reads the referenced session's context and injects a formatted reference block into the prompt

## Thread Index

- Lazy session index that scans session files on demand
- Fuzzy scoring by session name/title
- Invalidates on session changes (create, switch, rename)

## Source

`src/features/threads/`
