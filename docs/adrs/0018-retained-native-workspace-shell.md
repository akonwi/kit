# 0018: Retain native workspace panes in full-width tabs

## Status

Proposed

## Context

The native TUI needs one workspace model for file, diff, and subagent
conversation surfaces without turning each feature into a separate full-screen
workflow. The agent transcript remains Kit's primary narrative, while secondary
work must survive tab changes and terminal resizing.

Source files, diffs, and subagent conversations benefit from the terminal's
complete width. A permanent transcript-and-pane split makes both
surfaces narrow, introduces a second narrow-mode layout, and requires resize and
collapse state that does not contribute to the underlying task.

The server is authoritative for sessions, workspace content, revisions, diffs,
review records, and subagent execution. Pane arrangement, focus, scroll, and
selection are properties of one TUI attachment. Conflating these layers would
either leak Vaxis types into client contracts or make ephemeral client layout
into shared session state.

The root shell is viewport-native: it paints the terminal edge to edge and does
not add an enclosing border. Workspace navigation must preserve that geometry
and must not add persistent global session navigation.

## Decision

### Full-width tabbed workspace

The shell presents one full-width tabbed content region. The first surface is
**Agent**, followed by retained workspace panes in stable order. Agent contains
the existing native transcript implementation unchanged: this decision does not
redesign transcript entries, Markdown, tool activity, scrolling, streaming,
pending state, or selection.

The composer remains fixed below the content region and is available while any
tab is selected. Its attachment shelf sits immediately above it on every tab,
including structured review-comment chips. This allows the user to discuss a
visible file, diff, or subagent conversation without first returning to Agent.
A server-owned pending interaction is presented by the existing renderer-owned
interaction dock in that same composer region.

The initial workspace has no secondary panes. While Agent is the only surface,
the tab strip is omitted and the transcript uses the full content region.
Opening the first secondary pane reveals the strip and selects that pane.
Closing the final secondary pane removes the strip and returns to Agent.

There is no transcript-and-pane split, partial drawer, collapsed workspace rail,
wide/narrow mode switch, draggable workspace divider, or workspace ratio. Every
selected surface receives the full content width at every terminal size.
Pane-specific navigation, such as a repository tree within a file or diff pane,
may still use a responsive in-pane drawer with its own named breakpoint.

### Representative layouts

These diagrams show ownership and geometry rather than redefining transcript or
pane typography. The root viewport has no enclosing border.

With no secondary panes open, Kit looks like the existing transcript shell and
does not spend a row on a single obvious Agent tab:

```text
my session                                             GPT 5.6 · medium · 41%
──────────────────────────────────────────────────────────────────────────────

                         existing Agent transcript

You
Review the retry behavior.

Kit
I’ll inspect the implementation and its tests.


──────────────────────────────────────────────────────────────────────────────
Ask Kit anything…
──────────────────────────────────────────────────────────────────────────────
ready                                               ~/Developer/agent/kit (main)
```

Opening a file introduces the tab strip. Agent remains first and the selected
file receives the entire content width:

```text
my session                                             GPT 5.6 · medium · 41%
──────────────────────────────────────────────────────────────────────────────
 Agent  [server.go 2 ×]  Diff 1   reviewer ⠋
──────────────────────────────────────────────────────────────────────────────
internal/server/server.go                                      modified

 114 │ func readFrame(reader io.Reader) error {
 115 │     frame, err := decodeFrame(reader)
 116 │     if err != nil {
     ┌ Review note ────────────────────────────────────────────────────────────┐
     │ Should EOF terminate this retry generation?                            │
     └─────────────────────────────────────────────────────────────────────────┘
 117 │         return fmt.Errorf("read frame: %w", err)
 118 │     }
 119 │     return handleFrame(frame)
 120 │ }

 [Review comment · server.go:116 · Should EOF terminate this retry…  ×]
──────────────────────────────────────────────────────────────────────────────
Ask about the visible workspace evidence…
──────────────────────────────────────────────────────────────────────────────
ready                                               ~/Developer/agent/kit (main)
```

Selecting Agent restores the existing transcript without remounting file, diff,
or subagent conversation panes:

```text
my session                                             GPT 5.6 · medium · 41%
──────────────────────────────────────────────────────────────────────────────
[Agent]  server.go 2   Diff 1   reviewer ⠋
──────────────────────────────────────────────────────────────────────────────

                         existing Agent transcript

Kit
The implementation is open in server.go and the current patch is in Diff.

  Edit 2 sections   internal/server/server.go
  Run command       go test ./internal/server

 [Review comment · server.go:116 · Should EOF terminate this retry…  ×]
 [Review comment · retry_test.go:48 · Cover the exhausted retry path  ×]
──────────────────────────────────────────────────────────────────────────────
Ask Kit anything…
──────────────────────────────────────────────────────────────────────────────
ready                                               ~/Developer/agent/kit (main)
```

