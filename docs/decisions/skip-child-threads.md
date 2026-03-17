# Skip Child Threads

**Date:** 2026-03-17

## Decision

Do not implement child thread creation / continue-in-new-thread flows.

## Rationale

Child threads (similar to git branches) allow branching the conversation into a new session while preserving the parent. This would enable users to:

- Explore alternative approaches without affecting the main conversation
- Keep multiple solution attempts in separate threads
- Fork from any point in the conversation

However, this feature is not worth the implementation cost because:

1. **Handoff is sufficient** - The user uses `/handoff` to create a new session and revisits the parent if needed. This workflow provides similar benefits without the complexity of maintaining parent-child relationships.

2. **Low actual usage** - In practice, `/fork` and `/tree` were rarely used in Pi.

3. **Thread references already exist** - The `@@` picker and `[[thread:id]]` expansion already enable referencing content from other sessions when needed.

## Trade-offs

- Users who want true branching (keep parent + explore child simultaneously) would need to manually handoff
- No visual indication of thread relationships in the UI
- Simpler architecture without parent-child session metadata

## Future

If there's demand for this feature, it could be revisited. The existing thread reference system provides a foundation to build on.
