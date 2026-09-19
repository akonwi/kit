# 0019: Expose session workspace files through bounded contracts

## Status

Accepted

## Context

Directory explorers and file viewers need to work for the native TUI and for a
browser or attached client whose machine is not the session host. A client
therefore cannot discover or read the workspace through its own filesystem.
It also cannot safely turn a presentation path into a server-local absolute
path.

A session's working directory is mutable. Relative coding tools snapshot that
scope when their execution begins, while `/cd` and `change_cwd` can publish a
new scope concurrently with client requests. Workspace files can also change
without a Kit command because tools, editors, build processes, and other
clients share the host filesystem.

Directory trees and files are untrusted, potentially very large, and subject to
symlink races, unreadable entries, non-text bytes, and replacement during a
read. Diff and review workflows additionally need historical and Git-aware
identities, but making ordinary file exploration depend on Git or a review
model would couple otherwise independent capabilities.

## Decision

### Ownership and client boundary

The Kit server owns workspace resolution, visibility policy, filesystem I/O,
resource limits, and projection into wire records. Workspace operations are
bound-session operations. Authorization to a session is necessary but does not
relax workspace containment.

The shared session client exposes an optional renderer-neutral `workspaceFiles`
facet with immutable negotiated `WorkspaceLimits` and operations equivalent to:

```text
workspace() -> WorkspaceRef
listDirectory(workspaceId, path, pageSize, cursor?) -> DirectoryPage
readFile(workspaceId, path, expectedFileRevision?) -> FileRead
```

`WorkspaceLimits` reports the client-relevant page, response, path, preview, and
admission bounds. Clients use those limits to avoid work the server cannot
accept; daemon-wide scheduling limits remain private implementation policy.

The local TUI uses that facet through the authenticated daemon path, just as an
attached TUI or semantic web client does. Renderers do not import server,
session-runtime, storage, coding-tool, or filesystem implementations. They do
not use absolute host paths to open workspace content.

The session manager remains authoritative for the published cwd. A server-owned
workspace-files service snapshots that state and performs host filesystem
operations through a narrow port. The protocol package owns wire-safe records
and validation. The session-client
package projects those records into client-owned values. Renderer models add
only presentation state such as expansion, selection, filtering, scroll
position, and loading state.

### Workspace identity and mutable cwd

`WorkspaceRef` contains:

- the bound `sessionId`;
- the session's clean absolute `cwd` for display only;
- a deterministic opaque `workspaceId` of at most 128 bytes; and
- `state`, either `ready` or `unavailable`.

The server derives the ID as a versioned SHA-256 hash of the canonical encoded
`sessionId` and the session's published clean absolute cwd, and encodes it with
a canonical prefix and unpadded base64url. Hash input is length-delimited rather
than joined ambiguously. Clients neither calculate nor parse the ID. It is an
identity and routing guard, not an authorization token or a claim that the
workspace contents are unchanged.

Changing cwd to a different canonical path changes the workspace ID. Returning
to an earlier cwd restores the same ID, intentionally identifying the same
logical mutable workspace for that session. Different sessions at the same cwd
have different IDs. Replacing the directory or retargeting a symlink at the
same published cwd does not change the ID; containment and root validation are
performed afresh by every operation, while directory and file revisions
identify content observations.

`workspace()` always returns a reference for an existing session. If resolving
the published cwd finds it missing, non-directory, or inaccessible, the
reference is `unavailable`; list and read operations return `not_found`,
`not_directory`, or `permission_denied`, respectively. Recovery at the same cwd
restores `ready` with the same workspace ID. Snapshots and cwd events do not
include host error text or physical root details.

Session snapshots, cwd mutation results, and cwd-change events project the
current `WorkspaceRef`. Every list and read request names the expected workspace
ID. The server snapshots the published cwd and securely opens its authority root
at admission, performs the operation through that root, and checks before
success that the session's current workspace ID still matches. A mismatch
produces `stale_workspace`, not data silently relabeled as belonging to another
cwd. Every successful response repeats the `WorkspaceRef`.

