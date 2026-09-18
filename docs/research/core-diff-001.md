# CORE-DIFF-001 implementation research

Date: 2026-09-17

## Decision

Kit should **not use one Go library end to end**. Use Git CLI plumbing only for
repository-format authority (repository discovery, refs, trees, index entries,
and ignore-aware untracked discovery), then read the live filesystem through
Kit's secure workspace I/O and generate semantic line edits in Go under hard
budgets. Do not ask Git to diff working-tree content.

This split is important for both correctness and security:

- Git understands worktrees, linked worktrees, object formats, refs, packed
  objects, index extensions, ignore rules, and unborn repositories better than
  a second implementation.
- Direct reads preserve what the user actually sees, avoid clean/smudge filters,
  and permit the same containment and stale-read checks as `CORE-WORK-001`.
- A semantic Go edit script gives Kit explicit old/new coordinates without
  making the wire contract or renderer depend on unified-patch syntax.

A server-owned, bounded parser for Git-generated patches is a viable fallback,
but is not the preferred content path. `git diff HEAD`, even with
`--no-ext-diff --no-textconv`, can execute a configured **clean filter** while
comparing a worktree file. A hostile-repository probe demonstrated this. There
is no generic `--no-filter` switch and repository `.gitattributes` cannot be
turned off by `core.attributesFile=/dev/null`.

## Proposed architecture

Keep metadata/status discovery, content acquisition, and content diffing as
three explicit stages.

### 1. Snapshot repository metadata with non-content Git plumbing

Run Git directly with `exec.CommandContext`; never use a shell. Use a sanitized
environment (`GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0`,
`GIT_NO_LAZY_FETCH=1`, `GIT_NO_REPLACE_OBJECTS=1`, `GIT_CONFIG_NOSYSTEM=1`,
`GIT_CONFIG_GLOBAL=/dev/null`, a private empty `HOME` and `XDG_CONFIG_HOME`, no
inherited `GIT_*` variables except an allowlist), a fixed locale, no pager, an
explicit working directory, and the standard global option
`-c core.fsmonitor=false` on **every** repository-aware invocation. The override
is mandatory because even `ls-files` and `check-attr` can execute a
repository-configured fsmonitor hook while reading the index.
`GIT_NO_LAZY_FETCH=1` is also mandatory: otherwise a partial clone's missing
object can invoke configured remotes and credential helpers. Missing promised
objects become a typed unavailable/skipped result. Put global Git options before
the subcommand.

Recommended operations:

```text
# Every line also includes: -c core.fsmonitor=false
git --no-pager --literal-pathspecs rev-parse --show-toplevel --git-dir
git --no-pager rev-parse --verify HEAD^{commit}           # save exact OID; unborn if absent
git --no-pager ls-tree -r -z --full-tree <exact-HEAD-OID> # old mode/OID/path
git --no-pager ls-files --stage -t -z                     # index stages + skip flags
git --no-pager -c core.excludesFile=/dev/null \
    ls-files --others --exclude-standard -z               # eligible untracked
git --no-pager cat-file --batch                           # OIDs supplied by Kit
git --no-pager -c core.attributesFile=/dev/null \
    check-attr -z --all --stdin                            # bounded path input
```

The two `ls-files` streams are separate and must not be mistaken for a combined
status stream. `core.excludesFile=/dev/null` prevents repository config from
naming an external global-ignore file; in-tree `.gitignore` and
`.git/info/exclude` still apply. Likewise, the attribute command should see
repository attributes but not a configured external attribute file. Parse
NUL-delimited records, validate every path at the boundary, sort by canonical
path bytes, reject unexpected record shapes, and place byte/record/deadline
bounds around stdout and stderr. Feed `cat-file` only validated OIDs obtained
from Git, parse its header, enforce the declared size before reading, and verify
the exact byte count.

The candidate path set is the union of HEAD-tree paths, index paths (needed for
staged additions and intent-to-add), and non-ignored untracked paths. Compare
HEAD blobs to securely opened current filesystem entries; the index is metadata,
not the diff's intermediate side. This naturally makes a file changed in both
index and worktree one HEAD-to-current-worktree change and avoids double-counting.
A staged change subsequently reverted in the worktree produces no content
change.

