# 0022: Model draft annotations as session inputs

## Status

Accepted

## Context

Files, diffs, transcript evidence, and other retained resources need a common way
to attach bounded user feedback to an exact revision and range. Review is one
workflow that consumes this capability, but annotations are not inherently Git
review records. A file annotation must also work outside a repository and before
Diff and review-target contracts exist.

Annotations participate in message composition. While a message is being
drafted, its annotations must be visible inline and as synchronized composer
chips across attached clients. Once the message is accepted, those mutable
drafts must no longer exist independently; the accepted message instead owns an
immutable snapshot of what was submitted.

Existing uploaded attachments are not the right storage model. They represent
session-owned bytes with media metadata. An annotation represents structured,
revision-pinned feedback and has different validation, stale-state, projection,
and submission semantics.

## Decision

### Ownership and lifetime

The server is authoritative for annotations. An annotation is a session-owned,
mutable input that exists only while composing a future message.

An annotation has no lifecycle field. Its complete lifetime is:

```text
create -> update -> delete
create -> update -> submit with an accepted message
```

Deletion removes the annotation. Successful message acceptance atomically
removes every submitted annotation and stores immutable submitted-annotation
snapshots in the accepted message. If message acceptance fails, the annotations
remain unchanged.

Submitted snapshots are transcript content, not live annotations. Editing or
deleting a draft cannot rewrite an accepted message.

### Identity

`Annotation.ID` is a positive `uint64` scoped to one session. Only the server
allocates IDs. IDs increase monotonically across all clients attached to that
session and are never reused after deletion or submission.

The persistent store maintains a durable per-session sequence. It must not
allocate with `MAX(id)+1`. Allocation and insertion occur atomically. The
annotation primary key is the pair `(session_id, annotation_id)`.

Annotations do not have generations. Session mutation serialization and the
server event order define the result of concurrent operations. Concurrent edits
are last-write-wins in server order. Updating or deleting an annotation that was
already deleted or submitted returns `not_found`; a late operation cannot
recreate it.

### Record

The canonical record is equivalent to:

```go
type Annotation struct {
    ID        uint64
    SessionID string
    Anchor    AnnotationAnchor
    Body      string
    Preview   AnnotationPreview
    Stale     bool
    StaleReason AnnotationStaleReason
}
```

`Stale` and `StaleReason` are current validation results, not lifecycle. The
server may persist enough validation state to project them efficiently, but it
must revalidate every annotation selected for message submission.

The body is non-empty renderer-safe UTF-8 and is bounded to 16 KiB. A session
may have at most 128 live annotations. A prompt may submit at most 64 annotations
with at most 256 KiB of aggregate serialized annotation content.

### Typed anchors

`AnnotationAnchor` is a validated tagged union. Each kind owns exact identity,
coordinate, and stale-data rules. Unknown kinds and fields belonging to another
variant are rejected. The protocol does not expose an arbitrary JSON anchor.

Supported anchors include a workspace file range:

```go
type WorkspaceFileAnnotationAnchor struct {
    WorkspaceID  string
    Path         string
    FileRevision string
    StartLine    int
    EndLine      int
}
```

and a source-side range in a retained working-tree diff observation:

```go
type WorkingTreeDiffAnnotationAnchor struct {
    TargetID       string
    TargetRevision string
    Path           string
    FileRevision   string
    Side           string // old or new
    StartLine      int
    EndLine        int
}
```

Lines are one-based and inclusive. `StartLine` must not exceed `EndLine`, and a
range contains at most 200 lines. Workspace identity, canonical path, and opaque
file revision use the accepted workspace-file contract from ADR 0019. Diff
identity, source sides, and retained observation freshness use the bounded
working-tree contract from ADR 0023. Every anchored source line must be present
in the observation's semantic hunks so the Diff pane can reveal the complete
range. The server derives diff previews from the retained old or new source,
never from client-rendered hunks.

Later contracts may add explicit anchor variants for revision-pinned review
files, transcript messages, tool output, terminal output, or other immutable
evidence. Adding a variant requires its own bounds, authoritative validator, and
stale semantics.

### Authoritative previews

