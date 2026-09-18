# CORE-DIFF-001 contract and design-gate follow-up

Date: 2026-09-17

## Executive summary

### Confirmed findings

- A working-tree diff cannot be one atomic Git/filesystem snapshot. It can be a
  bounded, coherent **retained observation**: fence Git-derived control state
  before and after secure live-file reads, retain the admitted bytes, and reject
  an attempt whenever a fence or file identity changes.
- Git 2.54.0 exposes sparse-index directory entries as stage-zero mode `040000`
  entries and non-cone exclusions as `S` (skip-worktree) files. Treating either
  as absent live files would invent deletions. Assume-unchanged appears as a
  lowercase `h` but does not prevent Kit from securely reading the file.
- Intent-to-add is not distinguishable from an empty stage-zero blob using
  `ls-files --stage` alone. The bounded difference between index-only raw
  output with `--ita-invisible-in-index` and `--ita-visible-in-index` identifies
  it without reading worktree content. Conflicts expose stages 1, 2, and 3.
- `check-attr`, `ls-tree`, and `ls-files` did not execute configured filters,
  textconv, external diff, pager, credential helper, or fsmonitor when invoked
  as specified below. `git diff HEAD --no-ext-diff --no-textconv` still executed
  a clean filter and remains forbidden. The earlier fsmonitor result remains
  confirmed: every repository-aware command needs `-c core.fsmonitor=false`.
- A linked worktree uses a `.git` file, a per-worktree gitdir, and a separate
  common dir. Object alternates and replace refs materially change object
  authority. `GIT_NO_REPLACE_OBJECTS=1` neutralized replacement traversal.
- The checked-in patience-plus-linear-space-LCS prototype reconstructs both
  inputs, is cancellable, enforces work/edit bounds and a conservative scratch
  admission estimate, agrees with a
  sanitized Git oracle on the checked cases, and fails quickly on 5,000-line
  disjoint input. It is viable as an owned bounded R1 algorithm.

### Decisions proposed for R1

- Support only a repository whose worktree root exactly equals the session
  workspace root. Accept a normal in-tree `.git` directory and a strictly
  validated linked-worktree gitfile/common-dir shape. Never broaden content
  authority beyond the workspace.
- The target is `HEAD` (or empty for unborn) versus the literal live worktree.
  The index is a control/classification input, never an intermediate diff side.
  Pure index-only content or mode changes are not changed files. Therefore a
  staged-then-live-reverted file is absent from the changed-file list.
- Reject sparse checkout/sparse index and partial clone at target scope. Expose
  conflicts and intent-to-add at file scope without hunks. Do not recurse into
  submodules or nested repositories.
- Support only attribute/config cases for which old-side checkout bytes are
  provably literal or a no-op. Fail closed on filters, `ident`,
  `working-tree-encoding`, automatic text detection, or CRLF-producing
  conversion. Never execute conversion or presentation helpers.
- Materialize and cache one bounded observation. List cursors page only that
  observation. A guarded file-diff read additionally revalidates target control
  state, all candidate metadata, and the requested file digest.
- Own a small patience-anchor plus linear-space Hirschberg implementation with
  a comparison budget. It produces a valid deterministic edit script, not a
  promise of Git-identical or globally minimal presentation.

### Rejected alternatives

- Git worktree diff/status as content authority: clean filters and fsmonitor can
  execute, and output is not safely bounded semantic evidence.
- A raw index parser: it duplicates Git index extensions and sparse/split-index
  compatibility. Use bounded Git plumbing plus the two ITA raw-index views.
- Supporting sparse checkout by treating absent skip-worktree paths as deleted:
  incorrect. Expanding sparse directories in R1: too much object/index policy.
- Implicit ancestor repositories or arbitrary gitfile/alternate authority:
  violates the workspace boundary.
- General autocrlf/encoding/ident reproduction in R1: version/platform-sensitive
  and insufficiently validated.
- Histogram diff: no R1 quality requirement justifies another heuristic and
  tie-breaking contract. Unbounded trace-space Myers is also rejected.

### Open risks

Only macOS arm64, Git 2.54.0 (Apple Git-157), and Go 1.27.1 were exercised.
Linux amd64/arm64, case-sensitive filesystems, the oldest supported Git,
SHA-256 repositories, linked-worktree permission variants, and CRLF-native
platform behavior still require CI fixtures. No fence detects a malicious
same-user ABA mutation that changes and restores all observed state between
checks. R1 promises bounded coherent evidence under observable mutation, not a
kernel snapshot.