At narrow widths the information architecture does not change. The strip keeps
the selected tab visible and moves undisplayed tabs behind labeled overflow:

```text
my session                         GPT 5.6 · 41%
────────────────────────────────────────────────
 Agent   ⋯ 1 more  [Diff 1 ×]  reviewer ⠋
────────────────────────────────────────────────
working tree · internal/server/server.go

@@ readFrame
-  if err != nil {
+  if err != nil && !errors.Is(err, io.EOF) {
       return fmt.Errorf("read frame: %w", err)
   }
+  retry.reset()

 [Review comment · server.go:116 · EOF behavior… ×]
────────────────────────────────────────────────
Ask about the visible evidence…
────────────────────────────────────────────────
ready                           ~/agent/kit (main)
```

The overflow and pane-list action opens Kit's standard picker modal. It does not
replace the content region with an ad hoc list:

```text
my session                                             GPT 5.6 · medium · 41%
──────────────────────────────────────────────────────────────────────────────
 Agent   server.go 2  [Diff 1 ×]  reviewer ⠋
──────────────────────────────────────────────────────────────────────────────
                 ┌ Open workspace tab ─────────────────────┐
                 │ Search tabs…                            │
                 ├─────────────────────────────────────────┤
                 │   Agent                                 │
                 │   server.go                    2 notes  │
                 │ ✓ Diff                         1 note   │
                 │   reviewer                     running  │
                 ├─────────────────────────────────────────┤
                 │ enter open · x close · esc cancel       │
                 └─────────────────────────────────────────┘

──────────────────────────────────────────────────────────────────────────────
Ask about the visible workspace evidence…
──────────────────────────────────────────────────────────────────────────────
ready                                               ~/Developer/agent/kit (main)
```

The modal follows the shared picker-dialog geometry, focus trap, filtering,
selection, mouse behavior, fixed footer, and semantic theme treatment.

### Workspace file picker

Directory browsing is a navigation action, not a retained workspace tab. The
workspace file action opens a separate picker-style modal above the selected
Agent or workspace surface:

```text
my session                                             GPT 5.6 · medium · 41%
──────────────────────────────────────────────────────────────────────────────
 Agent  [server.go 2 ×]  Diff 1   reviewer ⠋
──────────────────────────────────────────────────────────────────────────────
                 ┌ Open file ─────────────────────────────┐
                 │ Search paths…                          │
                 ├────────────────────────────────────────┤
                 │ ▾ internal                             │
                 │   ▾ tui                                │
                 │       app.go                           │
                 │     › workspace.go                     │
                 │   ▸ session                            │
                 │ ▸ docs                                 │
                 ├────────────────────────────────────────┤
                 │ enter open · → expand · ← collapse     │
                 │ / filter · esc cancel                  │
                 └────────────────────────────────────────┘

──────────────────────────────────────────────────────────────────────────────
Ask about the visible workspace evidence…
──────────────────────────────────────────────────────────────────────────────
ready                                               ~/Developer/agent/kit (main)
```

The picker starts at the attached session's workspace root and combines lazy,
bounded directory traversal with path filtering. Enter on a file opens or
selects its retained File tab and dismisses the modal. Directories expand in
place and never create tabs. The picker owns its query, expanded paths,
directory-observation pages, focused row, loading, and errors for that dialog
invocation; it does not persist or restore them across session switches.
Observation pages are keyed by server directory revision, while expansion and
selection use workspace incarnation plus canonical path so refresh can
reconcile valid navigation state. Opaque server pagination cursors remain
observation-cache data rather than navigation state.

File and Diff panes may still provide an in-pane tree drawer when navigation
belongs to that workflow. Those drawers reuse the same bounded workspace client
contracts but remain independent from the global Open file picker.

### Review workflow without a Review tab

Review is session workflow state, not a separate workspace surface. Review
comments render immediately after their anchored line or range in the File or
Diff tab where they apply. Saving a comment also projects it immediately as a
structured review attachment chip above the fixed composer, so it remains
visible from Agent and every workspace tab. Tab metadata may show the number of
drafts associated with that resource, such as `server.go 2` or `Diff 1`.

Each chip identifies its file and line or range plus a bounded comment preview.
Activating it opens or selects the corresponding File or Diff tab and reveals
the anchor. Editing the inline comment updates the same attachment and chip;
removing the chip removes the review attachment and inline saved comment through
the shared review controller. The controller, rather than either presentation,
owns their one-to-one identity and ordered submission state.