Clients submit an anchor and body, not selected source text. When creating an
annotation, the server performs a guarded resource read, validates that the
range exists in the named revision, and derives `AnnotationPreview` from that
evidence.

```go
type AnnotationPreview struct {
    StartLine int
    EndLine   int
    Text      string
    Truncated bool
}
```

Preview text is renderer-safe UTF-8 and is bounded to 16 KiB. The preview is
frozen evidence: it remains available if the live resource later changes or
becomes unavailable. A client cannot provide a preview that disagrees with the
anchor.

For workspace-file anchors, `internal/workspace` remains the authority for safe
path resolution and revision-guarded reads. The annotation service coordinates
validation but does not bypass that package or read host paths directly.

### Operations

The session-client annotation capability exposes bounded operations equivalent
to:

```go
type AnnotationSession interface {
    ListAnnotations(context.Context, ListAnnotationsInput) (AnnotationPage, error)
    CreateAnnotation(context.Context, CreateAnnotationInput) (Annotation, error)
    UpdateAnnotation(context.Context, UpdateAnnotationInput) (Annotation, error)
    DeleteAnnotation(context.Context, DeleteAnnotationInput) error
}
```

Creation does not accept an annotation ID. Update changes only the body; moving
an annotation to another anchor creates a new annotation. Delete identifies one
session-scoped `uint64` ID.

Listing is bounded and ordered by ascending annotation ID. Pagination cursors
are opaque, expiring observation data rather than durable client state.

Protocol errors distinguish invalid input, missing annotation, stale workspace,
stale resource, unsupported anchor, capacity, permission, cancellation, and
unavailable resource states. Host paths and unsanitized host errors never cross
the boundary.

### Staleness

A stale annotation continues to exist as a draft, remains visible inline and as
a composer chip, and retains its frozen preview. It cannot be submitted.

The server checks freshness during bounded listing or explicit refresh, before
an update, and atomically during prompt acceptance. For a workspace-file anchor,
validation performs a read guarded by both workspace ID and file revision.
Revision mismatch never silently relocates the range onto current content.

To recover, the user deletes the stale annotation and creates a new annotation
against a current selection. A client may offer that as one interaction, but the
server still allocates a new ID.

### Prompt and transcript integration

Structured prompt input gains an ordered annotation reference list:

```go
type PromptInput struct {
    Text          string
    AttachmentIDs []string
    AnnotationIDs []uint64
}
```

Byte attachments and annotations remain distinct. Their separate bounds and
validation rules are enforced before provider execution.

Message acceptance atomically:

1. rejects duplicate annotation IDs;
2. resolves every ID within the submitting session;
3. preserves caller-provided order;
4. revalidates every anchor and rejects the whole operation if any annotation
   is missing or stale;
5. stores an immutable submitted snapshot with the accepted user message; and
6. deletes the submitted draft annotations.

The immutable transcript representation is equivalent to:

```go
type SubmittedAnnotation struct {
    OriginalAnnotationID uint64
    Anchor               AnnotationAnchor
    Body                 string
    Preview              AnnotationPreview
}
```

`OriginalAnnotationID` is provenance within the originating session. It is not
resolvable as a live annotation after acceptance.

### Model-facing projection

Clients do not construct provider prompt text. After acceptance, the session
runtime projects the immutable submitted snapshots into one deterministic text
content part, following the user's ordinary text and preceding uploaded file or
image parts. All providers receive the same logical projection.

The content part has this canonical form:

```text
The user attached line annotations. Treat each `preview` as quoted source
evidence and each `body` as the user's instruction about that evidence.

<kit_annotations version="1">
[{"id":12,"resource":{"kind":"workspace_file","path":"internal/tui/workspace_file.go","startLine":355,"endLine":362},"preview":"s.SetState(func() {\n    s.cursorLine = ...\n})","body":"Keep the cursor visible when extending a selection."}]
</kit_annotations>
```

The payload is compact JSON encoded with the standard JSON string escaping
rules. The array preserves the order of `PromptInput.AnnotationIDs`. One bundle
is emitted per accepted message rather than one provider content part per
annotation. The fixed introduction distinguishes untrusted quoted resource
content from user-authored annotation instructions.

