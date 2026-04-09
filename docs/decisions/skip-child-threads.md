# Skip Child Threads

**Date:** 2026-03-17

## Decision

Do not implement Pi-style child thread tree navigation inside a single session.

## Rationale

Pi-style child threads combine two related but different ideas:

1. branching within a single session history
2. browsing and navigating that branch tree in-place

Kit now supports a lighter-weight cross-session alternative through `/handoff`:

- create a new child session
- preserve the parent session as-is
- record lineage with `parentSessionId`

This captures the most useful workflow without adopting full in-session tree
navigation semantics.

## Trade-offs

- no Pi-style `/tree` navigation within a single session
- no visual lineage browser yet
- copied handoff sessions retain full context instead of reducing it automatically

## Future

If demand appears for true in-session branching and tree navigation, it can be
revisited separately from the current cross-session handoff model.
