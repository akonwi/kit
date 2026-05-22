# 0016: Merge child sessions back into parent context as synthetic summaries

## Status
Accepted

## Context

`/handoff` creates a linked child session by copying the parent's persisted turns and then continuing work in that child.

That branching flow is useful for side quests, but without a merge path it leaves the user with two awkward options:

- continue in the child forever
- manually copy the child outcome back into the parent

Kit needs a lightweight child-to-parent merge operation that preserves the parent's conversation while making the child outcome visible and resumable.

This is not a general arbitrary session merge system. It is a narrower workflow for finishing a handoff child and merging its useful result back into its parent.

## Decision

Kit supports merging the current child session back into its parent as a concise synthetic summary.

The merge flow is defined as follows:

- handoff children persist lineage metadata:
  - `parentSessionId`
  - `forkedFromTurnId`
- merge-up is initiated from the session explorer exposed by `/sessions`, not from a dedicated squash command
- squash is only available when the selected row is the current session and that session is a child
- merge logic operates on the current session only via `runtime.mergeUp()`
- merge boundaries use `forkedFromTurnId`, not shared-prefix heuristics or turn counts
- if the stored fork boundary no longer exists in child history, the whole child session is summarized as a fallback
- the generated result is appended to the parent as a synthetic `handoff-summary` turn
- after a successful merge, runtime switches back to the parent and deletes the child session

## Summary generation

Child-session merge summaries use the same shared summary-generation infrastructure as compaction, but with a different prompt.

The merge summary is tuned for the parent's needs. It should focus on:

- the branch goal
- progress and outcomes in the child
- key decisions or code changes
- remaining issues
- context the parent should preserve before continuing

This keeps the merge note useful as resumable context rather than as a continuation of the conversation.

## Boundary model

The post-fork boundary is defined by `forkedFromTurnId` captured when the child is created.

Merge behavior:

1. find `forkedFromTurnId` in the child history
2. if found, summarize only turns after that boundary
3. if not found, summarize the entire child session

This decision intentionally avoids reconstructing boundaries from shared prefixes.

## UI entry point

The merge action belongs to the session explorer exposed by `/sessions`.

Rationale:

- squash is a session-lineage operation
- the session explorer presents saved sessions as a unified forest rooted at top-level sessions
- this avoids adding another top-level slash command for a specialized workflow

Because merge deletes the child after success, the `/sessions` flow requires confirmation before continuing.

## Parent transcript behavior

The merge result is stored in the parent transcript as an explicit synthetic assistant message with kind `handoff-summary`.

The transcript should render this as a dedicated merge note rather than as a normal assistant reply.

Current direction:

- divider-style transcript row
- collapsed by default
- expandable with `▸ / ▾`
- plain indented body showing the structured summary sections

This keeps the merge visible and inspectable while preserving transcript readability.

## Operation order

The preferred merge order is:

1. generate the child summary
2. keep that summary in memory
3. switch to the parent
4. append the synthetic summary turn to the parent
5. persist the parent
6. delete the child

This minimizes the chance of losing the generated result during the session switch.

## Consequences

### Positive

- side-quest work can return to the parent without manual copy/paste
- lineage is explicit and machine-readable
- merge behavior survives compaction because it does not rely on turn counts
- the parent transcript stays inspectable because the merge result is persisted as a normal turn
- compaction and merge-up share one summary-generation path instead of duplicating logic

### Trade-offs

- merge is destructive for the child session because successful squash deletes it
- the parent transcript contains generated content rather than only live conversation turns
- old child sessions without a usable fork boundary produce less precise whole-session summaries

## Related

- `docs/adrs/0009-compaction-strategy.md`
- `docs/adrs/0010-skip-child-threads.md`
- `docs/features/handoff.md`
