# 0020: Add file, diff, and review workspace surfaces

## Status

Proposed

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
and directory reads, revision-aware diffs, review targets, review records, and
structured review attachments. Renderers consume these capabilities through
session-client interfaces and never use display cwd or host filesystem access as
I/O authority.

Workspace file contracts are accepted in
[ADR 0019](0019-expose-session-workspace-files.md) and implemented by
`CORE-WORK-001`. Diff and review surfaces remain blocked on `CORE-DIFF-001`,
`CORE-REVIEW-001`, and `CORE-REVIEW-002`.

### Workspace file picker

The workspace file action opens Kit's standard picker modal above the selected
Agent or workspace surface. It starts at the attached session's workspace root
and combines lazy, bounded directory traversal with path filtering.

Enter on a file opens or selects its retained File tab and dismisses the modal.
Directories expand in place and never create tabs. The picker owns its query,
expanded paths, observation pages, focused row, loading, and errors for one
dialog invocation; this state is not restored across session switches.

Observation pages are keyed by server directory revision. Expansion and
selection use workspace incarnation plus canonical path so refresh can preserve
valid navigation state. Opaque server pagination cursors remain observation
cache data rather than navigation state.

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

### Review workflow without a Review tab

Review is session workflow state rather than a retained surface. Review comments
render immediately after their anchored line or range in the applicable File or
Diff pane. Saving a comment also projects it as the same structured review
attachment chip above the fixed composer, visible from Agent and every workspace
tab.

Each chip identifies its file and line or range plus a bounded preview.
Activating it opens or selects the corresponding File or Diff tab and reveals
the anchor. Editing or removing either projection updates the same review record
through a shared controller. Closing a File or Diff tab does not discard its
revision-pinned comments or attachments.

Starting review, selecting its target, navigating changed or skipped files,
reviewing drafts, and submitting use bounded standard picker or confirmation
modals. Stale comments remain visible with warning treatment and block
submission according to the review contract. Successful submission consumes
the corresponding structured attachments. There is no Review pane descriptor
or retained Review tab.

### Ownership and lifecycle

The TUI owns picker query and expansion, pane selection and scroll, local drawer
state, pending reveal anchors, and other ephemeral presentation state. The
server remains authoritative for content, revisions, diffs, targets, comments,
and durable attachments.

File and Diff definitions derive quiet changed, stale, draft-count, or error
markers without reordering, selecting, or focusing their tabs. Hidden panes obey
ADR 0018's input, hit-testing, polling, generation, and disposal rules.

## Required properties and verification

Tests must establish:

- lazy bounded directory expansion, filtering, pagination, refresh, loading,
  empty, and error behavior in the file picker;
- directory refresh replacing revision-keyed observations while preserving
  valid path-keyed expansion and selection;
- identity and deduplication across every supported origin for mutable and
  review-pinned files and working-tree, commit-review, and branch-review diffs;
- repeated opens revealing anchors without remounting or reordering panes;
- selectable file and diff presentation at representative wide and narrow
  widths, including truncation and unavailable content;
- cwd-incarnation changes preserving frozen old content while refusing further
  reads, with the same path opening distinctly in the new incarnation;
- one-to-one inline review comments and structured attachment chips, including
  add, edit, remove, anchor navigation, stale state, ordered submission, and
  successful consumption;
- retention of pane-local selection, scroll, drawer, and draft state across tab
  switches and resizing, with disposal on close or session replacement; and
- hidden-pane activity markers without background focus, input, or expensive
  work.

## Consequences

The accepted shell can ship independently with Agent and subagent conversations
while File, Diff, and review work proceeds behind explicit server contracts.
Directory browsing remains a bounded modal, evidence remains in retained
full-width panes, and review stays synchronized with both its evidence and the
composer without adding a Review tab.

This work adds feature-specific identity, stale-state, pagination, and review
coordination complexity. Those concerns remain outside the generic workspace
controller and must not weaken its client-local lifecycle boundaries.

## Related

- [ADR 0018: Retain native workspace panes in full-width tabs](0018-retained-native-workspace-shell.md)
- [ADR 0019: Expose session workspace files through bounded contracts](0019-expose-session-workspace-files.md)
- [Native TUI roadmap](../roadmap/tui.md)
- [Core and protocol roadmap](../roadmap/core.md)