### Implementation phases

1. Repository authority/discovery and sanitized bounded Git runner.
2. Control-state observation, retries, secure live-content acquisition, and
   retained cache.
3. Attribute classifier and old-side projection for the deliberately narrow R1
   matrix.
4. Owned bounded semantic differ and hunk pagination.
5. Protocol/session-client projections, diff annotation anchor, and adversarial
   integration matrix. Backlog completion remains unchanged until all gates pass.

## Scope and evidence

This follows `core-diff-001.md`, ADRs 0019, 0020, and 0022, the current
`internal/workspace` secure I/O, and `internal/vcs` process runner. There was no
additional coordinating-session contract artifact; its intended split was a
stable logical working-tree target plus revision-guarded changed-file and hunk
operations.

Reproduction artifacts are:

- `core-diff-001-followup-probes.sh`: sparse, index, attribute/helper, repository
  authority, and semantic-fence probes in a private temporary tree;
- `core-diff-001-lab`: a separate Go research module containing the bounded
  algorithm, property/differential/adversarial tests, fuzz target, and benchmarks.

The shell probe uses no network and removes its temp directory. The Git oracle
runs `diff --no-index` from a neutral temporary directory with isolated config.

## 1. Observation protocol

### 1.1 Authority admission

An attempt starts by validating `(sessionID, workspaceID)` exactly as ADR 0019,
opening the workspace root, and recording its device/inode identity. Repository
validation is performed before general repository commands. All paths remain
canonical repository-relative paths; because R1 requires repository root equal
workspace root, they are also canonical workspace paths.

A Git child is direct-exec, process-group cancellable, deadline-bound, and has
bounded stdout/stderr. After filesystem discovery and config rejection, every
repository command receives explicit independently validated `--git-dir` and
`--work-tree` values and verifies the resolved dirs; it never relies on `-C` for
authority. Remove all inherited `GIT_*`; set an empty private
`HOME`/`XDG_CONFIG_HOME`, `GIT_CONFIG_NOSYSTEM=1`,
`GIT_CONFIG_GLOBAL=/dev/null`, `GIT_OPTIONAL_LOCKS=0`,
`GIT_TERMINAL_PROMPT=0`, `GIT_NO_LAZY_FETCH=1`,
`GIT_NO_REPLACE_OBJECTS=1`, `LC_ALL=C`; use `--no-pager`, literal pathspecs
where applicable, and `-c core.fsmonitor=false` before every subcommand.
Never return stderr.

### 1.2 One attempt

Each attempt has three control observations and one live acquisition:

1. **C0 / enumerate:** resolve exact `HEAD^{commit}` or unborn; read the HEAD
   tree; record the semantic index; enumerate eligible untracked paths; derive
   the candidate union; resolve effective attributes and allowlisted config.
   Reject any `core.worktree` key before these repository-aware commands.
2. **C1 / pre-read fence:** repeat HEAD, semantic index, eligible untracked set,
   effective attributes for the candidate union, and allowlisted config. Reject
   if it differs from C0. This closes races among the separate Git commands.
3. **F / content:** fetch only OIDs from the exact C0 HEAD through bounded
   `cat-file --batch`; securely open/lstat every live candidate through the
   workspace root; record kind, mode, identity, size, mtime, ctime, and SHA-256;
   read retained bytes within limits; re-stat/re-resolve after each read.
4. **C2 / post-read fence:** repeat C1, revalidate workspace/root identity, and
   re-resolve each candidate path. Candidate metadata must equal F. A requested
   absent path must still be absent. Only then may the observation publish.

The semantic index consists of sorted `ls-files --stage -v -z` records
(`-v`, not `-t`, is required for assume-unchanged), conflict stages, and the
sorted path delta between bounded raw cached-index commands using
`--ita-invisible-in-index` and `--ita-visible-in-index` against exact HEAD (or
the empty tree for unborn). Both raw commands also use `--no-renames`,
`--abbrev=64`, `--no-ext-diff`, and `--no-textconv`; a path present only in the
visible view as a zero-to-empty-blob addition is ITA. Unknown or inconsistent
record shapes fail closed.
Split index is left to Git. Sparse directory entries, skip-worktree flags, or a
sparse-checkout config fail the target before content acquisition.