Starting review, choosing its target, navigating changed or skipped files, and
reviewing all drafts use bounded modal pickers. Submitting opens a modal summary
that identifies the target and the same ordered attachments before confirmation.
Closing a File or Diff tab does not discard its revision-pinned comments or
chips; reopening the resource restores their inline projections. Stale comments
remain visible inline and as warning-state chips and block submission according
to the review contract. Successful submission consumes the corresponding chips.
There is no Review descriptor or retained Review tab.

### Subagent picker and conversation tabs

The attached session's subagent roster is a modal status picker rather than a
retained tab:

```text
my session                                             GPT 5.6 · medium · 41%
──────────────────────────────────────────────────────────────────────────────
 Agent   server.go 2  [Diff 1 ×]  reviewer ⠋
──────────────────────────────────────────────────────────────────────────────
                 ┌ Open subagent ────────────────────────┐
                 │ Search subagents…                     │
                 ├───────────────────────────────────────┤
                 │ ⠋ reviewer       running             │
                 │ ✓ designer       completed           │
                 │   librarian      idle                │
                 │ ✕ security       failed              │
                 ├───────────────────────────────────────┤
                 │ enter open · esc cancel               │
                 └───────────────────────────────────────┘

──────────────────────────────────────────────────────────────────────────────
Ask about the visible workspace evidence…
──────────────────────────────────────────────────────────────────────────────
ready                                               ~/Developer/agent/kit (main)
```

Selecting a durable conversation creates or focuses its retained subagent tab.
Agent transcript activity, completion notifications, tool activity, and the
command palette may open the same identity directly. Merely starting or
activating a subagent does not create a tab: background work cannot mutate tab
order or fill the workspace without user navigation. Status remains available
through Agent activity, notifications, and the picker. There is no Subagents
roster descriptor or retained roster tab.

### Ownership and model boundaries

The native TUI has one retained workspace controller for its attached session
view. It owns Agent selection, an ordered set of secondary tabs, the active tab,
and content/composer focus ownership. Its state is independent of the immutable
session-client binding.

A pane descriptor is renderer-neutral plain data. It identifies the semantic
surface and the resource or evidence to open, for example a workspace
incarnation and path, a diff source identity, or a subagent conversation
identity. It contains no `ui.Widget`, render object, focus node, scroll
controller, callback, goroutine, or concrete server implementation. Descriptors
may cross client package boundaries, but they are not authoritative session
records and are not added to wire snapshots merely to restore TUI layout.

The native TUI owns an exhaustive pane-definition registry keyed by descriptor
kind. Each definition supplies:

- the stable identity used for deduplication;
- the concise tab label and collision disambiguation;
- close policy and runtime availability;
- activity or stale metadata that may decorate its tab;
- the Vaxis pane-body factory; and
- feature-specific projection from session-client data into the body.

Adding a descriptor kind without a native definition must fail at build or
contract-test time. Other renderers may consume equivalent descriptors, but do
not import native definitions or Vaxis rendering.

The workspace host owns the tab strip, overflow, modal pane picker, retention,
and surface-level focus routing. Pane bodies own their content, local
navigation, scroll controllers, contextual header and footer, drawers, feature
actions, loading and error states, and resource lifecycle. They do not add an
outer frame, repeat the tab title, or control global tab selection.

### Pane identity and opening

Identity describes the resource, not the action that opened it. Opening a
descriptor whose identity is already present selects that tab; it does not
append, remount, or reorder it. A descriptor update for the same identity
updates the mounted pane's inputs without replacing retained local state unless
the resource revision is incompatible.

Initial identities are:

- mutable file: attached session, workspace incarnation, and canonical
  workspace-relative path, regardless of whether it was opened from the file
  picker, search, Agent transcript, diff, or review;
- revision-pinned review file: authoritative review target identity plus
  canonical repository-relative path;
- diff: authoritative logical target identity, covering the current working
  tree or an explicit commit/branch review target;
- subagent conversation: the server's durable conversation identity.

A review target identity is issued by the review contract and includes its
repository and target kind. The pane receives revision changes as guarded input;
revision alone does not append a duplicate tab. Commit and branch review targets
therefore use ordinary review-scoped File and Diff tabs without introducing a
generic historical evidence pane. Transcript-recorded tool diffs and arbitrary
past snapshots do not receive retained tabs.

Open requests may carry a line, range, hunk, or changed-file anchor. The anchor
is navigation input, not part of file or diff identity. A repeated open updates
the existing pane's pending reveal target and focuses it.