Returning from workspace A to B and back to A deliberately creates no new
workspace identity. Directory and file revisions describe the age of returned
observations, and renderers use a local request generation to discard an older
response superseded by a newer request.

Workspace reads do not hold the cwd mutation lock while doing filesystem I/O.
They may run concurrently with agent tools and other clients. A coding tool or
shell command already admitted against an older cwd keeps its own snapshot;
this contract does not alter that rule. There is no process-global cwd or
active-workspace state.

External filesystem mutations do not create session events in R1. Clients
refresh explicitly or on a bounded renderer-owned schedule. Filesystem watchers
may later be an invalidation optimization, never the source of truth.

### Canonical workspace paths

All requested and returned workspace paths use one protocol representation:

- UTF-8, renderer-safe text of at most 4,096 encoded bytes;
- `/` separators on every host;
- relative to the `WorkspaceRef`, never absolute;
- no empty segment, `.` or `..` segment, repeated separator, trailing
  separator, backslash, NUL, control character, or directional formatting
  character; and
- no Unicode normalization or case folding.

The empty string denotes only the workspace root in a directory-list request.
It never denotes a file. For every component, the server requires an exact byte
match with the name returned by the containing directory; a case-folded or
Unicode-normalized alias is `invalid_path`, even when the host filesystem would
resolve it. Resolution accepts at most 64 components and examines at most
20,000 directory names across all exact-name checks in one request. Exceeding
either bound produces `limit_exceeded` rather than accepting an alias. Every
non-root response path is the filesystem-observed canonical spelling joined
with an entry name under these rules. Clients treat paths as opaque identities
and never join them to `cwd` for I/O. This gives one protocol identity to a host
entry on case-insensitive and normalization-insensitive filesystems.

Directory entries with names that cannot be represented safely are omitted and
the page set reports a bounded `unsupported_name` omission. Kit does not invent
a lossy display name that could alias another entry.

### Containment and symlinks

Containment is enforced by descriptor-based or equivalently race-safe host
filesystem operations, not by lexical prefix checks alone.

- Intermediate symlink components observed during resolution are rejected by
  directory listing and file read requests. Every operation remains anchored to
  the opened root if a component changes concurrently, so a race cannot escape
  the workspace.
- A listed symlink is projected as kind `symlink`; listing never descends into
  it and does not expose its target text.
- A final relative symlink may be read as a file only when its resolved target
  is a regular file inside the same workspace root. Absolute symlinks are
  rejected even when they happen to name an inward target. The opened
  descriptor, resolved containment, and file identity are revalidated before
  success.
- A final directory symlink cannot be listed, even when it points inward.
- Broken links produce `not_found`. A link or race that would leave the root
  produces `outside_workspace`. An intermediate symlink produces
  `symlink_traversal`.
- Sockets, devices, FIFOs, and other non-regular objects are listed as `other`
  but are never read as files.

If the platform cannot prove containment and descriptor identity for an
operation, the operation fails closed. Error details never disclose the target
of a symlink or any path outside the workspace.

### Directory listing contract

`DirectoryPage` is a flat page of immediate children, not a recursive tree. It
contains:

```text
sessionId
workspace: WorkspaceRef
directory: { path, revision }
entries: [{ path, name, kind, size?, modifiedAt?, fileRevision? }]
nextCursor?
truncated
truncationReason?       # entry_limit | observation_limit
omissions: [{ reason, count }]
```

`kind` is one of `file`, `directory`, `symlink`, or `other`. Optional metadata
is omitted when it cannot be obtained safely; clients must not infer
readability from its presence. A regular file's `fileRevision`, when present,
uses the same revision vocabulary as `readFile` and is only a hint for guarded
opening. Failure to inspect the requested directory itself is an error rather
than an empty page.