The untracked digest is the complete bounded, sorted output of
`ls-files --others --exclude-standard -z` with external excludes disabled.
Attributes are the complete bounded effective `check-attr -z --all --stdin`
output for sorted candidates with external attributes disabled. Config is a
canonical encoding of only `core.filemode`, `core.autocrlf`, `core.eol`,
`extensions.objectformat`, sparse settings, partial-clone/promisor markers, and
worktree-config state, read without includes. Consequently an ignore or
attribute edit that changes no effective candidate/output does not stale an
observation; it is semantically irrelevant. An edit that changes admission,
classification, or projection changes a fence, as the probe demonstrates.

Retry at most twice after the initial attempt (three total), with one aggregate
8-second deadline. A fence or live-file race discards all attempt data. If the
third attempt races, return `stale_target`; resource overflow returns
`limit_exceeded`, not a retry. Cancellation is returned unchanged.

### 1.3 Manifest and revisions

The retained canonical manifest contains:

```text
policy/algorithm/limit profile versions
session ID, workspace ID, workspace root identity
validated worktree/gitdir/common-dir identities
HEAD state and exact commit OID
HEAD tree digest
semantic index digest and index summary
eligible untracked digest
allowlisted config and effective-attribute digest
sorted candidate records:
  path, old kind/mode/OID or absent,
  live kind/mode, identity metadata, content SHA-256 when admitted
all structured omission/unresolved/truncation records
```

`targetRevision = "diffrev_" + base64url(SHA-256(canonical manifest))`.
`fileRevision` hashes the policy version, target identity, canonical path, and
complete evidence for both sides. Side evidence is either kind/mode/content
digest (the HEAD OID is the old digest) or a canonical, revalidated absent
state. It is omitted when either side is unresolved; a typed skipped token is
never presented as a content identity.
Revisions are opaque, at most 128 bytes, equality-only, and not durable Git IDs. An incomplete observation
still needs an identity for paging, but carries `complete=false` and its exact
truncation reason; it is never represented as complete.

### 1.4 Cache, cursors, and follow-up guards

Retain manifests, summaries, and admitted old/new bytes for 120 seconds; at most
2 observations/64 MiB per session and 8/256 MiB daemon-wide, with fair
admission. Eviction is LRU within those ceilings. Every cursor is authenticated
and binds session, workspace, target ID/revision, operation, file revision where
applicable, page bounds, and next offset. Unknown, expired, evicted, tampered,
or cross-operation cursors return `stale_cursor`.

Changed-file pages come directly from retained sorted summaries; they do not
re-run Git. Before `readFileDiff`, recheck workspace/root, C2 control state, and
all candidate path metadata. Rehash the requested live regular file, or
revalidate its expected absence, and compare its file revision before using
retained bytes. Classify:

- workspace ID/root changed: `stale_workspace`;
- HEAD/index/untracked/attributes/config/candidate set or another candidate's
  observable metadata changed: `stale_target`;
- requested path identity/mode/bytes changed: `stale_file`;
- observation absent: `stale_target` for a named revision, `stale_cursor` for a
  cursor continuation.

A successful response is evidence from the retained exact revision. Mutations
that happen after the final guard create a newer state; they do not rewrite the
retained response.

## 2. Sparse, index, and repository states

| State | R1 result | Reason/classification |
| --- | --- | --- |
| normal stage-zero index | supported | index is a fence, not a content side |
| assume-unchanged | supported | ignore hint; secure live read remains authoritative |
| skip-worktree | reject target | `unsupported_repository`, reason `sparse_checkout` |
| sparse-index mode `040000` | reject target | `unsupported_repository`, reason `sparse_index` |
| cone/non-cone sparse checkout | reject target | never infer deletion from absent materialization |
| stages 1-3 | retain file summary, no hunks | `contentState=conflict` |
| intent-to-add | retain net-changed summary, no hunks | `contentState=intent_to_add` |
| gitlink/submodule | unresolved summary, unknown changedness | `contentState=unsupported_kind`; observation incomplete |
| staged then live-reverted | omitted | HEAD and live bytes/mode are equal |
| pure index-only content/mode change | omitted; target `indexSummary=diverged` | index is not the new side |
| unborn | supported | old tree is empty; index/untracked candidates with live content are additions |

For regular files, live Git mode is `100755` exactly when the host owner-execute
bit is set, otherwise `100644`; symlink/gitlink modes remain typed kinds. Compare
that live mode to HEAD regardless of `core.filemode`. The config value is a
fenced repository input, not permission to hide a literal live mode change.

An ITA file is not silently treated as an ordinary untracked addition because
its user intent and empty-index representation are materially different. A
conflicted path similarly gets no invented old/index side. Other paths remain
available when one path has either state.