This is not an atomic filesystem snapshot. Resolve HEAD once and use that exact
OID. Hash the raw, bounded index/untracked/attribute observations and repeat
HEAD plus those observations after content acquisition; discard and retry or
return `stale_target` if they differ. Revalidate admitted file descriptors as
ADR 0019 does. The retained observation manifest—not merely the list of
returned changed files—must let a guarded follow-up detect path-set changes.

Parse index stages explicitly. An unmerged path (stages 1–3), intent-to-add, or
unknown extension gets a typed conflict/unsupported classification rather than
an invented side. Detect skip-worktree and sparse-index state. R1 may reject a
sparse checkout as `unsupported_repository`; it must never report intentionally
unmaterialized files as deletions. Supporting it later requires treating an
absent skip-worktree path as index content and expanding sparse-directory entries
under the same bounds.

Do not use `git status`, `git diff`, `git diff-files`, `git hash-object` without
`--no-filters`, or any operation that converts worktree content. `git status`
may also execute a clean filter when it must refresh/hash a racily-clean file.
Do not use hooks, external diff drivers, textconv, shell aliases, or submodule
recursion. Set `GIT_NO_REPLACE_OBJECTS=1` unless replace refs are deliberately
part of the future contract.

Repository discovery is itself an authority boundary. Do not silently expand
filesystem authority from the session workspace to an ancestor worktree, a
hostile `.git` file, or an arbitrary object alternate. The recommended R1
policy is: the worktree root must equal the workspace root or be its descendant;
the gitdir may be the in-tree `.git` directory or a validated linked-worktree
gitdir whose common directory is owned by the same user and explicitly admitted
as repository metadata authority; object alternates are rejected. Anything
else fails closed as `unsupported_repository`. This preserves intentional linked
worktrees without treating an arbitrary ancestor or gitfile target as content
read authority.

### 2. Acquire and classify live content in Kit

Reuse the descriptor-anchored containment model from ADR 0019. Open/lstat each
candidate without following intermediate links; snapshot identity, mode, size,
mtime and ctime; read/hash under per-file and aggregate byte/time budgets; then
revalidate metadata before publishing. Cancellation must close descriptors and
terminate any Git child process.

Suggested initial semantics:

- regular UTF-8 without NUL: text;
- NUL or malformed UTF-8 in the bounded inspection: binary;
- executable `filter` or unsupported `working-tree-encoding` attributes:
  `attribute_transform_unsupported`; never execute them;
- safe built-in `text`/`eol`/`ident` transformations: reproduce Git's smudged
  old-side representation in Go from the blob, effective attributes, and the
  allowlisted `core.autocrlf`/`core.eol` values, then compare it with literal
  worktree bytes; if Kit cannot implement a case exactly, classify it unsupported
  rather than comparing a raw clean blob to transformed bytes;
- tracked deletion: empty new side;
- eligible untracked regular file: empty old side (all-added hunk; no algorithm
  is required);
- executable-bit-only change: metadata-only changed file;
- symlink: compare link-target bytes without following it, but expose a typed
  `symlink`/unsupported-content classification rather than text hunks initially;
- gitlink (`160000`): expose `submodule`/unsupported-content; compare recorded
  HEAD and index OIDs, but do not recurse or claim to detect arbitrary dirty
  nested worktrees;
- special file, oversized file, excessive path count, byte budget, or deadline:
  typed skipped/truncated reason, never an unbounded read.

Ignored files are absent because only `ls-files --others --exclude-standard`
adds untracked paths. Tracked files remain included even if a later ignore rule
matches. An unborn repository has no HEAD side: index paths and eligible
untracked regular files are additions. Repository `.gitattributes` may still
classify binary/text or customize diff presentation, but Kit's own UTF-8/NUL
policy is authoritative; content-transforming attributes are the exception and
must be implemented exactly or reported unsupported. Read only the allowlisted
scalar conversion config, reject unexpected origins/values, and include the
effective attribute/config policy in the observation revision. This avoids both
command execution and false “changes” caused by comparing a clean LF blob with
an expected CRLF/smudged worktree representation.

