# Thread References

## Status

Available now.

## Current UX

Users can reference other sessions/threads directly from the composer.

1. Type `#`
2. A filterable thread picker opens
3. Select a thread
4. The composer inserts a marker in this format:
   - `[thread:<id>:<name>]`
5. On submit, that marker is expanded into a bounded thread reference block
   before the message is sent to the agent

## Current behavior

Thread references are backed by a cached session index.

- the picker excludes the active session
- inserted references use a short id prefix plus the selected thread name
- the submitted message expands references by resolving the referenced session
  from Kit storage
- expansion currently produces metadata-only context, not sampled thread
  transcript content

Expanded block fields currently include:

- thread id
- title
- cwd
- updated timestamp
- turn count
- message count

## Why metadata-only expansion

Kit currently expands thread references into metadata rather than sampled thread
messages.

That keeps expansion lightweight and deterministic while still giving the model
an explicit thread anchor it can act on.

## Relevant modules

- `src/features/threads/thread-index.ts`
- `src/features/threads/expand-references.ts`
- `src/features/threads/index.ts`
- `src/shell/composer-controller.ts`

## Source

`src/features/threads/`
