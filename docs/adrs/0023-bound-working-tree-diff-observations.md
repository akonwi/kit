# 0023: Bound working-tree diff observations

## Status

Accepted

## Context

ADR 0020 requires an authoritative Diff surface, but ordinary workspace reads
from ADR 0019 are not coherent Git observations and Git worktree diff commands
can execute repository-configured clean filters. `CORE-DIFF-001` needs a
working-tree-only contract whose evidence remains bounded and whose stale,
binary, unsupported, and incomplete states are not collapsed into patches or
renderer policy.

The implementation research and executable probes are recorded in
[`../research/core-diff-001.md`](../research/core-diff-001.md) and
[`../research/core-diff-001-followup.md`](../research/core-diff-001-followup.md).

## Proposed decision

### Target semantics

R1 exposes one logical target kind: `working_tree`. Its old side is exact `HEAD`
or empty for an unborn repository; its new side is the literal live filesystem.
The index is a coherence and classification input, not a diff side. Pure
index-only changes are not changed files, and staged content reverted in the
live file is unchanged. The target reports whether the index is clean, diverged,
or conflicted.

A target ID is stable for the session/workspace/repository/policy identity. An
opaque target revision identifies one retained observation. Updating a revision
refreshes an existing Diff pane; it does not change target identity. An active,
visible Diff pane may poll for a fresh observation on a bounded client-owned
schedule. Clients compare deterministic revisions, keep at most one poll in
flight, stop polling when hidden, and defer applying a changed revision while a
review range or annotation editor is active. Once a changed revision is pending,
polling pauses so retained evidence for that interaction is not displaced. Server-pushed invalidation may
later replace the timer without changing observation authority.

R1 requires the repository worktree root to equal the session workspace root.
It accepts an in-tree `.git` directory or a validated same-owner linked-worktree
gitdir/common-dir. It rejects ancestor/descendant root mismatch, bare repos,
sparse checkout/index, partial clones, object alternates, hostile config
includes or `core.worktree`, unsafe metadata ownership/permissions,
unsupported repository extensions, and arbitrary gitfiles. After discovery,
every command uses explicit validated git-dir and work-tree values and verifies
the resolved dirs. Replace refs and lazy fetch are disabled.
Submodules and nested repositories are never traversed.

### Coherent observations

The server uses sanitized, bounded, direct-exec Git plumbing only for exact
HEAD/tree objects, semantic index records, eligible untracked paths, effective
attributes, and allowlisted config. Every repository invocation disables
fsmonitor, pager, prompts, inherited Git configuration, lazy fetch, and replace
objects. Git never receives live content for conversion or diffing.

One attempt records control state, repeats it before content reads, acquires and
hashes live files through ADR 0019's descriptor-anchored workspace authority,
then repeats control state and file identity checks. Any mismatch discards the
attempt. The server tries at most three times under one deadline, then returns
`stale_target`.

The retained manifest includes policy/limit versions, workspace and repository
identities, exact HEAD, tree/index/untracked/config/attribute observations, and
sorted per-path old object plus live kind/mode/identity/content digest or typed
state. A versioned SHA-256 canonical encoding derives target revisions. File
revisions exist only when each side has exact evidence: kind/mode/content digest
or a canonical revalidated absent state. A skipped token is not a content
identity.
Incomplete observations include exact truncation records and never claim to be
complete.

Observations expire after 120 seconds and are bounded to two/64 MiB per session
and eight/256 MiB daemon-wide. Authenticated cursors bind session, workspace,
target/revision, operation, path/file revision, page bounds, and offset.
Evicted, expired, mismatched, or tampered cursors are `stale_cursor`.

Changed-file pages use retained observations. A guarded file-diff read
revalidates workspace/root, Git control state, all candidate metadata, and the
requested file digest. Workspace change is `stale_workspace`; target control or
other candidate change is `stale_target`; requested file change is
`stale_file`.

### Supported content

Conflicted and intent-to-add paths receive summaries but no hunks. Assume-
unchanged is only an index hint and remains supported. Skip-worktree or sparse
index rejects the target. Symlinks, special files, binary text projections,
and unavailable objects have typed file states. Gitlinks/submodules, nested
repositories, oversized files, aggregate-budget overflow, and unexamined
candidates are unresolved with unknown changedness and make the observation
incomplete; index metadata never manufactures live submodule changedness.

Git resolves attribute precedence, but Kit executes no filter, external diff,
textconv, encoding, ident, pager, hook, helper, or remote operation. R1 supports
only literal/no-op old-side projection: filter/encoding/ident must be absent or
unset; `-text` is literal; explicit LF text is supported only for already
normalized blobs; unspecified text requires `core.autocrlf=false`. Automatic
text detection and CRLF-producing transformations are `unsupported_transform`.
`-diff` is binary; named diff drivers are ignored.