The server materializes one bounded, name-sorted observation of the directory
for the first page. Ordering is ascending by the canonical UTF-8 bytes of
`name`, with `path` as the deterministic tie-breaker. The directory revision
identifies that observation, including the visibility-policy version. An opaque
cursor of at most 512 bytes binds the session, workspace ID, directory
path and revision, ordering, bounds, and next position. Later pages come from
that observation. Cursors are not durable. The server keeps directory observations in a bounded,
expiring cache with aggregate per-session limits of 20,000 entries and 16 MiB,
daemon-wide limits of 80,000 entries and 64 MiB, and fair per-session admission.
Single-page observations are not cached. The concrete client-relevant bounds are reported by
`WorkspaceLimits`; cache eviction and daemon-wide scheduling remain server
policy. An unknown, expired, evicted, mismatched, or tampered cursor produces
`stale_cursor`. The client restarts pagination from the first page after that
error.

R1 defaults and hard ceilings are:

- 100 requested entries per page by default and 200 maximum;
- 10,000 examined or retained immediate children and 8 MiB of encoded entry
  metadata per directory observation;
- 512 KiB maximum encoded directory response, with a page shortened and a
  cursor issued when needed to remain under that bound; and
- at most one additional entry examined to prove entry-limit truncation.

`nextCursor` means another page exists within the materialized observation.
`truncated` means entries were omitted from the observation because the hard
entry or encoded-observation limit was reached; following cursors cannot recover those omitted
entries. Omissions caused by the visibility policy are not truncation, but
unsupported names and metadata races are reported in bounded omission
categories rather than per-entry error strings. `omissions` has at most three
records in that canonical order, one per reason: `unsupported_name`,
`entry_raced`, and `metadata_unavailable`. Each required count is the exact non-negative count
observed before the scan bound; no paths or host errors are included.

The `explore-v1` visibility policy exposes hidden, Git-ignored, and other
ordinary workspace files. It omits children named `.git` or `node_modules` to
avoid exposing implementation metadata and unbounded dependency trees through
normal exploration. It does not interpret project-specific ignore files. A
canonical path may be the target of
`listDirectory` or `readFile` even when that target was omitted from its
parent's page; the policy is applied only to children returned by the requested
listing. The shared project-path index used by composer mentions and flat file-picker
navigation remains a different bounded projection and applies hierarchical
`.gitignore` rules only; it does not interpret project-specific Kit ignore
files. Its session result reports explicit `truncated` state. The 4,000-entry
bound is proven by scanning at most one additional indexable entry, so a result
with exactly 4,000 entries is not labeled truncated unless another entry was
observed.

### File-read contract

`readFile` returns one bounded current-text preview:

```text
sessionId
workspace: WorkspaceRef
path
revision
size
modifiedAt?
encoding: "utf-8"
content
returnedBytes
returnedLines
truncated
truncationReason?       # byte_limit | line_limit
```

The server opens a regular-file descriptor, derives an opaque revision of at
most 128 bytes from the observed host file identity and high-resolution
metadata, including ctime on supported macOS and Linux hosts, reads from that
descriptor, and rechecks identity and metadata before
success. The revision is comparable only for equality, scoped to the workspace
ID, and not a durable content digest. It changes whenever Kit can observe file
replacement or content-affecting metadata change. Clients do not parse it or
reuse it with another workspace ID.

A caller may provide `expectedFileRevision`, normally from an open pane or a
directory entry. A mismatch before or after reading produces `stale_file` and
returns no content. A path replacement, truncation race, or observable metadata
change during an unguarded read also produces `stale_file`.

These checks detect ordinary concurrent writes but are not a filesystem
snapshot. An in-place writer can race a read without an observable identity or
metadata change, and a successful preview may then contain the bytes actually
read rather than a coherent historical version. Diff, review, and editing
contracts that require stronger evidence must use content digests or retained
snapshots instead of treating this revision as such evidence.