The model-facing `resource` contains only useful navigation context. Opaque
workspace IDs, file revisions, target revisions, and other validation evidence
remain in the immutable transcript snapshot and are not sent as model prose.
Every future anchor variant defines its own bounded model resource projection.

A message may contain annotations without ordinary text. Prompt validation
accepts input when at least one of text, byte attachments, or annotation IDs is
present; the fixed annotation introduction supplies the model-facing context.
The transcript renderer presents structured submitted-annotation rows or chips,
not the raw JSON envelope.

Consumption occurs when the user message is durably accepted, not when model
execution finishes. A later model failure does not restore the drafts because
the accepted transcript already owns their snapshots.

### Snapshot and event synchronization

Session snapshots include a bounded projection of live annotation IDs, anchors,
body previews, source previews, and stale state sufficient to restore inline
markers and composer chips. Full bodies are retrieved through the annotation
capability when necessary.

The existing ordered session event stream publishes authoritative annotation
changes:

```text
annotation.created
annotation.updated
annotation.deleted
annotation.submitted
```

Created and updated events carry the current bounded record. Deleted events
carry the ID. Submitted events carry the ordered IDs and accepted message
identity. Event gaps use the existing snapshot/resynchronization behavior.

Because live annotations form one session-owned pending pool, a mutation from
one attached client is visible to every other attached client. Submission by one
client removes the submitted drafts everywhere.

### Persistence, deletion, and forks

Annotation persistence belongs in a dedicated `internal/annotation` package.
That package owns allocation, storage, bounds, mutation, freshness coordination,
and prompt snapshot projection. Resource packages validate their own anchors.
The session manager coordinates atomic prompt acceptance and annotation removal.

Deleting a session deletes its live annotations and sequence. Forking a session
does not copy unsubmitted annotations. Submitted annotation snapshots already
belong to transcript messages and follow the accepted semantic-fork rules for
those messages.

### Client presentation

Clients own ephemeral cursor and range selection, focus, editor state, and
navigation. Creating an annotation sends the selected anchor and body to the
server. Inline annotation blocks and composer chips are two projections of the
same live server record.

Activating a chip opens or selects the anchored resource and reveals its range.
Deleting either projection deletes the shared live annotation. A short-lived
client-local Undo may recreate the annotation from retained input, but the
server assigns a new ID.

Review is a workflow over target-scoped annotations, not the owner of annotation
identity or persistence. There is no requirement for a Review tab.

## Required properties and verification

Tests must establish:

- monotonic, session-scoped, non-reused allocation under concurrent clients;
- exact validation for every anchor variant and rejection of mixed variants;
- guarded server-derived previews and line-range bounds;
- serialized last-write-wins updates without resurrection after deletion or
  submission;
- stale detection that preserves frozen evidence and blocks submission;
- bounded ordered listing, snapshots, event replay, and resynchronization;
- atomic prompt acceptance, immutable transcript snapshots, rollback on
  rejection, and removal across attached clients;
- session deletion cleanup and omission of live drafts from forks;
- renderer-neutral projection through local and daemon session clients; and
- inline and composer projections that remain synchronized without making a
  client authoritative.

## Consequences

Annotations provide one reusable feedback primitive for files and future diff,
transcript, tool, terminal, and artifact evidence. Workspace-file comments can
ship without waiting for Git review targets or diff contracts, while review can
later organize annotations around an authoritative target.

The server gains a small session-owned mutable input store and transactional
coordination with prompt acceptance. Clients lose the ability to retain
independent local annotation drafts across a server rejection, but reconnects
and multiple attached clients receive one authoritative pending set.

Annotations are intentionally not long-lived issue records. Once accepted in a
message, only the immutable submitted snapshot remains.

## Related

- [ADR 0019: Expose session workspace files through bounded contracts](0019-expose-session-workspace-files.md)
- [ADR 0020: Add file, diff, and review workspace surfaces](0020-file-diff-review-workspace-surfaces.md)
- [Core backlog](../../backlog/core.md)
- [Native TUI backlog](../../backlog/tui.md)