## 3. Attribute and transformation policy

Git remains authority for effective attribute precedence: system/global sources
are disabled; `$GIT_DIR/info/attributes`, in-tree `.gitattributes` from root to
path, and the normal precedence rules are reflected by `check-attr`. Kit parses
only canonical NUL records and hashes the effective result.

R1 applies this fail-closed matrix to each path:

| Effective state | Result |
| --- | --- |
| `filter` unspecified or explicitly unset | allowed |
| any set/string `filter` | `unsupported_transform` |
| `working-tree-encoding` set/string | `unsupported_transform` |
| `ident` set/string | `unsupported_transform` |
| `ident` unspecified/unset | allowed |
| `-text` | literal blob bytes; `eol` must be unspecified |
| `text`/`eol=lf` | allowed only when the blob has no CR or NUL; projection is a proven no-op |
| text unspecified, eol unspecified, `core.autocrlf=false`, core.eol irrelevant | literal blob bytes |
| `text=auto`, unspecified text with autocrlf true/input, `eol=crlf`, or any conversion-producing combination | `unsupported_transform` |
| `-diff` | `binary` |
| set/string `diff` | ignored by Kit; never execute driver/textconv |

Only allowlisted canonical config values are accepted (`false`, `true`, `input`;
`lf`, `crlf`, `native`). Unknown values fail the path as unsupported. Literal
live bytes are always the new side. Old bytes are the exact HEAD blob only when
the matrix proves checkout projection literal/no-op. This deliberately avoids a
false LF-versus-CRLF diff without claiming a complete clone of Git `convert.c`.

A regular path is called changed only after both sides have complete evidence:
a content digest or canonical verified absence.
Files beyond the per-file or aggregate hashing/read budget are unresolved
summaries with `change=unknown`, no `fileRevision`, and an exact reason; they
make the target observation incomplete. Unexamined candidates are structured
omissions, never guessed changes.

Text requires complete admitted bytes, valid UTF-8, no NUL, at most 1 MiB, 5,000
logical lines, and 64 KiB per logical line. CR and missing final LF are preserved.
The diff service reuses workspace's opened-root and exact-name authority through
an internal port, but adds no-follow lstat/readlink operations: it must not use
ADR 0019's final-symlink file-read behavior when classifying Git symlinks.
NUL or malformed UTF-8 is `binary`; over-limit inputs are `skipped` with a
specific reason. Symlink, gitlink/submodule, special file, and nested-repository
content is typed unsupported and never traversed.

No selected operation invokes worktree conversion. The helper-marker probe
covers clean/smudge filter, fsmonitor, external diff, textconv, pager, and
credential helper. Aliases cannot replace built-in commands invoked as
`git <builtin>`. The selected plumbing invokes no hooks. `GIT_NO_LAZY_FETCH=1`
and rejection of partial-clone config prevent remote/lazy fetch; no command uses
credentials or remotes.

## 4. Repository metadata authority

R1 accepts only:

1. worktree root exactly equal to the securely opened workspace root;
2. either an in-root real `.git` directory, or a `.git` regular file containing
   exactly one bounded `gitdir: <path>` record resolving to a conventional
   linked-worktree gitdir;
3. a linked gitdir whose bounded `commondir` resolves to a real directory;
4. gitdir/common-dir and relevant control files owned by the effective user,
   with no group/world-writable metadata directory or file and no symlink in
   the admitted metadata chain; and
5. an object database physically under the admitted common dir.

Repository metadata outside the workspace is a separate, explicit read-only
metadata authority; it never authorizes content reads. A normal linked worktree
is therefore supported, while an arbitrary gitfile target is rejected unless
its `gitdir`, backlink, `commondir`, ownership, permissions, and worktree record
form one consistent Git linked-worktree shape.

Reject with `unsupported_repository` (stable reason in parentheses):

- repository root is an ancestor/descendant rather than equal
  (`workspace_repository_mismatch`);
- bare repository (`bare_repository`);
- arbitrary or malformed gitfile/common-dir (`gitfile`);
- object alternates, non-empty `objects/info/alternates`, or escaped object dirs
  (`object_alternates`);
- partial clone/promisor config (`partial_clone`);
- sparse checkout/index (`sparse_checkout`/`sparse_index`);
- config include/includeIf keys or any `core.worktree` key
  (`config_authority`);
- unsupported object format or repository extension (`repository_format`); or
- unsafe owner, write permissions, or symlinked metadata (`metadata_authority`).