Exact renames can optionally be paired deterministically by old/new content
digest after acquisition. Ambiguous matches use path-byte tie breakers. Do not
run heuristic rename/copy detection in R1: it is potentially quadratic and
configuration/version dependent. Unpaired renames are semantically correct as
one deletion plus one addition. Copies likewise remain additions unless the
contract later requires a bounded explicit detector.

### 3. Produce semantic hunks in Go

Represent an internal edit script as equal/delete/insert ranges with half-open
old/new line indexes. Build context hunks directly from those ranges and project
for every returned line:

```text
kind = context | deletion | addition
oldLine?; newLine?; content; hasTerminatingLF
```

Hunks carry explicit one-based starts and counts for both sides. Preserve CR and
missing-final-newline state; do not derive coordinates later from prefixed
presentation lines.

The Go standard library has no line-diff primitive. A small owned implementation
is preferable to the surveyed generators. Use a linear-space Myers or
patience/Myers implementation with explicit maximum input
lines, maximum edit distance/work units, maximum produced ranges/hunks/lines,
periodic `context.Context` checks, checked integer arithmetic, and a typed
`diff_too_complex`/truncated result when a budget is exhausted. Fuzz the edit
script invariants and reconstruction, not just rendered patches. An all-added
untracked file and an all-deleted file can bypass the algorithm.

`github.com/pkg/diff/myers` has the best semantic API of the surveyed small
packages and accepts a context, but its own documentation and implementation
use quadratic trace space. Treat it as a useful reference, not a safe default.
A local 1,500-line/no-commonality probe peaked at about 85 MB RSS; cancellation
returned promptly, but cancellation is reported as an empty script rather than
an error, which is unsafe to interpret as “no changes.” If adopted, Kit would
need a wrapper that prevents entry above a conservative complexity bound and
separately checks `ctx.Err()`; owning a bounded implementation is clearer.

### Revisions and stale guards

Use opaque versioned SHA-256 revisions, not Git's worktree `index` line or an
abbreviated OID.

- File observation: target kind + canonical path + old mode/OID (or absent) +
  new kind/mode + SHA-256 of the exact admitted current bytes (or an explicit
  skipped-classification token) + observation policy version.
- Target observation: HEAD commit/absent marker + sorted file-observation
  records + truncation/omission records + policy/limit version.

Retain an internal manifest for the lifetime of the bounded result. A guarded
read/hunk request must match the target revision and revalidate the current
workspace incarnation, exact HEAD, index/untracked/attribute observation hashes,
and relevant file identity/content revision before success. Content digests
make returned evidence reliable; metadata-only revisions for skipped
large/special files must not be represented as content fingerprints. If the
aggregate scan cannot establish a complete view, return an explicit
incomplete/truncated observation rather than a “complete” revision.

Errors should map cancellation unchanged and otherwise expose only stable typed
codes such as `not_repository`, `unborn` (state, not necessarily error),
`stale_workspace`, `stale_target`, `binary`, `unsupported_kind`,
`diff_too_complex`, `limit_exceeded`, `capacity_exceeded`, and sanitized
`repository_unavailable`. Never return Git stderr, config values, absolute
paths, filter names, or object-parser errors verbatim.

## Contract implications and open decisions

This research recommends implementation primitives; it does not accept the
public contract. A subsequent CORE-DIFF-001 design must pin at least:

- a working-tree target identity scoped to `(session, workspace incarnation,
  validated repository root)` and an opaque target revision;
- `listChangedFiles(target, expectedRevision?, pageSize, cursor?)` returning
  canonical repository-relative paths, old/new kinds and modes, addition/
  deletion counts when known, file revisions, binary/unsupported/skipped state,
  and explicit target/file-list truncation;
- `readFileDiff(target, targetRevision, path, expectedFileRevision?, cursor?)`
  returning renderer-neutral hunks/lines and explicit partial-result state;
- stable pagination observations with expiry and stale-cursor behavior rather
  than rerunning Git for each page;
- one-based hunk coordinates with zero permitted only for an empty side (for
  example an all-added hunk starts old `0,count=0`, new `1,count=n`), while line
  records use nullable old/new line numbers;
