# Handoff

`/handoff` forks the current session into a new linked child session without rewriting or summarizing the prior conversation.

Current behavior:

1. Kit clones the current session's turns into a new session
2. it sets `parentSessionId` on the child session
3. it names the child session `handoff: {parentName}`
4. it switches to the child session immediately

No summary or compaction-style synthetic prompt is generated.

`/handoff` also accepts an optional inline message:

- `/handoff` — fork and switch only
- `/handoff continue with the refactor` — fork, switch, and submit `continue with the refactor` as the first new user message in the child session

The copied history remains unchanged; the optional message is simply the first new turn in the child session.

Current limits:

- handoff does not reduce context usage; it copies the full session history
- empty sessions cannot be handed off yet
- lineage is visible in `/debug`, but not yet surfaced more broadly in session-management UI

## How to access it

Run:

```text
/handoff
```

Optionally add a first message after the command.