Workspace-backed identities use the opaque workspace incarnation and canonical
relative paths supplied by `CORE-WORK-001`; display cwd is not an I/O or
identity authority. A cwd-incarnation change makes old workspace panes read-only
and stale while preserving their last loaded content and local position. They
cannot refresh, edit, or fetch more pages. Opening the same relative path then
creates a distinct pane for the new workspace incarnation.

### Ordering, selection, closing, and overflow

Agent is permanently first and is not closable. Secondary panes append in
first-open order. Selecting, revealing, deduplicating, resizing the terminal,
or restoring a hidden body never changes that order.

Exactly one surface is selected. Closing an inactive tab leaves selection
unchanged. Closing the active tab selects the tab now occupying the same index,
or the preceding final tab. Closing the last secondary tab selects Agent and
removes the strip.

The initial hard ceiling is 32 secondary tabs per attached view. Reopening an
existing identity still succeeds at the ceiling. Opening a new identity is
rejected with actionable feedback and access to the pane picker rather than
silently evicting retained state or a draft.

The host measures labels in terminal cells. It keeps Agent and the selected tab
visible where space permits, preserves canonical tab order, and represents
hidden entries with a labeled count such as `⋯ 3 more`. Activating overflow or
the pane-list command opens the shared modal picker in stable tab order. The
picker supports filtering, opening, and closing eligible tabs; unavailable
entries remain visible with a concise reason when doing so aids recovery.

### Retained lifecycle

Pane bodies mount lazily on first selection. Once mounted, they remain
reconciled under a stable outer widget while their tab exists, including across
tab selection and terminal resizing. This retains editor drafts, selection,
scroll, expanded tree nodes, hunk position, and other local state. Closing a tab
or disposing its attached session view unmounts the body and cancels pane-owned
work.

The host provides separate semantic signals for:

- **active**: this pane's tab is selected;
- **visible**: layout currently paints the pane; and
- **focused**: keyboard ownership is inside the pane.

Only the selected tab is visible. Hidden bodies receive zero/offstage layout,
are excluded from hit testing, selection traversal, and focus traversal, and
cannot install active keyboard handlers. They suspend polling, file watching,
animation, and expensive rendering unless a feature has a documented background
requirement. Asynchronous updates return through Vaxis runtime dispatch and are
generation-checked so a closed pane cannot update retained state.

Server events continue to update the attachment's client model while a pane is
hidden. A definition may derive a quiet changed, stale, running, or error marker
for its tab. Activity does not reorder, select, or focus a tab. The pane
reconciles current client data when selected.

### Responsive behavior

The same full-width tab model applies at every terminal width. There is no
responsive change in pane lifecycle or navigation semantics. Width changes only
affect label packing, overflow, wrapping, and feature-owned internal layout.

The root remains viewport-native: global header, tab/content region, composer,
and footer stay edge to edge. The tab strip uses one structural bottom
separator, selected-tab styling rather than pane frames, and concise activity
metadata. Pane headers and hint rows appear only when they add local scope, live
metadata, or actionable guidance.

### Focus and input routing

The controller records logical focus ownership between the selected content
surface and composer. Each pane remembers its last meaningful internal focus
target. Selecting a secondary tab restores that pane's valid remembered target
or its declared default. Selecting Agent restores the existing transcript focus
behavior. A primary-button action requests the selected surface's focus before
performing an action that assumes keyboard ownership.

Input follows visible layer and ownership:

1. the topmost modal picker or dialog;
2. the interaction dock presenting a server-owned pending interaction;
3. the focused control within the selected Agent/pane surface or composer;
4. tab select, close, and next/previous commands; and
5. global shell commands.

Handlers are registered only while their layer and pane are eligible. A hidden
pane cannot consume an event, and a conditional Vaxis action is omitted rather
than registered with an ignored result that would block an outer action.

`Tab` moves focus between the selected Agent/workspace content and the composer;
`Shift+Tab` performs the reverse transition. These bindings are part of the shell
contract rather than ordinary pane bindings. When a modal picker, dialog, or
interaction dock owns input, it intercepts the Vaxis focus intents and keeps
focus within that active layer. Previous/next tab selection remains a separate
intent whose default bindings are decided with the rest of the built-in keymap.
The modal pane picker traps focus and Escape dismisses it before any underlying
shell action.

### Client-local and server-owned state

The server owns session identity and cwd, canonical workspace paths and file
revisions, bounded file and directory data, revision-aware working-tree diffs,
review records and attachments, subagent roster/conversations/status, and user
settings intended to follow the user.