- exact omission rules for non-UTF-8/unsafe Git paths and exact distinctions
  among incomplete target scan, skipped file, truncated hunk page, complexity
  exhaustion, stale data, and fatal repository error; and
- fixed negotiated ceilings. Reasonable R1 starting points, to validate with
  benchmarks, are 4,000 candidate paths, 1 MiB/5,000 lines per text side, 32 MiB
  aggregate bytes per observation, 512 KiB encoded responses, 200 records per
  page, and explicit edit-work/hunk/returned-line limits.

Canonical path ownership needs an explicit decision when repository root and
workspace root differ; under the recommended authority policy, return
repository-relative paths plus a validated mapping to workspace-relative paths,
never absolute paths. Initial CORE-DIFF-001 is working-tree-only. ADR 0020's
commit/branch identities belong to later review-target work and should reuse the
semantic records without silently expanding this milestone.

## Comparison matrix

| Candidate | Combined HEAD-to-worktree capability | Semantic output and bounds | Portability / footprint / health | Assessment |
| --- | --- | --- | --- | --- |
| Git CLI porcelain/plumbing + Kit parser/algorithm | Git diff/status understands staged+unstaged, modes, symlinks, gitlinks and ignores; untracked still needs separate enumeration. A validated tree/index/others union models non-sparse HEAD-to-live-WT after coherence checks; sparse/conflicted states need explicit handling. | Plumbing is NUL framed. Patch output is parseable but presentation-oriented and must be externally bounded. `CommandContext` provides kill-based cancellation. Git algorithms are mature. | Requires a compatible Git executable already expected by a coding agent; behavior matrix must pin a minimum version. No binary-size cost. GPL Git is a separate process, not linked. | **Recommended for metadata/objects only.** Do not diff repository worktree content because filters can execute. |
| `go-git/go-git/v5` v5.19.2 | `Worktree.Status` reports index and worktree columns and untracked files, while commit-tree diffs/patches are useful for object trees. It has no single authoritative HEAD-to-live-WT semantic patch API; local probe omitted dirty-submodule state. | Like the recommended CLI design, it still requires Kit to compose repository metadata and live content. Context support is uneven across APIs; whole-tree/status operations materialize state. | Pure Go, Apache-2.0, active release. Minimal import added about 5.0 MB to a stripped probe and brings a broad crypto/transport/filesystem dependency graph. Its weaker fit is Git-format fidelity (new index/object/worktree extensions), larger compatibility surface and footprint—not the need for composition by itself. | **Reject as authority.** Potentially useful for isolated object operations, but Git `cat-file` is smaller and tracks installed Git compatibility. |
| `sourcegraph/go-diff` v0.9.0 | Parser only; it discovers no repository state and generates no diff. | Good Git/unified parser with hunk coordinates, streaming readers and overflow checks. Hunk body still needs semantic line parsing. Reader has no intrinsic total-input/line bound; wrap it. | Pure Go, MIT, current release; approximately +0.19 MB in stripped probe. | Best lightweight **fallback parser** if Kit accepts Git patch generation; not an end-to-end solution. |
| `bluekeyes/go-gitdiff` v0.9.0 | Parser/applier only. | Richest parsed model here: rename/copy/modes/OIDs, binary markers, text fragments, operations and positions. Uses unbounded `ReadString`, so an outer byte-limited reader is mandatory; no context API. | Pure Go, MIT, current release; approximately +0.84 MB in stripped probe. | Best feature-complete patch-parser alternative, especially if binary Git patches are ever needed. Larger attack/API surface than CORE-DIFF-001 needs. |
| `sergi/go-diff` v1.4.0 | No Git metadata. Can compare two strings. | Diff-match-patch returns semantic operations and has a timeout setting, but coordinates/hunks/line endings must be built by Kit. String/rune materialization and heuristic cleanup complicate strict work/memory bounds and Git-like deterministic line diffs. | Pure Go, MIT, maintained; approximately +0.70 MB in stripped probe. Mutable options should be per-call, not shared. | Acceptable for UI text matching, **not preferred** for authoritative bounded line diffs. |
| `pkg/diff` (2024-12-24 pseudo-version) | No Git metadata. Generic sequences. | Excellent semantic `edit.Script` and `context.Context`; however Myers trace is explicitly quadratic-space and cancellation collapses to an empty script. | Pure Go, BSD-3-Clause, no tagged release, limited recent activity; approximately +0.43 MB stripped. | Useful reference/API shape only unless very tightly pre-bounded or forked. |
| `hexops/gotextdiff` v1.0.3 | No Git metadata. | Myers returns text edits, but API is string/byte-position oriented, no context/cancellation/budgets, and no direct hunk model. | Pure Go, BSD-3-Clause; last tagged release 2020 and repository push observed 2023. | Reject for this contract. |
| `aymanbagabas/go-udiff` v0.4.1 | No Git metadata. | Generates unified presentation text rather than semantic edits; no cancellation/work budget API. | Pure Go, BSD-3-Clause, active/current; approximately +0.75 MB stripped. | Good formatter, wrong abstraction. |
| `libgit2` + `git2go` | Native status/diff APIs can cover most Git cases, with callbacks for semantic deltas/hunks/lines. Untracked and HEAD composition still need policy. | Better semantic callbacks than parsing CLI patches; notification callbacks can abort, but memory/time limits still need wrappers. Attributes/config/filter behavior must be pinned and audited. | ADR 0021 proves Kit has a CGO release matrix, but authorizes native Tree-sitter specifically; libgit2 would require a new decision. It adds a large native library, allocator/native crash surface, license notices, four-platform build work, and strict git2go/libgit2 ABI coupling. `git2go` latest release observed was 2022 and its default branch push 2024. | Comparison only; **reject** unless eliminating the Git executable later becomes a product requirement. |

