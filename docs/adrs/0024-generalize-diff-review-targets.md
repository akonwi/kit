# 0024: Generalize diff review targets

## Status

Accepted

## Context

ADR 0023 exposes one authoritative `working_tree` target comparing exact `HEAD`
with the live filesystem. The retained Diff pane, semantic hunk renderer, and
revision-pinned annotations consume that contract. Review also needs to inspect
one committed change or the total change on a branch without making clients run
Git, parse patches, or become evidence authorities.

A selectable Git revision is not an observation revision. Users select a target
specification; the server resolves and pins its Git endpoints, then derives an
opaque observation revision from the resulting evidence. Conflating those
identities would allow a moving ref to silently rebind comments or cursors.

## Decision

### Target kinds and identity

Kit supports these diff targets:

- `working_tree`: exact `HEAD` or an empty tree versus the literal live
  filesystem, as defined by ADR 0023;
- `commit`: the selected commit's first parent versus that commit, or the empty
  tree versus a root commit; and
- `branch`: the merge base selected for a local branch comparison versus the
  branch head pinned when the target is selected.

A target specification contains canonical full object IDs. Branch names,
subjects, abbreviated IDs, and relative times are display metadata and never
identify evidence. Merge commits use the first parent in commit mode. A branch
ref moving after selection does not mutate an open target.

`targetId` is a deterministic opaque identity for the session, workspace,
repository authority, policy version, target kind, and pinned endpoints.
`targetRevision` identifies one exact retained observation. `fileRevision`
identifies exact old/new evidence for one path. Clients never manufacture or
reinterpret these values.

The working-tree target retains its stable logical target ID while its
observation revision changes. Commit and branch target IDs include their pinned
endpoints and are immutable.

### Server-owned target catalog

The session-client contract exposes a bounded target catalog. It returns:

1. the working tree;
2. the current branch versus its resolved default/local base when meaningful;
3. bounded local branch-base choices; and
4. at most 20 recent commits, newest first.

Catalog entries contain an authenticated opaque target reference plus target
kind, full pinned endpoints, bounded display metadata, and optional target
annotation count. The opaque reference binds the session, workspace,
repository authority, policy version, target kind, pinned endpoints, and expiry.
The server snapshots repository state for one catalog response. It does not
contact remotes, fetch objects, execute helpers, or accept arbitrary ref syntax
from clients. Missing, ambiguous, unsafe, or unsupported refs are omitted with
bounded diagnostics.

Selecting an entry calls a generalized observation operation equivalent to:

```text
listDiffTargets(workspaceId) -> bounded target catalog
observeDiff(workspaceId, targetReference, pageSize?, cursor?)
  -> observation + ordered changed-file summaries + next cursor
readFileDiff(targetId, targetRevision, path, expectedFileRevision?,
             pageSize?, cursor?)
  -> guarded file state + semantic hunk fragments + next cursor
```

The client echoes only the server-issued opaque reference when selecting a
target; display OIDs and names are not selection authority. The server verifies
the reference and revalidates its resolved objects before use. Process and
protocol boundaries reject unknown target kinds, malformed object IDs, expired
references, endpoint mismatches, and cross-session or cross-workspace
references.

### Evidence and repository authority

ADR 0023 remains authoritative for working-tree observation, bounds, cursors,
and stale classes. Commit and branch observations reuse its repository
validation, response limits, semantic hunk contract, differ, cache ceilings,
and authenticated cursors.

Committed targets read both sides from validated local Git objects. They never
read committed-side content from the live filesystem and never invoke filters,
textconv, external diff, hooks, pagers, credentials, lazy fetch, or remotes.
Missing objects fail closed. Object IDs are resolved and stored in full; short
IDs are presentation only.

Committed observations are immutable while their pinned objects remain
available. Ref movement does not make them stale. Repository authority or
object availability changes return typed unavailable/stale errors without
rebinding the target. Working-tree targets alone poll for newer observations.

