# Session explorer modal polish

## Status

- [ ] not started

## Context

A first pass of the `/tree` session explorer modal exists and is good enough for early usage, but needs more real-world tree data before deeper polishing work is worth implementing.

## Follow-up ideas

### Tree readability
- Stronger distinction between selected row and current session
- Better current-session marker
- Truncate or align long names more gracefully
- Consider aligned metadata columns for short id / updated time
- Improve root and ancestor visual treatment

### Details pane
- Show lineage path from root to selected session
- Show parent session name, not only parent id
- Show child count
- Show model used by that session
- Improve first-message preview wrapping/truncation

### Large trees / navigation
- Add viewported scrolling for long trees
- Keep selected row visible while navigating
- Consider page-up/page-down behavior later

### Session actions in modal
- Rename selected session in place
- Delete selected session with confirmation
- Refresh tree after mutation
- Prevent deleting the active session

### Relation labeling
- Better labels for root / ancestor / current / child / descendant / sibling
- Possibly show distance from current session

### Modal polish
- Subtitle with root session name
- Better loading / empty / error states
- More contextual footer hints per mode

## Deferred until more usage
Wait for more actual handoff/session tree usage before implementing these improvements.
