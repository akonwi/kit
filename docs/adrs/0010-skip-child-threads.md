# 0010: Skip child thread tree navigation

## Status
Accepted

## Context

Session branching and branch-tree navigation are related but different concerns.

Kit needs a simpler model for now.

## Decision

Do not implement in-session child thread tree navigation.

## Rationale

A child-thread tree model combines two different ideas:

1. branching within a single session history
2. browsing and navigating that branch tree in place

Kit uses a lighter-weight cross-session alternative through `/handoff`:

- create a new child session
- preserve the parent session as-is
- record lineage with `parentSessionId`

This captures the most useful workflow without introducing full in-session tree navigation semantics.

## Trade-offs

- no `/tree`-style navigation within a single session
- no visual lineage browser yet
- copied handoff sessions retain full context instead of reducing it automatically

## Future

If demand appears for true in-session branching and tree navigation, it can be revisited separately from the current cross-session handoff model.

## Related

- `docs/features/handoff.md`