Use `git config --file <validated-local-config> --no-includes -z --list` only to
parse the admitted files, reject include keys, fingerprint them, then run other
plumbing. Validate common and worktree config separately. This avoids reading a
hostile external include before rejecting it. Same-user concurrent config
replacement is covered by pre/post identity/digest fences to the observable
extent described above.

Replace refs are ignored with `GIT_NO_REPLACE_OBJECTS=1`. Nested repositories
inside an outer worktree are not recursed; an untracked nested repository is a
single `nested_repository` unresolved entry. A gitlink/submodule is always an
unresolved `unsupported_kind` with unknown changedness; index OIDs do not create
an exception to HEAD-to-live semantics.

## 5. Bounded semantic line diff

### 5.1 Evaluated algorithms

- **Trace-space Myers:** good minimal edit scripts, but worst-case trace memory
  grows with edit distance; the prior 1,500-line probe reached about 85 MiB.
- **Linear-space Myers:** attractive `O((N+M)D)` time and linear memory, but the
  middle-snake implementation and deterministic tie handling are materially
  harder to prove. It remains a possible later replacement behind an algorithm
  version.
- **Patience + exact fallback:** unique common lines form deterministic LIS
  anchors. The prototype uses linear-space Hirschberg LCS within gaps. It is
  simple to reconstruct/test and gives good source diffs. Anchors may sacrifice
  global minimality, which is acceptable and explicit.
- **Histogram:** useful on repeated source, but adds frequency heuristics and
  version-sensitive tie behavior without improving R1 safety. Rejected.

Own the small implementation. Do not fork `pkg/diff`: quadratic trace storage
and cancellation-as-empty are incompatible. The prototype is evidence, not
production code; implementation should preserve its property suite and add
hunk goldens.

### 5.2 Exact limits and fallback

Per file side: 1 MiB, 5,000 logical lines, 64 KiB/line. Algorithm budget:
4,000,000 line comparisons, 16 MiB scratch, 20,000 edit records, 2,000 hunks,
and cancellation checks at least every 1,024 comparisons and recursion edge.
Checked arithmetic precedes allocation. The prototype conservatively admits
unique-line maps and verifies its estimate against measured allocations. The
production implementation must replace runtime maps with sorted preallocated
occurrence records so the 16 MiB scratch ceiling is structural rather than an
allocator estimate; input and output buffers are separately covered by the
1 MiB-side and 20,000-record limits.

On budget exhaustion return a successful file result with
`computation.state=too_complex`, its closed reason, and no partial edit script.
Never present a prefix as the whole diff. All-added
and all-deleted regular files bypass LCS. Hunk projection uses three context
lines and caps a response to 1,000 lines, 20 hunk fragments, and 512 KiB encoded.
Continuation may split a large hunk only at a line boundary and marks
`continuedBefore/continuedAfter`; coordinates remain those of the complete hunk.

Local benchmark (Apple M3 Pro, Go 1.27.1, three iterations):

```text
5,000 lines, one changed unique line: 0.84 ms/op, 2.33 MiB, 3,375 allocs
5,000 disjoint lines, 2M budget:     6.29 ms/op, 1.49 MiB, 6,591 allocs
```

The disjoint case terminates `ErrTooComplex`. A three-second fuzz run executed
about 1.65 million reconstruction cases. These are research measurements, not a
service SLO.

## 6. Proposed contract

Names below are wire-shape proposals; protocol types own exact validation.

```go
type DiffTarget struct {
    ID, WorkspaceID string
    Kind string // "working_tree" only in R1
    RepositoryPath string // always "" in R1
}

type DiffObservation struct {
    SessionID string
    Target DiffTarget
    Revision string
    Head DiffHead // commit{oid} | unborn
    IndexSummary string // clean | diverged | conflicted
    Complete bool
    Truncation *DiffTruncation // candidate_limit | byte_limit |
                               // observation_limit | deadline
    Omissions []DiffOmission // unsupported_path | unexamined_candidate,
                             // each with an exact observed count
}

type DiffFileSummary struct {
    Path string
    FileRevision *string // absent unless exact old/live evidence exists
    Change string // added | deleted | modified | mode_changed | unknown
    Old, New DiffSide
    ContentState string // text | binary | conflict | intent_to_add |
                        // unsupported_transform | unsupported_kind |
                        // too_large | unavailable
    Reason string // closed enum, present for non-text states
    Additions, Deletions *int // optional; never guessed
}

type DiffComputation struct {
    State string // complete | too_complex
    Reason string // empty unless too_complex: diff_work | diff_memory |
                  // edit_limit | hunk_limit
}

type DiffSide struct {
    Kind string // absent | regular | symlink | submodule | other
    Mode uint32
}

type DiffHunk struct {
    OldStart, OldCount, NewStart, NewCount int
    ContinuedBefore, ContinuedAfter bool
    Lines []DiffLine
}
type DiffLine struct {
    Kind string // context | deletion | addition
    OldLine, NewLine *int
    Content string
    HasTerminatingLF bool
}
```

