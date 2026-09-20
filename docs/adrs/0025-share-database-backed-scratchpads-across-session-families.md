# 0025: Share database-backed scratchpads across session families

## Status

Accepted

## Context

A scratchpad is durable Markdown working memory that a user and the parent agent
can update while working across a related set of sessions. A fork is an
independent conversation, but information recorded while exploring a fork may
remain pertinent to its ancestors and siblings. Making each fork own a copied
scratchpad would allow those notes to diverge and disappear when the fork is
removed.

Scratchpad content is application state rather than a project file. It does not
need external-editor interoperability, a host filesystem path, or implicit
inclusion in every provider request. Kit needs one authoritative representation
that supports concurrent attached clients, agent edits, autosave conflicts,
bounded event delivery, and recovery after missed events.

The server is authoritative for shared state. Session clients are permanently
bound to one session, while forks form a lineage rooted at one persistent
session. Scratchpad ownership and synchronization must preserve those
boundaries without making clients resolve lineage or directly access Kit's
storage.

## Decision

### Session-family ownership

One scratchpad belongs to one persistent root-session family. The root session
owns the scratchpad, and every descendant fork reads and edits that same record.
A fork of a fork retains the original root owner.

Every persistent session stores a non-empty `ScratchpadOwnerID`:

- a root session uses its own session ID;
- a semantic fork copies the source session's `ScratchpadOwnerID`; and
- changing session name, cwd, model, or archive state does not change ownership.

`ParentSessionID` alone does not establish scratchpad membership: it also records
provenance for independently created peer sessions under ADR 0017. Those peers
own independent scratchpads. The session manager explicitly selects self
ownership for ordinary and model-created sessions and inherited ownership only
for semantic forks.

The server validates this identity and never accepts a client-selected owner.
Clients operate through their bound session; the server resolves its persisted
owner. Fork creation copies only the owner identity and never copies scratchpad
content.

Archiving any family member does not delete the scratchpad. Archiving the root
also leaves the shared record available to surviving descendants. Kit's current
session deletion operation is archival, so physical family and scratchpad
reclamation remain outside this decision.

Temporary sessions remain process-local under ADR 0009 and do not create
scratchpad rows or expose the durable scratchpad capability.

### Authoritative record and persistence

The canonical record is equivalent to:

```go
type Scratchpad struct {
    OwnerSessionID string
    Content        string
    Revision       int64 // encoded as a canonical decimal string on the wire
    UpdatedAt      time.Time
}
```

Scratchpads are stored in the daemon-owned Kit SQLite database under
`KIT_HOME`. There is no scratchpad Markdown sidecar, virtual host path, or
second materialized copy. SQLite is the only persistence authority.

The storage schema has one scratchpad row keyed by root owner session ID. Root
session creation and initial empty scratchpad creation are one atomic registry
operation. The inherited owner ID is atomic with the Kit child registry row and
participates in the existing recoverable cross-store fork workflow from ADR
0005; it does not make droid-store creation and Kit registry publication one
transaction. Foreign-key constraints and deletion guards prevent an owner or
scratchpad row from disappearing while its family remains addressable.

`Content` is canonical multiline UTF-8 Markdown and may be empty. Scratchpad
validation is distinct from the single-line renderer-text predicate: content
must be valid UTF-8, use LF rather than CR line endings, reject every Unicode
format character (`Cf`), and reject every Unicode control character (`Cc`)
except LF and tab. Its encoded UTF-8 length is at most 64 KiB after validation.
The same predicate is enforced before entering a write transaction and at
protocol, tool, service, and persistence boundaries. `Revision` is a positive,
monotonically increasing signed 64-bit value compatible with a SQLite integer.
Every content change increments it exactly once; a no-op update returns the
current record without incrementing it. Reaching `MaxInt64` makes further
content changes fail with a typed exhaustion error. `UpdatedAt` is server
assigned.

### Session-client operations

Scratchpad support is an optional bound-session capability equivalent to:

```go
type ScratchpadSession interface {
    Scratchpad(context.Context) (protocol.Scratchpad, error)
    UpdateScratchpad(context.Context, protocol.UpdateScratchpadInput) (protocol.Scratchpad, error)
}

type UpdateScratchpadInput struct {
    ExpectedRevision int64 // encoded as a canonical decimal string on the wire
    Content          string
}
```

The update is an atomic compare-and-swap. It succeeds only when
`ExpectedRevision` equals the authoritative revision. A stale update returns
HTTP 409 with the stable code `scratchpad_revision_conflict` and a bounded,
validated `scratchpad` details field containing the complete current record.
Local transports project the same typed conflict. This lets clients recover
without a second read.

The scratchpad error contract is:

- `scratchpad_invalid_content` (HTTP 400);
- `scratchpad_too_large` (HTTP 413);
- `scratchpad_revision_conflict` (HTTP 409 with the current record);
- `scratchpad_revision_exhausted` (HTTP 409);
- `scratchpad_migration_required` (HTTP 409);
- `scratchpad_unsupported` (HTTP 409); and
- `scratchpad_unavailable` (HTTP 503 with no unsanitized storage detail).

Request cancellation uses the transport's existing cancellation behavior rather
than manufacturing a response after the requester is gone. Local transports
project the same stable error codes and typed details.

Domain revisions are positive `int64` values; JSON encodes them as canonical
non-zero decimal strings so browser clients never lose precision. Malformed,
zero, signed, non-canonical, or overflowing revision values are rejected on
both sides of the boundary. `updatedAt` is UTC RFC 3339 with nanoseconds, using
the same canonical encoding and validation on snapshots, responses, conflict
details, and events.

The operation does not accept a session ID or owner ID in its body. The bound
session supplies both authority and family resolution. Local and remote
transports expose identical validation and conflict behavior.

Human updates use revision comparison. Agent exact edits instead execute one
server-side read/validate/write transaction against the latest committed
content. SQLite transactions and revision comparison arbitrate concurrent
family mutations without an application-level owner lock.

### Events and snapshot recovery

Every committed content change publishes a renderer-neutral
`scratchpad.changed` session event containing the complete authoritative
record:

```text
scratchpad.changed {
  ownerSessionId
  content
  revision
  updatedAt
}
```

The event has no parent turn or run identity. A mutation through any descendant
is fanned out to attached clients for every loaded session whose
`ScratchpadOwnerID` matches. The event's normal `sessionId` remains the bound
session stream receiving that projection; `ownerSessionId` identifies the
shared resource.

Events are published only after the SQLite transaction commits. Draft
keystrokes, scheduled autosaves, rejected updates, and no-op updates do not
publish events. User updates and agent-tool updates use the same event path.

SQLite revision comparison serializes competing human replacements. Agent
exact edits perform their read, replacement validation, and write in one SQLite
transaction against the latest committed record. No application-level owner
lock is required.

Session synchronization holds the bound session's event-log lock while it
captures the event cursor and then reads the authoritative scratchpad. A
mutation commits first, copies the currently loaded family runtimes under the
manager registry lock, releases that lock, and appends to each stream
independently. A runtime is published in the manager registry before its first
snapshot. This ordering gives every snapshot one safe outcome: older content
with the change still after its cursor, or newer content with a possibly
duplicate event. Concurrent writers may append events out of revision order;
clients apply only records with newer revisions, so the database revision
remains authoritative. A daemon failure after commit but before complete
fan-out is recovered by a new stream and authoritative snapshot.

The complete record fits within the existing 128 KiB session-event payload
bound because content is limited to 64 KiB. Clients apply a record only when its
revision is newer than the record they hold, making duplicate delivery and the
race between a command response and its event harmless.

Adding full-content events also adds byte bounds to the session event path. One
loaded runtime retains at most 8 MiB of encoded events in addition to its
existing event-count bound. One event page or SSE record contains at most 512
KiB of encoded events; pagination emits fewer events rather than exceeding the
byte limit, and one individually valid scratchpad event always fits. Full-content
events do not evict the protected start of an active run: if retained-stream
capacity cannot hold both, the existing live-replay-unavailable and
snapshot-resynchronization path applies rather than growing without bound or
discarding protected run events.

Family fan-out walks a stable snapshot of currently loaded runtimes and appends
to their event logs sequentially rather than spawning one goroutine per
descendant. Scratchpad delivery therefore adds no unbounded concurrency and at
most one bounded event append per loaded family member.

The authoritative session snapshot includes the current scratchpad record when
the capability is available. A client that misses an event, falls outside the
retained event suffix, or reconnects reconciles from the snapshot. Event
publication is therefore a live synchronization optimization rather than a
second durability mechanism.

### Model and tool boundary

Scratchpad content is not a system-prompt section, context file, automatic user
message attachment, or implicit provider input. Creating or changing a
scratchpad does not rebuild or reconfigure the session prompt. A prompt may run
without reading the scratchpad.

Top-level parent agents in persistent root sessions and their forks receive two
dedicated tools:

- `read_scratchpad` reads the current shared record through the bound session;
- `edit_scratchpad` applies targeted exact replacements to the current shared
  record.

Neither tool accepts a path, session ID, or owner ID. Resolution occurs when the
tool begins, so inherited conversation history cannot redirect an operation to
a different family. Tool descriptions may explain that the resource is shared
by the root and all forks; the content itself is exposed to the model only when
the model explicitly reads it or when a tool result reports an edit.

