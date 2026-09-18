# 0020: Add file, diff, and review workspace surfaces

## Status

Accepted

## Context

[ADR 0018](0018-retained-native-workspace-shell.md) establishes the accepted
native workspace shell, retained pane lifecycle, exhaustive pane registry, and
durable subagent conversation tabs. The shell intentionally accepts additional
pane kinds. File surfaces can use the accepted workspace-file contract, while
Diff and review surfaces still require authoritative contracts.

Directory browsing is navigation rather than retained evidence. Files and diffs
need revision-aware identity and stale-state behavior, while review spans those
surfaces and the composer. Treating review as another generic tab would separate
comments from their evidence and duplicate workflow state.

## Decision

### Contract dependencies

The server owns canonical workspace paths, workspace incarnations, bounded file
and directory reads, revision-aware diffs, review targets, and draft annotations.
Renderers consume these capabilities through session-client interfaces and never
use display cwd or host filesystem access as I/O authority.

Workspace file contracts are accepted in
[ADR 0019](0019-expose-session-workspace-files.md) and implemented by
`CORE-WORK-001`. Draft annotation identity, lifetime, anchors, and message
submission are defined by
[ADR 0022](0022-model-draft-annotations-as-session-inputs.md). Diff and review
target surfaces remain blocked on `CORE-DIFF-001` and `CORE-REVIEW-001`.

### Workspace file picker

The workspace file action opens Kit's standard picker modal above the selected
Agent or workspace surface. It uses the attached session's bounded indexed
project-path source, shared with composer `@` mentions, and presents one flat
fuzzy-searchable list. The shared source owns session and cwd association,
entries, loading, error, truncation, cache freshness, generation, cancellation,
and guarded asynchronous completion so simultaneous picker consumers do not
start independent index loads.

Enter on a regular file opens or selects its retained File tab through the
current `WorkspaceRef` and dismisses the modal. Indexed directory entries may
improve path matching and presentation, but they do not require drill-down and
do not create tabs. Explicit refresh supersedes and cancels an older index load
and forces the daemon to rebuild rather than reuse its freshness cache.
The modal owns only its query, focused path, current workspace reference, and
invocation lifecycle; cwd changes and session replacement close and reset it.
The 4,000-entry index bound is presented explicitly when the source reports
truncation.

The workspace directory-listing contract remains available unchanged for
future static directory or tree displays. It is not the global file picker's
loading model. Retained File panes continue to read content through the
`WorkspaceFilesSession` contract and treat its `WorkspaceRef` as authoritative.

### File and Diff panes

File and Diff panes use the accepted descriptor, registry, host, and retained
lifecycle from ADR 0018. They receive the complete content width and may own a
responsive in-pane repository drawer with a named breakpoint.

Initial identities are:

- mutable file: attached session, workspace incarnation, and canonical
  workspace-relative path, independent of the action that opened it;
- revision-pinned review file: authoritative review target identity plus
  canonical repository-relative path; and
- diff: authoritative logical target identity covering the working tree or an
  explicit commit or branch review target.

A revision update is guarded input and does not append a duplicate tab. Open
requests may carry a line, range, hunk, or changed-file anchor; anchors are
navigation input rather than identity. Repeated opens update the existing pane's
pending reveal target and focus it.

A cwd-incarnation change makes old mutable workspace panes read-only and stale
while preserving their last loaded content and local position. They cannot
refresh, edit, or fetch additional pages. Opening the same relative path under
the new incarnation creates a distinct pane.

File panes provide selectable syntax-highlighted text, line numbers, bounded
horizontal and vertical navigation, and explicit binary, unreadable, stale,
truncated, loading, empty, and error presentation. Diff panes provide changed
file and hunk navigation, semantic added/removed/context styling, line-number
gutters, and unified or split layouts where width permits.

### Annotations and review without a Review tab

Annotations are session-owned inputs for a message being drafted, as defined by
ADR 0022. They render immediately after their anchored line or range in the
applicable File or Diff pane and project as synchronized structured chips above
the fixed composer, visible from Agent and every workspace tab.

Each chip identifies its resource and range plus a bounded server-derived
preview. Activating it opens or selects the corresponding File or Diff tab and
reveals the anchor. Editing or deleting either projection updates or deletes the
same live annotation. Closing a File or Diff tab does not discard its draft
annotations. Successful message acceptance stores immutable submitted snapshots
in the message and removes the corresponding live annotations.

Review is a target-scoped workflow over annotations rather than the owner of
their identity or persistence. Starting review, selecting its target, navigating
changed or skipped files, and submitting use bounded standard picker or
confirmation modals. Stale annotations remain visible with warning treatment
and block submission. There is no Review pane descriptor or retained Review tab.

### Ownership and lifecycle

The TUI owns picker query and expansion, pane selection and scroll, selected
line ranges, local drawer state, pending reveal anchors, and other ephemeral
presentation state. The server remains authoritative for content, revisions,
diffs, targets, live annotations, and immutable submitted-annotation snapshots.

File and Diff definitions derive quiet changed, stale, draft-count, or error
markers without reordering, selecting, or focusing their tabs. Hidden panes obey
ADR 0018's input, hit-testing, polling, generation, and disposal rules.

## Required properties and verification

Tests must establish:

- shared indexed-source single-load behavior, fuzzy filtering, refresh,
  loading, empty, error, and explicit truncation presentation in both pickers;
- generation and cancellation guards for refresh, cwd changes, and session
  replacement;
- keyboard and primary-mouse navigation plus guarded opening of regular files
  through the current workspace reference;
- identity and deduplication across every supported origin for mutable and
  review-pinned files and working-tree, commit-review, and branch-review diffs;
- repeated opens revealing anchors without remounting or reordering panes;
- selectable file and diff presentation at representative wide and narrow
  widths, including truncation and unavailable content;
- cwd-incarnation changes preserving frozen old content while refusing further
  reads, with the same path opening distinctly in the new incarnation;
- one-to-one inline annotations and structured composer chips, including create,
  edit, delete, anchor navigation, stale state, ordered submission, immutable
  message snapshots, and removal of submitted drafts;
- retention of pane-local selection, scroll, drawer, and draft state across tab
  switches and resizing, with disposal on close or session replacement; and
- hidden-pane activity markers without background focus, input, or expensive
  work.

## Consequences

The accepted shell can ship independently with Agent and subagent conversations
while File, Diff, and review work proceeds behind explicit server contracts.
Indexed file navigation remains a bounded modal, evidence remains in retained
full-width panes, and annotations stay synchronized with both their evidence and
the composer without adding a Review tab. Static directory/tree surfaces may still
use the separate bounded directory contracts when their presentation requires
that hierarchy.

This work adds feature-specific identity, stale-state, indexed-source,
annotation, and review coordination complexity. Those concerns remain outside the generic
workspace controller and must not weaken its client-local lifecycle boundaries.

## Related

- [ADR 0018: Retain native workspace panes in full-width tabs](0018-retained-native-workspace-shell.md)
- [ADR 0019: Expose session workspace files through bounded contracts](0019-expose-session-workspace-files.md)
- [ADR 0022: Model draft annotations as session inputs](0022-model-draft-annotations-as-session-inputs.md)
- [Native TUI backlog](../../backlog/tui.md)
- [Core and protocol backlog](../../backlog/core.md)