Operations:

```text
observeWorkingTree(workspaceId, pageSize?, cursor?)
  -> {observation, files, nextCursor?}

readFileDiff(targetId, targetRevision, path, expectedFileRevision?,
             pageSize?, cursor?)
  -> {observation, file, computation, hunks, nextCursor?}
```

First-page observation materializes a new snapshot. A list cursor selects the
retained snapshot and ignores a request to create a newer one. `readFileDiff`
requires target revision; expected file revision is optional but, when present,
must match. Ordering is ascending canonical UTF-8 path bytes. Renames/copies are
not paired in R1: they are deletion/addition.

Default/max list page is 100/200 records. Diff page default/max is 500/1,000
lines and 10/20 hunk fragments. Responses are at most 512 KiB. Target admission
limits are 4,000 candidates, 32 MiB aggregate admitted live/blob bytes, and the
per-file/algorithm limits above. Scan overflow returns an explicit incomplete
observation; it does not silently omit files. A Git path that fails ADR 0019's
UTF-8 renderer-safe canonical syntax is not lossy-encoded: it increments an
`unsupported_path` omission count and makes the observation incomplete.

Coordinates are one-based. A hunk start is zero only when that side is empty,
for example old `(0,0)` for an all-added file. Context has both line numbers,
deletion only old, addition only new. `Content` excludes LF and preserves a
preceding CR; `HasTerminatingLF` records LF. Paths identify the one R1 file;
old/new path fields are intentionally absent until rename identity is designed.

`observation.complete` describes target coverage; `computation.state` describes
whether a complete edit script exists; `nextCursor` alone describes page
continuation. These states are intentionally separate.

Expected content states are records, not transport errors. Stable errors are:

```text
invalid_path, not_repository, unsupported_repository,
stale_workspace, stale_target, stale_file, stale_cursor,
not_found, permission_denied, limit_exceeded, capacity_exceeded,
repository_unavailable, unavailable
```

Cancellation remains cancellation. Safe details are closed enums only:
`unsupported_repository.reason`, `limit_exceeded.limit`, capacity scope, and a
current target/file revision when completely and safely observed. Never expose
absolute paths, Git stderr, OIDs from hostile records, filter names, or config
values as error text.

### Annotation seam

A future ADR 0022 anchor variant should be exactly revision-pinned:

```text
{ kind: "diff_range", targetId, targetRevision, path, fileRevision,
  side: "old" | "new", startLine, endLine }
```

The server resolves the retained revision, verifies the line range on the named
side, and freezes the preview. A context line may be anchored on either explicit
side; additions only new and deletions only old. Stale anchors never relocate.
The range remains at most 200 lines. Adding this variant is not part of this
research change.

## Verification still required

Before production implementation is accepted, run the scripts/tests plus
integration fixtures on macOS and Linux, arm64 and amd64, with the oldest and
newest supported Git. Add SHA-1/SHA-256 object formats; case-sensitive and
case-insensitive filesystems; filemode true/false; linked worktrees on separate
volumes; permissions/ownership; split index; malformed plumbing; process-tree
cancellation; genuinely promised missing objects with an unguarded helper
positive control and a `GIT_NO_LAZY_FETCH` negative control; concurrent mutation
at C0/C1/F/C2; and response/cursor eviction tests. Attribute conversion support
must not expand without byte-for-byte checkout comparison on every supported
Git/platform matrix.

## Independent review

The `code-reviewer` subagent independently reviewed the report, ADR, probes, and
prototype. Its sound findings are incorporated: reject/override `core.worktree`;
never mint exact file revisions or changedness for unread oversized content;
make gitlinks unresolved rather than index-backed; fix assume-unchanged probing
to use `-v`; disable rename/config variability in ITA detection; separate
omissions, computation, and pagination on the wire; isolate probe Git config;
define executable-mode semantics; and strengthen prototype accounting,
cancellation, and checked arithmetic. The cross-platform verification matrix was completed before ADR 0023 was
accepted and `CORE-DIFF-001` was marked complete.