Size figures are incremental approximations from tiny darwin/arm64 Go 1.27.1
programs (`-trimpath -ldflags='-s -w'`) over a 1.18 MB baseline. They are useful
for relative screening, not release forecasts.

## Behavior and risk findings

- A fixture with one file staged and then edited again produced one `git diff
  HEAD` hunk against the final worktree, as required. A staged-then-reverted file
  is similarly eliminated by direct HEAD/current comparison.
- `git status --porcelain=v2 -z --no-renames` represented mode changes,
  symlinks, deletion and dirty submodule state; `git diff HEAD` omitted
  untracked files. `ls-files --others --exclude-standard -z` returned only the
  non-ignored untracked files.
- Git's patch represented a symlink as target-text lines, a mode-only change
  without hunks, and a dirty gitlink as `Subproject commit ...-dirty`. Those are
  useful observations but should not force Kit to pretend these are ordinary
  text files.
- In the same fixture, go-git reported staged/worktree columns and eligible
  untracked paths but did not report the dirty submodule.
- `--no-ext-diff --no-textconv` prevented configured external diff and textconv
  commands in a probe. It did **not** prevent a `.gitattributes` clean filter
  from executing during `git diff HEAD`. A separate clean-file probe showed
  `git status` could execute the filter after a metadata change forced content
  checking.
- A configured `core.fsmonitor` executable ran during `git ls-files --stage`.
  The standard `-c core.fsmonitor=false` override blocked it; nominally
  read-only plumbing is not process-safe without this override.
- `git diff --no-index` run from inside the hostile repository also executed the
  filter. Running it from a neutral non-repository cwd with sanitized config did
  not. This can be retained as a fallback/verification oracle using
  server-created, controlled-name snapshots, but incurs temp-file and patch
  parsing complexity.
- A 32 MiB `git diff --no-index` probe under `exec.CommandContext` was killed in
  about 6 ms after a 5 ms deadline. Production code must additionally cap pipe
  bytes and continue draining/terminate correctly to avoid child-process
  deadlock.
- Pure-Go parsers and algorithms are race-friendly when instances are not
  mutated concurrently, but none supplies Kit's queue, aggregate memory, or
  response bounds. Apply bounds before calling them and fuzz malformed/truncated
  records. Bluekeyes and Sourcegraph intentionally accept long lines, so a
  limited reader is non-optional.