Directory listings and file reads enter bounded, fair per-session work queues.
The negotiated limits report the per-session active and pending capacity. A
request waits asynchronously while capacity is available, respects context
cancellation and transport deadlines, and is removed from the queue when its
caller leaves. `capacity_exceeded` is returned only when the bounded pending
queue is full, not merely because the active-work limit has been reached.
A separate cancellation-safe daemon-wide pending queue is checked before a
request waits on the daemon active-work gate; overflow returns
`capacity_exceeded` with `scope=daemon`. Daemon-wide admission prevents one or
many sessions from exhausting file descriptors, memory, filesystem I/O, or
response bandwidth, but its concrete limit is server policy. Admission covers exact-name and containment work as
well as directory materialization or descriptor reading.

R1 reads return at most 1,000,000 content bytes and 5,000 logical lines. A
logical line is an LF-terminated sequence or one final non-empty unterminated
sequence; an empty file has zero lines. The server reads only the bounded prefix
plus the lookahead needed to prove byte or line truncation. Content is returned
verbatim; line endings are not normalized. A partial terminal UTF-8 sequence at
the byte boundary is excluded from content and counts toward byte truncation.
The truncation reason is whichever boundary occurs first in source order, with
`byte_limit` winning when both occur at the same boundary. `truncated` and its
reason are explicit, so a renderer can show partial content without presenting
it as the complete file. R1 does not provide arbitrary byte-range reads. A
future range/page operation must retain the same workspace ID and file revision
guards.

A preview is classified as binary, and no content is returned, when the
inspected bounded prefix contains NUL or malformed UTF-8. This is intentionally
a safe text-projection rule rather than a MIME claim about uninspected bytes.
Empty UTF-8 files are valid text files.

### Errors and transport mapping

Workspace operations use a typed error envelope with a stable `code`, a
renderer-safe `message` of at most 512 UTF-8 bytes, and at most four
code-specific safe string details of at most 128 bytes each, such as the current
workspace ID. The required codes are:

| Code | Meaning | HTTP class |
| --- | --- | --- |
| `invalid_path` | malformed or non-canonical input | 400 |
| `not_found` | requested entry or final symlink target is absent | 404 |
| `not_directory` | list target is not a real directory | 400 |
| `not_file` | read target is not a regular file | 400 |
| `permission_denied` | host denied the requested operation | 403 |
| `outside_workspace` | resolution escaped or could not prove containment | 403 |
| `symlink_traversal` | an intermediate or directory symlink was requested | 400 |
| `binary_file` | bounded preview cannot be projected as UTF-8 text | 415 |
| `stale_workspace` | expected workspace ID is no longer current | 409 |
| `stale_file` | expected or observed file revision changed | 409 |
| `stale_cursor` | directory pagination observation is unavailable or mismatched | 409 |
| `limit_exceeded` | request limits themselves are invalid | 413 |
| `capacity_exceeded` | the bounded pending-work queue is full | 429 |
| `unavailable` | authoritative workspace service is closing or temporarily unavailable | 503 |

Error `details` has an exact per-code schema. `stale_workspace` may contain
`currentWorkspaceId` and `currentWorkspaceState`; `stale_file` may contain
`currentFileRevision` only when that value was safely observed;
`limit_exceeded` contains the symbolic `limit` that was crossed; and
`capacity_exceeded` contains `scope`, either `session` or `daemon`. The allowed
`limit` values are `path_bytes`, `path_components`, `path_name_checks`,
`page_size`, `directory_entries`, `directory_response_bytes`, and
`directory_observation_bytes`. All detail
values are strings. All other workspace codes have no details. Optional stale details
are omitted rather than filled with an unsafe or unknown value.

A missing session remains the session protocol's normal `not_found` error.
Cancellation remains cancellation rather than being converted to a workspace
error. Unexpected host failures are sanitized as internal errors. Boundary
validation occurs on both server and client, including successful records and
error details.