### Annotation scope

The existing diff annotation wire anchor remains target-neutral in practice: it
pins target ID/revision, path, file revision, side, and one-based range. Its
`working_tree_diff` discriminator and field names remain compatible initially;
renaming them requires a separate explicit migration. The server persists the
pinned target definition needed to validate or reconstruct committed evidence.
Commit and branch annotations are separate target scopes and never attach to a
later ref value.

Switching targets does not delete annotations. Composer chips remain visible
across tabs; activating one selects the existing Diff tab, restores its exact
target, and reveals the pinned range. Committed evidence may be reconstructed
from its pinned objects after observation-cache expiry. Submission validates
that the exact evidence remains available and never silently rebinds line
coordinates.

### Native TUI

The retained Diff tab is one surface per workspace, not one tab per target.
Changing target replaces the pane's guarded target input without remounting,
reordering, or creating another tab.

The header identifies the active target:

```text
Diff › Working tree
Diff › a1b2c3d  Fix parser bounds
Diff › feature/diff-target vs main
```

The target label opens Kit's standard modal picker. Keyboard actions are:

- `g`: toggle between the working tree and the current `HEAD` commit;
- `Shift+G`: open the target picker.

The picker is filterable by subject, branch name, or abbreviated object ID. The
working tree is pinned first, a current branch comparison is pinned second when
available, and recent commits follow. The selected target has a check mark;
targets with annotations use the standard draft indicator. Escape closes the
picker without changing target. There is no arbitrary object-ID input or commit
graph.

A target switch cancels older work, keeps the previous presentation until the
new target's first coherent page arrives, and never mixes old files with new
target state. It preserves the selected path when that path exists in the new
target; otherwise it selects the first changed file. Line, hunk, and scroll
positions reset when their evidence cannot be mapped safely. An active range or
annotation editor blocks switching with concise warning feedback.

Commit and branch targets do not poll. The working-tree target retains bounded
active-visible polling. A committed target selected while the working tree is
dirty shows a transient informational notice. Empty, missing-object, stale,
unsupported, truncated, and error states remain explicit.

### Non-goals

This feature is not a history browser. It does not provide arbitrary revision
ranges, commit graphs, remote branch discovery, fetching, more than 20 recent
commits, multi-parent merge review, rename/copy pairing, or silent draft
coordinate reconciliation.

## Required properties and verification

Tests must establish:

- exact target identity and endpoint validation across local and daemon clients;
- root-commit, first-parent commit, branch merge-base, and moving-ref semantics;
- no remote, helper, filter, textconv, hook, pager, credential, or lazy-fetch
  execution while listing or observing targets;
- immutable committed evidence sourced from objects rather than the worktree;
- bounded catalog ordering, filtering metadata, cancellation, and diagnostics;
- target-switch generation guards with no mixed file lists or stale completion;
- in-place Diff-tab identity, path preservation, and safe navigation reset;
- working-tree-only polling and no hidden-pane or committed-target polling;
- target-scoped annotation creation, listing, chip activation, reconstruction,
  stale handling, submission, and persistence migration; and
- picker keyboard, mouse, focus, loading, empty, error, and draft-indicator
  presentation.

## Consequences

The server and protocol gain generalized target resolution and immutable
committed-object evidence. The TUI gains a compact review-target workflow
without adding a Review tab or Git authority to clients. Target-scoped
annotations require additional persisted identity, and retained committed
objects may disappear after repository maintenance; those cases fail visibly
rather than rebinding evidence.

## Related

- [ADR 0020: Add file, diff, and review workspace surfaces](0020-file-diff-review-workspace-surfaces.md)
- [ADR 0022: Model draft annotations as session inputs](0022-model-draft-annotations-as-session-inputs.md)
- [ADR 0023: Bound working-tree diff observations](0023-bound-working-tree-diff-observations.md)
- [Core backlog](../../backlog/core.md)
- [Native TUI backlog](../../backlog/tui.md)
