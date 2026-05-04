# Ataraxy review integration

## Summary

If Kit revisits Ataraxy integration, treat the two tools as complementary rather
than interchangeable.

- `sem` is a better fit for **semantic diff/navigation**
- `inspect` is a better fit for **review triage/prioritization**

Kit's existing line-based patch viewer should remain the exact inspection layer.

## Current Kit baseline

Current review/diff tooling is fundamentally:

- file-based
- hunk-based
- line-based

This is good for exact patch inspection but weaker for:

- semantic rename/move understanding
- prioritizing risky changes
- grouping tangled changes
- de-emphasizing cosmetic noise

## Recommended framing

Use Ataraxy output as an additional lens on top of Kit's existing raw patch
viewer, not as a full replacement for it.

### `sem`

Best fit:

- changed entity listing
- semantic diff summaries
- rename/move detection
- entity-first navigation before drilling into raw hunks

Think:

> What changed semantically?

### `inspect`

Best fit:

- risk scoring
- blast radius / dependency-aware review ordering
- logical grouping of independent changes
- top-level review verdicts and triage

Think:

> What should be reviewed first?

## Preferred product direction

If explored in Kit later:

- keep the existing patch viewer as the source of truth for exact line review
- use `inspect` to drive triage and prioritization
- use `sem` to improve semantic navigation and summaries

Potential eventual layering:

- `inspect` chooses **what to review first**
- `sem` explains **what entity changed**
- Kit patch UI shows **the exact lines**

## Integration style

These tools should likely be treated as optional external backends.

Expected approach:

- shell out to installed binaries
- consume JSON output where possible
- degrade gracefully when tools are unavailable

Avoid making them hard runtime dependencies in the first pass.

## Suggested first spike

If revisited, start with `inspect` first rather than `sem`.

Reason:

- Kit already has a workable raw diff viewer
- the larger gap is review guidance and prioritization
- prioritized review lists pair naturally with future in-TUI modal review flows

Suggested first spike:

1. shell out to `inspect ... --format json`
2. map the result into a small Kit adapter model
3. prototype a ranked review list in the TUI
4. drill down into the existing patch viewer for exact inspection

## Risks / trade-offs

- external CLI dependency and version management
- unsupported-language / unsupported-repo cases
- mismatch between entity-level triage and Kit's current file/hunk note model
- feature complexity if too many review modes appear at once

## Recommendation

When revisiting this area later:

- start narrow
- use `inspect` first for triage
- keep raw patch review central
- add `sem` later if semantic navigation proves worthwhile