The server resolves containment before classifying an outside target, and it
does not return host error text verbatim. This prevents permission and missing
errors from becoming an oracle for paths beyond the workspace.

### Client projections

The session client preserves canonical path, workspace ID, directory revision,
file revision, truncation, omission, and typed-error semantics. It
may provide typed convenience values, but it does not collapse them into generic
strings or hide partial-result state.

A TUI or web renderer keys directory state by
`(workspaceId, directoryPath, directoryRevision)` and file panes by
`(workspaceId, path)`. It guards asynchronous completion with the workspace ID and a renderer-local request generation. A cwd change
invalidates loaded directory observations and marks or reloads old file
panes; it never causes an existing pane to begin showing the same relative path
from a different root. External file changes are handled by refresh and
`stale_file`, preserving selection and scroll position only when the renderer
can do so safely.

File content remains untrusted even when it is valid UTF-8. TUI clients render
it through text/cell APIs that cannot interpret terminal escape sequences, and
web clients insert it as text rather than HTML; bidi and control characters in
content are never executed as markup or terminal bytes.

This contract supplies semantic entries and text, not terminal rows, HTML,
syntax tokens, tree expansion, fuzzy-match scores, or pane identity policy.
Those remain client and renderer projections.

### Separation from diff and review

Workspace listing and reads describe only the live filesystem under one
session-scoped workspace ID. They do not run Git, produce patches or hunks,
retain agent edit evidence, define base/head sides, or own review comments.

Diff and review contracts receive their own target and revision identities and
bounds. They may reuse the canonical workspace-path syntax and may reference a
workspace file revision when a diff side corresponds to the current file, but a
workspace ID is not a Git tree, commit, patch, or review revision. A
future revision-pinned file-range operation can extend the `workspaceFiles`
facet without changing directory pages or making ordinary file reads depend on
review state.

## Required properties

- No client filesystem path is treated as authoritative workspace content.
- Every operation is bound to one session and one expected workspace ID.
- Canonical protocol paths cannot be absolute or escape through syntax or
  symlinks.
- Successful list and read results are bounded and identify the observation
  they represent.
- Pagination cannot be replayed against a different session, root, directory,
  policy, or observation.
- Missing, permission, binary, stale-workspace, stale-file, and stale-cursor
  states remain distinguishable by clients.
- Concurrent cwd mutation cannot relabel old-root data as current, and
  observable concurrent file mutation fails stale rather than receiving a new
  revision label.
- TUI and web clients can implement equivalent exploration semantics without
  importing server internals or executing Git.
- File exploration does not become the diff or review authority.

## Consequences

Clients can render lazy directory trees and safe text previews for local or
remote sessions through one semantic boundary. Stable workspace identities,
scoped observation revisions, and explicit truncation let renderers retain
useful state while rejecting late or stale results. Descriptor-based
containment and non-traversal of directory symlinks
reduce escape and race risk.

The server must maintain bounded directory observations or use equivalently
stable opaque cursors, and it must implement platform-specific safe-open logic
for macOS and Linux. Listings are observations rather than live synchronized
trees. R1 file previews are prefix-bounded rather than arbitrary-range readers,
and symlinked directories are intentionally less convenient than ordinary
directories.

## Scope boundaries

This contract does not define filesystem watch events, recursive tree snapshots,
server-side fuzzy search, arbitrary revision-pinned ranges, durable file
snapshots, or workspace mutation operations. It also does not own Git status,
diff/review contracts, syntax highlighting, or client pane behavior. Those
capabilities require their own accepted contract and backlog requirement; this
ADR does not commit Kit to implementing them.

## Related

- [0001: Native Go application architecture](0001-native-go-architecture.md)
- [0013: Bound initial transcript snapshots](0013-bound-initial-transcript-snapshots.md)
- [`../features/session-cwd.md`](../features/session-cwd.md)
- [`../../backlog/core.md`](../../backlog/core.md) (`CORE-WORK-001`)
- [Native TUI backlog](../../backlog/tui.md)