This recommendation is **directionally sound, not implementation-ready** until
four acceptance gates are resolved and tested: coherent observation/stale
semantics, sparse/conflicted-index behavior, safe attribute/EOL transformation,
and repository/gitdir/object-store authority under hostile configuration. The
bounded edit algorithm is another material risk. Mitigate it with
reconstruction/property tests, differential tests against sanitized `git diff
--no-index`, adversarial repeated-line and no-commonality inputs, fuzzing, race
tests, and fixed golden semantics. Finally, the contract must expose
scan/file/byte truncation and must not mint a complete target revision after a
bounded scan stops.

## Deterministic verification matrix

Run fixtures on all four release targets and the oldest/newest supported Git:

1. clean, detached HEAD, validated linked worktree, bare/non-repository, unborn,
   cone/non-cone sparse checkout and sparse-index repository;
2. staged+unstaged same file, staged add/delete followed by live reversal,
   intent-to-add and every unmerged index stage;
3. non-ignored and ignored untracked regular files;
4. exact rename represented as delete/add and deterministic optional pairing;
5. NUL, malformed UTF-8, CRLF, empty, and missing-final-LF files;
6. symlink replacement, executable-bit mode-only change, gitlink OID change and
   explicit unsupported classification without claiming nested dirty-state
   detection;
7. malicious `.gitattributes`, clean/smudge filter, EOL/encoding conversion,
   textconv, external diff, pager, aliases, fsmonitor/other hooks, config
   includes, object
   alternates/replace refs, hostile gitfiles and filenames;
8. huge files, many files, disjoint/repeated lines, output limits, malformed or
   incomplete plumbing records, missing promisor objects with lazy fetch
   disabled, Git early exit, cancellation, and concurrent mutation of HEAD,
   index, ignores, attributes and files before/during/after reads;
9. stable ordering/revisions and stale-target rejection after content, mode,
   path, HEAD, index, cwd-incarnation, and ignore-policy changes.

Compare semantics, not Git patch formatting. Version-specific Git diagnostics
must never appear in protocol fixtures.

## Evidence

Local throwaway probes were run outside the worktree under
`/tmp/core-diff-probes` and `/tmp/core-diff-fixture` using Git 2.54.0,
Go 1.27.1, and macOS arm64. They exercised staged+unstaged content, ignored and
untracked files, rename-as-delete/add, binary, symlink, mode, dirty submodule,
unborn state, malicious drivers/filters, large disjoint inputs, binary-size
probes, and cancellation. Probe code and fixtures are intentionally not part of
the repository.

Primary references (accessed 2026-09-17):

- Git `status`, `ls-files`, `ls-tree`, `cat-file`, `diff`, and attributes:
  <https://git-scm.com/docs/git-status>, <https://git-scm.com/docs/git-ls-files>,
  <https://git-scm.com/docs/git-ls-tree>, <https://git-scm.com/docs/git-cat-file>,
  <https://git-scm.com/docs/git-diff>, <https://git-scm.com/docs/gitattributes>
- go-git: <https://github.com/go-git/go-git>
- Sourcegraph parser: <https://github.com/sourcegraph/go-diff>
- Bluekeyes parser: <https://github.com/bluekeyes/go-gitdiff>
- diff-match-patch port: <https://github.com/sergi/go-diff>
- semantic Myers implementation: <https://github.com/pkg/diff>
- gotextdiff and go-udiff: <https://github.com/hexops/gotextdiff>,
  <https://github.com/aymanbagabas/go-udiff>
- libgit2/git2go: <https://github.com/libgit2/libgit2>,
  <https://github.com/libgit2/git2go>

## Independent review

The `code-reviewer` subagent independently challenged the initial report. Its
sound findings covered sparse-index deletions, cross-command snapshot races,
partial-clone lazy fetch, attribute/EOL semantics, index conflicts, repository
metadata authority, submodule overclaims, contract gaps, go-git comparison
fairness, CGO decision scope, probe reproducibility, and repository-configured
fsmonitor execution from nominally read-only plumbing. The report now makes
those cases explicit, adds acceptance gates and a proposed contract shape, and
retains the central probe at `docs/research/core-diff-001-probes.sh`. The review's
bottom line—directionally sound but not implementation-ready until the named
gates are designed and tested—is adopted here.

This report informs `CORE-DIFF-001`; it does not change ADR 0019, ADR 0020,
ADR 0021, or backlog status.