The TUI process owns open descriptors, tab IDs and order, selected tab, logical
and widget focus, scroll, selection, drawer expansion inside individual panes,
activity markers, and unsubmitted view-local edits. It derives these from
authoritative records but does not persist them as session events or shared
settings. There is no workspace ratio or other shell-layout preference to
persist. Drafts that must survive reconnect or client restart use their
feature's explicit server contract; mounted-widget retention is not durability.

A session client remains bound to one session. Switching sessions attaches the
replacement client and replaces the workspace controller and widget subtree as
one session-scoped view. The old subtree is disposed only after replacement is
ready or the switch is abandoned. The replacement starts with Agent selected
and no secondary panes. Kit does not cache or restore open panes, order,
selection, scroll, focus, or drafts when switching back to a session.

Session exploration and switching remain on-demand through the command palette
and session explorer. The workspace does not add a global session strip, rail,
running count, or process-global active-session state.

### Extension rules

New pane kinds use the same descriptor, registry, host, and lifecycle contract:

- the modal file picker consumes bounded server directory APIs and keeps its
  expansion, filtering, and focused-row navigation within the dialog lifecycle;
- file panes consume canonical revision-aware reads, share mutable-file identity
  across origins, and preserve independent view and annotation state;
- diff panes consume revision-aware working-tree changes, including agent
  edits, and keep layout and hunk selection locally;
- review is a cross-pane workflow: revision-pinned comments render inline in
  their File and Diff tabs, while target, changed-file, and submission flows use
  modal pickers rather than a Review tab; and
- the subagent roster is a modal status picker, while explicitly opened durable
  conversations each receive one retained tab.

A pane may request another descriptor to be opened but may not mutate host tab
state directly. Cross-pane opens return the selected tab identity and then use
the host focus request. Feature packages consume session-client interfaces and
do not import server, SQLite, or droids internals.

## Required properties and verification

Tests for the implementation must establish:

- the initial Agent-only layout with no redundant tab strip;
- exhaustive registry coverage and stable identity, label, availability, close,
  and activity-marker contracts;
- deduplication across every supported open origin for mutable and review-pinned
  File tabs and for working-tree, commit-review, and branch-review Diff tabs;
- deterministic append, selection, close, ceiling, and rejected-open behavior;
- exact tab, content, composer, separator, and footer geometry at representative
  wide and narrow widths, using cell-aware labels;
- selected-tab visibility, labeled overflow counts, and modal picker behavior;
- the existing transcript presentation remaining unchanged inside Agent;
- composer and attachment-shelf availability on Agent and secondary tabs, with
  `Tab` and `Shift+Tab` moving focus deterministically between selected content
  and composer;
- one-to-one inline review comments and structured attachment chips, including
  immediate add/edit/remove projection, anchor navigation, stale warning state,
  ordered submission, and consumption after success;
- preservation of pane-local state across tab switches and resizing, and
  disposal on close or session replacement;
- focus restoration, mouse focus requests, overlay precedence, and proof that
  hidden panes receive no keyboard or mouse events;
- hidden-pane activity markers without ordering, focus, or selection theft;
- cwd-incarnation changes preserving frozen old content while refusing further
  reads, with the same relative path opening a distinct new pane;
- file-picker directory refresh replacing revision-keyed observations while
  preserving valid path-keyed expansion and selection within the dialog;
- session switching starting a fresh Agent-only workspace without state leakage
  or persistent global navigation; and
- viewport-native rows and cells without a root frame or duplicate pane chrome.

Presentation tests assert visible cells, styles, geometry, focus, and state
transitions. Controller tests assert identities and lifecycle independently of
Vaxis rendering.

## Consequences

Files, diffs, and subagent conversations receive the full terminal
width, and the workspace has one information architecture at every viewport
size. Agent remains immediately available as the first surface, while the fixed
composer and attachment shelf let users act on visible workspace evidence and
keep review comments visible across tabs. Removing split, drawer,
collapse, and ratio state substantially reduces layout and focus complexity.

The design does not show transcript and workspace content simultaneously. Users
switch to Agent when they need conversation context, and hidden panes can only
communicate bounded activity through their tabs or the modal picker. Many open
tabs require overflow navigation, making the quality and discoverability of the
picker important.

Retained panes still consume memory, so the tab ceiling and feature-local cache
bounds are required. Navigation-only directory browsing stays in a bounded
modal instead of stretching a sparse tree across a full-width retained tab.

## Related

- [ADR 0001: Native Go application architecture](0001-native-go-architecture.md)
- [Native TUI roadmap](../roadmap/tui.md)
- [Core and protocol roadmap](../roadmap/core.md)
- [V2 UI direction](../design/v2-ui-direction.md)
- [TUI tool activity presentation](../design/tui-tool-activity.md)