`edit_scratchpad` uses the standard exact-match, uniqueness, simultaneous-edit,
and overlap validation. One edit with empty `oldText` may initialize currently
empty content; empty `oldText` is otherwise invalid. The resulting content must
satisfy the 64 KiB limit before commit. An exact-match failure leaves the record
unchanged and publishes no event.

Generic `read`, `write`, and `edit` tools cannot address scratchpad content
because it has no filesystem path. Subagents do not receive scratchpad tools in
the initial contract. A later grant to subagents must explicitly define whether
they share the parent family's owner and how concurrent mutations are
attributed and bounded.

### Client drafts and autosave

An editor owns a local draft and the authoritative revision from which that
draft began. Keystrokes update only the draft; they are not server state and do
not affect agent context. The macOS editor submits a compare-and-swap update
after a five-second quiet period; the TUI uses 250 ms. Only committed responses
and events replace the client's authoritative record.

The editor exposes explicit loading, saving, clean, unsaved, conflict, and save
failure states. When a newer family event arrives:

- a clean editor silently adopts the event content and revision while preserving
  a valid cursor and scroll position where possible;
- a dirty editor whose draft equals the event content becomes clean at the new
  revision; and
- any other dirty editor preserves its draft, enters conflict state, and stops
  automatic writes.

A clean remote update does not produce a toast, banner, or other origin notice.
A conflict is different because it blocks saving: the pane shows a persistent
inline warning with a **Review changes** action. Review temporarily replaces the
editor body with a selectable, scrollable unified diff from the current shared
content to the local draft. It does not open another workspace tab or modal.
The review offers:

- **Keep editing**, which returns to the preserved draft and leaves the conflict
  unresolved;
- **Use shared**, which explicitly discards the draft and adopts the newest
  authoritative record; and
- **Replace shared with mine**, which submits the draft against the exact shared
  revision displayed by the diff.

If the shared revision advances before replacement commits, replacement fails
closed and the review refreshes against the newer record. Neither destructive
choice occurs implicitly. The unified representation is shared across clients;
renderers may use native selection and scrolling but do not substitute a nested
side-by-side layout.

A non-conflict persistence failure offers retry. During orderly scratchpad-tab
close, a client attempts an immediate save and keeps the pane open with
actionable failure feedback when the save cannot complete. Drafts may remain in
memory while their pane or client process remains alive. In accordance with ADR
0018, `Ctrl+C` still always quits or detaches the native client after a
best-effort immediate save; client exit, crash, forced termination, and an
abandoned browser page may lose an uncommitted local draft. This decision does
not create durable client draft storage.

### Native workspace presentation

The native TUI presents Scratchpad as an on-demand singleton workspace tab under
ADR 0018. It is a full-width peer of Agent at every terminal size, uses the
shared fixed composer, and does not introduce a modal editor, split layout, or
special responsive surface. Reopening Scratchpad focuses its existing retained
tab rather than creating another.

The tab supplies the Scratchpad title. The normal pane has no repeated title,
context header, family-ownership label, preview mode, or byte-usage display. Its
body is the word-wrapped multiline Markdown editor. A quiet pane footer owns
focus hints and the current save state: unsaved, saving, saved, conflict, or save
failure. Hidden-tab decoration may expose dirty, conflict, or failure state
without selecting or reordering the tab; clean remote changes add no indicator.

The pane body owns the editor, cursor and scroll state, status feedback,
conflict review, actions, and focused keymap. It remains mounted while its tab is
open so selection and a local draft survive tab changes. Server events continue
to reconcile its authoritative record while hidden without selecting,
reordering, or focusing the tab.

### Native macOS workspace presentation

The macOS client presents the same on-demand singleton Scratchpad workspace tab.
The existing workspace group actions may move it beside Agent in the optional
second group, but split presentation is explicit user layout rather than the
scratchpad default.

The tab supplies the title and note icon. The pane otherwise contains the native
word-wrapped Markdown source editor and a quiet save-state footer, with no
repeated title, context header, preview toggle, byte-usage display, or clean
remote-update notification. Native selection, undo, redo, find, scrolling, and
accessibility remain available. The conflict warning and unified review use the
same actions and shared-to-draft orientation as the TUI, with native controls
and text selection.

Other renderers use the same scratchpad operations, event semantics, conflict
language, and local-draft rules while owning renderer-appropriate controls.

### Migration

Production migration is the only operation allowed to read scratchpad sidecars
from `~/.kit`. Before import it creates the required immutable production backup
under the explicit migration workflow. That backup, not the live v2 scratchpad
table, preserves legacy source bytes that cannot be consolidated.

The v2 schema migration backfills existing persistent sessions in one
transaction. Because parent provenance cannot prove semantic-fork lineage, each
existing row initially points to itself and receives one empty revision-one
scratchpad. An explicit migration may merge owners only after verifying canonical
droid `ForkedFrom` metadata; it never infers sharing from `ParentSessionID`.
Missing parents, cycles, or inconsistent verified lineage fail reconciliation
rather than manufacturing ownership.