A regular path is called changed only after complete evidence on both sides;
verified absence is exact evidence for additions and deletions. Text must be
complete UTF-8 without NUL, at most 1 MiB, 100,000 logical lines, and 64 KiB per line. Aggregate observation content is at most 32 MiB across 4,000
candidates. Non-canonical or unsafe Git path bytes are counted as
`unsupported_path` omissions and make the observation incomplete; they are
never lossy-encoded. Git symlinks are classified with a no-follow workspace
operation rather than ADR 0019's final-symlink file read.

### Semantic contract

The session-client facet exposes operations equivalent to:

```text
observeWorkingTree(workspaceId, pageSize?, cursor?)
  -> observation + ordered changed-file summaries + next cursor

readFileDiff(targetId, targetRevision, path, expectedFileRevision?,
             pageSize?, cursor?)
  -> guarded file state + semantic hunk fragments + next cursor
```

Summaries contain canonical path, optional exact file revision, change kind
(`added`, `deleted`, `modified`, `mode_changed`, or `unknown`), old/new kind and
mode, content state/reason, and optional known addition/deletion counts. The
observation separately carries structured truncation and counted omission
records. R1 does not pair renames or copies. Live regular mode is `100755` when
the owner-execute bit is set and `100644` otherwise, compared with HEAD even
when `core.filemode=false`; the config remains a coherence input only.

Hunks carry explicit one-based old/new starts and counts. Zero is allowed only
for an empty side. Lines are `context`, `deletion`, or `addition`, with nullable
old/new line numbers, content excluding LF, and `hasTerminatingLF`. CR is
preserved. Pagination may split a hunk only on line boundaries and marks both
continuation directions. Input size limits bound observation and computation;
they do not cap a successful response to one page. A successful computation is
retained and all semantic hunk lines remain reachable through cursors.

Changed-file pages default/max to 100/200 records. Hunk pages default/max to
500/1,000 lines and 10/20 fragments. Every encoded response is at most 512 KiB.
Expected binary/unsupported/skipped/complex states are successful typed records.
Closed file reasons are `nul`, `malformed_utf8`, `attribute_binary`, `filter`,
`encoding`, `ident`, `text_conversion`, `symlink`, `submodule`, `special`,
`nested_repository`, `file_bytes`, `line_count`, `line_bytes`,
`missing_object`, and `read_unavailable`. Observation truncation is one of
`candidate_limit`, `byte_limit`, `observation_limit`, or `deadline`; counted
omissions are `unsupported_path` or `unexamined_candidate`. Fatal codes are
`invalid_path`, `not_repository`, `unsupported_repository`, `stale_workspace`,
`stale_target`, `stale_file`, `stale_cursor`, `not_found`,
`permission_denied`, `limit_exceeded`, `capacity_exceeded`,
`repository_unavailable`, and `unavailable`; cancellation remains cancellation.
Target
coverage, edit-computation state, and page continuation are distinct fields:
`observation.complete`, `computation.state`, and `nextCursor` respectively.

Kit owns a deterministic patience-anchor plus linear-space LCS differ. It is
bounded to 4,000,000 comparisons, 16 MiB structurally preallocated scratch,
20,000 edit records, and 2,000 hunks, with periodic cancellation. Exhaustion returns `too_complex` with no
partial script. The contract does not promise Git-identical or globally minimal
presentation.

An ADR 0022 working-tree diff annotation anchor pins target ID/revision,
path/file revision, side, and one-based range. Commit, branch, review, rename,
and copy targets remain future work.

## Implementation verification

- hostile-repository probes prove no helper, filter, hook, remote, lazy fetch,
  pager, external diff, textconv, alias, or credential execution;
- concurrent changes at every observation phase produce retry or the specified
  stale class, never mixed evidence;
- sparse/index/conflict/ITA/unborn/linked-worktree and authority fixtures cover
  the supported semantics in automated tests; reproducing the full fixture
  matrix on every release target and Git boundary is not a release gate;
- edit scripts reconstruct both inputs under property tests, sanitized-oracle
  differential tests, adversarial repeated/disjoint inputs, fuzzing, benchmarks,
  cancellation, and every output bound; and
- protocol/server/client tests preserve exact revisions, coordinates, typed
  states, truncation, cursor expiry, and annotation-ready identity.

## Consequences

Clients receive renderer-neutral, revision-pinned evidence without executing
Git or parsing patches. The conservative repository and attribute policy makes
some valid repositories or files unavailable in R1, but avoids invented
content, authority expansion, and process execution. Retained observations and
revalidation add memory and I/O cost, bounded by explicit admission and cache
limits.

This proposal does not implement the service and does not mark `CORE-DIFF-001`
complete.
