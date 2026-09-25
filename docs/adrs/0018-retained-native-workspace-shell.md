# 0018: Retain native workspace panes in full-width tabs

## Status

Accepted

## Context

The native TUI needs one workspace model for secondary surfaces without
turning each feature into a separate full-screen workflow. The agent transcript
remains Kit's primary narrative, while explicitly opened subagent conversations
must survive tab changes and terminal resizing.

Agent and subagent conversations benefit from the terminal's complete width. A
permanent transcript-and-pane split makes both surfaces narrow, introduces a
second narrow-mode layout, and requires resize and collapse state that does not
contribute to the underlying task.

The server is authoritative for sessions and subagent execution. Pane
arrangement, focus, scroll, and selection are properties of one TUI attachment. Conflating these layers would
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
tab is selected. Its attachment shelf sits immediately above it on every tab.
This allows the user to discuss visible secondary content without first
returning to Agent.
A server-owned pending interaction is presented by the existing renderer-owned
interaction dock in that same composer region.

The initial workspace has no secondary panes. While Agent is the only surface,
the tab strip is omitted and the transcript uses the full content region.
Opening the first secondary pane reveals the strip and selects that pane.
Closing the final secondary pane removes the strip and returns to Agent.

There is no transcript-and-pane split, partial drawer, collapsed workspace rail,
wide/narrow mode switch, draggable workspace divider, or workspace ratio. Every
selected surface receives the full content width at every terminal size.

### Representative layouts

With no secondary panes open, the tab strip is omitted and Agent retains the
entire content region:

```text
my session                                             GPT 5.6 · medium · 41%
──────────────────────────────────────────────────────────────────────────────

                         existing Agent transcript

──────────────────────────────────────────────────────────────────────────────
Ask Kit anything…
──────────────────────────────────────────────────────────────────────────────
ready                                               ~/Developer/agent/kit (main)
```

Opening a durable subagent conversation introduces the strip while preserving
the full-width content and fixed composer:

```text
my session                                             GPT 5.6 · medium · 41%
──────────────────────────────────────────────────────────────────────────────
 Agent  [reviewer ⠋ ×]
──────────────────────────────────────────────────────────────────────────────

                    retained reviewer conversation

──────────────────────────────────────────────────────────────────────────────
Ask about the visible workspace evidence…
──────────────────────────────────────────────────────────────────────────────
ready                                               ~/Developer/agent/kit (main)
```

At narrow widths, Agent and the selected tab remain visible while undisplayed
tabs move behind labeled overflow. Activating overflow or the `tabs` command
opens Kit's standard picker modal for filtering, selecting, and closing tabs.

### Subagent picker and conversation tabs

The attached session's subagent roster is a modal status picker rather than a
retained tab:

```text
my session                                             GPT 5.6 · medium · 41%
──────────────────────────────────────────────────────────────────────────────
 Agent   [reviewer ⠋ ×]  designer
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
The Subagents picker and Agent tool activity may open the same identity
directly, while the command palette provides access to the picker. Merely
starting or
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
surface and the resource or evidence to open, such as a durable subagent
conversation identity. It contains no `ui.Widget`, render object, focus node,
scroll controller, callback, goroutine, or concrete server implementation.
Descriptors may cross client package boundaries, but they are not authoritative
session records and are not added to wire snapshots merely to restore TUI
layout.

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
updates the mounted pane's inputs without replacing retained local state.

The accepted native pane kind is a subagent conversation, identified by the
server's durable conversation identity.

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

Pane bodies mount when their tab opens and remain reconciled under a stable
outer widget while that tab exists, including across tab selection and terminal
resizing. This retains selection, scroll, focus, and other pane-local state.
Closing a tab or disposing its attached session view unmounts the body and
cancels pane-owned work.

The host provides separate semantic signals for:

- **active**: this pane's tab is selected;
- **visible**: layout currently paints the pane; and
- **focused**: keyboard ownership is inside the pane.

Only the selected tab is visible. Hidden bodies sit behind a zero-sized
offstage wrapper, receive no layout updates, are excluded from hit testing,
selection traversal, and focus traversal, and cannot install active keyboard
handlers. Pane-specific polling and observation are limited to the visible pane
unless a feature has a documented background requirement. Retained hidden
widgets may still perform lightweight reconciliation or animation while
mounted; suspending that work is a separate optimization. Asynchronous updates
return through Vaxis runtime dispatch and are generation-checked so a closed
pane cannot update retained state.

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

Pane-local dialogs participate in the same admission and ownership rules as
shell dialogs. Their ownership is scoped to the attached session, workspace,
selected pane, and pane incarnation. Only explicitly supported child dialogs
may nest. An interaction arriving beneath a modal waits without taking focus;
closing the modal hands focus to the pending interaction before restoring the
underlying control. Mounted editors retain their cursor and selection, and
return addresses are validated before restoring focus. Invalid return targets
fall back to the composer.

Bracketed paste belongs to its originating session, owner incarnation, and
control. Ownership changes discard it rather than redirecting it. Paste is text
insertion only, never a navigation, submission, or dismissal command. A rendered
target that no longer owns input cannot receive stale keys or paste; after a
background text selection temporarily captures native focus, the interaction
dock reclaims focus on the next frame. Background selection and scrolling remain
available without granting keyboard ownership.

`Ctrl+C` clears text and staged attachments when the non-empty composer owns
input. Otherwise it quits/detaches the client without cancelling server-owned
work, interactions, or annotations. A draft behind another input owner is not
cleared. Key releases and repeats do not trigger another action. Escape cancels
only the innermost reversible operation; pending non-cancellable work consumes
it.

`Tab` moves focus between the selected Agent/workspace content and the composer;
`Shift+Tab` performs the reverse transition. These bindings are part of the shell
contract rather than ordinary pane bindings. When a modal picker, dialog, or
interaction dock owns input, it intercepts the Vaxis focus intents and keeps
focus within that active layer. `Ctrl+[` and `Ctrl+]` select the previous and
next workspace tab, wrapping through Agent and retained tabs. The modal pane
picker traps these commands, focus movement, and Escape before any underlying
shell action.

### Client-local and server-owned state

The server owns session identity and cwd, subagent roster, conversations and
status, and user settings intended to follow the user.

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
selection, scroll, focus, or pane-local edits when switching back to a session.

Session exploration and switching remain on-demand through the command palette
and session explorer. The workspace does not add a global session strip, rail,
running count, or process-global active-session state.

### Extension rules

Additional pane kinds require a future accepted decision and must use the same
descriptor, exhaustive registry, host, and lifecycle contract. A pane may
request another descriptor to be opened but may not mutate host tab state
directly. Feature packages consume session-client interfaces and do not import
server, SQLite, or droids internals.

## Required properties and verification

Tests for the accepted implementation establish:

- the initial Agent-only layout with no redundant tab strip;
- exhaustive registry coverage and stable identity, label, availability,
  close, and activity-marker contracts for supported pane kinds;
- deterministic deduplication, append, selection, close, ceiling, and rejected
  open behavior;
- exact tab, content, composer, separator, and footer geometry at representative
  wide and narrow widths, using cell-aware labels;
- selected-tab visibility, labeled overflow counts, and standard modal picker
  behavior;
- the existing transcript presentation remaining unchanged inside Agent;
- composer availability on Agent and secondary tabs, with `Tab` and
  `Shift+Tab` moving focus deterministically between content and composer;
- preservation of pane-local state across tab switches and resizing, plus
  disposal on close or session replacement;
- focus restoration, mouse focus requests, overlay precedence, and proof that
  hidden panes receive no keyboard or mouse events or pane-specific polling;
- hidden-pane activity markers without ordering, focus, or selection theft;
- session switching starting a fresh Agent-only workspace without state
  leakage or persistent global navigation; and
- viewport-native rows and cells without a root frame or duplicate pane chrome.

Presentation tests assert visible cells, styles, geometry, focus, and state
transitions. Controller tests assert identities and lifecycle independently of
Vaxis rendering.

## Consequences

Agent remains immediately available as the first surface, while explicitly
opened subagent conversations receive the full terminal width and retain local
state across navigation and resizing. Removing split, drawer, collapse, and
ratio state substantially reduces layout and focus complexity.

The design does not show transcript and secondary content simultaneously.
Users switch to Agent when they need parent conversation context, and hidden
panes communicate bounded activity through their tabs or the modal picker.
Many open tabs require overflow navigation, so the picker and the 32-tab ceiling
remain part of the shell contract.

## Related

- [ADR 0001: Native Go application architecture](0001-native-go-architecture.md)
- [ADR 0020: Add file, diff, and review workspace
  surfaces](0020-file-diff-review-workspace-surfaces.md)
- [Native TUI backlog](../../backlog/tui.md)
- [Core and protocol backlog](../../backlog/core.md)
- [V2 UI direction](../design/v2-ui-direction.md)
- [TUI tool activity presentation](../design/tui-tool-activity.md)
