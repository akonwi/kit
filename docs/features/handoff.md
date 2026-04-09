# Handoff

## Status

Available now.

## Goal

Allow the current session to fork into a new linked child session without
rewriting or summarizing the prior conversation.

## Current behavior

`/handoff` is a fork-like command.

When invoked, Kit:

1. clones the current session's turns into a new session
2. sets `parentSessionId` on the child session
3. names the child session `handoff: {parentName}`
4. switches to the child session immediately

No summary or compaction-style synthetic prompt is generated.

## Optional first message

`/handoff` also accepts an optional inline message:

- `/handoff` — fork and switch only
- `/handoff continue with the refactor` — fork, switch, and submit
  `continue with the refactor` as the first new user message in the child
  session

The copied history remains unchanged; the optional message is simply the first
new turn in the child session.

## Why this is useful

- branch into a new direction without losing prior work
- preserve the full parent conversation in the child
- keep formal lineage between sessions via `parentSessionId`
- avoid lossy handoff summaries

## Current limits

- handoff does **not** reduce context usage; it copies the full session history
- empty sessions cannot be handed off yet
- lineage is currently visible in `/debug`, but not yet surfaced more broadly in
  session-management UI

## Source

- `src/features/commands/handoff.ts`
- `src/runtime/agent-runtime.ts`
- `src/session/types.ts`