Production import groups sidecars by migrated root family, canonicalizes and
validates content, and deduplicates byte-identical values. A family with zero
non-empty sources retains its empty row; a family with exactly one distinct
valid non-empty value imports it with a revision increment. Multiple distinct
non-empty values, unreadable files, invalid content, and oversized content do
not cause one source to be chosen silently. The family is marked `migration_required`, the
migration journal records bounded paths, hashes, and diagnostics pointing into
the backup, and the migration reports that explicit reconciliation is required.
While marked, reads and writes return `scratchpad_migration_required`; clients
cannot treat the empty row as authoritative or edit it. Explicit reconciliation
selects or supplies one valid bounded value, compares against the still-empty
revision-one row, commits revision two, and clears the marker atomically. It
never stores unbounded conflicting copies in normal scratchpad rows.

Import, conflict reporting, and reconciliation are idempotent. Re-running with
the same backup and resolution cannot increment revisions repeatedly, duplicate
diagnostics, or overwrite content committed after the marker was cleared.
Normal v2 startup and scratchpad operations use only the configured v2
`KIT_HOME` database and never inspect `~/.kit`.

### Non-goals

This decision does not provide external file editing, project-local notes,
implicit model memory, automatic prompt attachment, scratchpad history,
collaborative character-level merging, rich Markdown preview, durable client
draft recovery, physical family purging, or subagent scratchpad access.
Revision conflicts preserve live drafts but do not merge them.

## Required properties and verification

Tests must establish:

- root sessions own themselves and every depth of fork inherits the same stable
  scratchpad owner without copying content;
- parent, child, and sibling operations observe one record and monotonically
  increasing revisions;
- archiving or deleting one fork cannot remove family scratchpad content;
- atomic compare-and-swap behavior and typed conflicts under concurrent clients;
- owner-level serialization of agent exact edits and user updates;
- multiline UTF-8, LF, control/format-character, and 64 KiB validation at
  protocol, service, tool, and persistence boundaries;
- successful mutations publish the complete committed record to every attached
  family session, while drafts, failures, conflicts, and no-ops publish nothing;
- duplicate, reordered command-response/event delivery converges by revision,
  snapshot resynchronization recovers missed events, and event retention,
  batching, and family fan-out remain within their byte and runtime bounds;
- scratchpad content is absent from system prompts and implicit provider input;
- path-free read/edit tools always resolve the bound session's family and are
  unavailable to temporary sessions and subagents;
- clean, saving, unsaved, conflict, retry, tab-switch, failed close, and
  best-effort exit behavior without claiming durable draft recovery;
- silent clean-event reconciliation and dirty-event conflict review preserve
  cursor, scroll, drafts, and revision guards as specified;
- the unified conflict diff and keep-editing, use-shared, and guarded-replace
  actions behave equivalently in native TUI and macOS presentations;
- the native panes are retained singleton editors without redundant title,
  context, preview, or byte-usage chrome, with correct focus, keyboard, mouse,
  hidden-tab, split-group, and event behavior; and
- migration never reads production state implicitly and preserves every
  divergent legacy source for explicit reconciliation.

## Consequences

A note recorded in any fork remains visible to its root, siblings, and later
descendants, so deleting an exploratory fork cannot discard shared working
memory. SQLite provides one transactional authority and removes filesystem
locking, sidecar permissions, generic file-tool interception, and dual-state
synchronization.

Clients must handle ordinary optimistic conflicts because several sessions and
attachments can edit one family resource. A full-content change event is simple
and bounded but can add up to 64 KiB to each family session's retained live event
suffix per committed edit. Autosave debounce and bounded event retention limit
that cost; a later protocol may replace content with invalidation-only events
without changing storage authority or revision semantics.

Scratchpad content consumes no model tokens unless the model explicitly reads
it. That makes the scratchpad deliberate working memory rather than ambient
instruction, but agents cannot rely on it without choosing to use the dedicated
tool.

## Related

- [ADR 0001: Native Go application architecture](0001-native-go-architecture.md)
- [ADR 0005: Fork settled droid conversations semantically](0005-droids-semantic-forking.md)
- [ADR 0009: Keep temporary sessions process-local](0009-keep-temporary-sessions-process-local.md)
- [ADR 0011: Use SSE for session event delivery](0011-use-sse-for-session-events.md)
- [ADR 0017: Create top-level sessions from model tools](0017-create-top-level-sessions-from-model-tools.md)
- [ADR 0018: Retain native workspace panes in full-width tabs](0018-retained-native-workspace-shell.md)
- [Core backlog](../../backlog/core.md)
- [Native TUI backlog](../../backlog/tui.md)
- [Native macOS backlog](../../backlog/macos.md)
