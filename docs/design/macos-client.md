# macOS client implementation reference

This document records integration constraints and lifecycle decisions for the
native client. Presentation belongs in [the design language](macos-design-language.md),
architecture in [ADR 0013](../adrs/0013-native-macos-client.md), and unfinished
capabilities in [the macOS backlog](../../backlog/macos.md).

## State and transport

`AppModel` owns connections. `SessionStore` coordinates the authoritative
`SessionReplica`, session-local `SessionUIState`, and injected offline test state.
`SessionClient` separates transport from presentation. Wire snapshots and ordered
SSE events project into native models before views consume them. Subagents share
the transcript projection and renderer with parent sessions.

Connections reject redirects and non-loopback discovery URLs. Tokens remain in
memory and never appear in URLs or user-facing errors. Reconnect, event gaps, and
resynchronization acquire a new snapshot and stream cursor. Cancelling a client
subscription does not abort the server run. Response application checks server,
session, attachment generation, and working directory where relevant.

Prompt acknowledgements clear the submitted draft. Ambiguous failures trigger
read-only resynchronization, preserve the draft, and let the user decide whether
to send again. The client never automatically resubmits prompts. Queue restoration
returns all queued messages to the composer together; individual queue editing
and deletion are unsupported.

## Transcript rendering

`NativeTranscript` uses reusable NSTableView rows hosting SwiftUI content. History
starts with the server-selected snapshot window and prepends server-paginated
history. There is no client-side fixed 40-message initial limit.

SwiftUI reports unconstrained row height at the current width. Deferred updates
validate cell identity and width generation, coalesce row changes, and disable
implicit layout animations. Width and typography changes invalidate measurements.
Unpinned updates preserve the visible message and inset; pinned updates follow
the bottom. Wheel input unpins before row layout, and unchanged view updates do
not issue scroll commands. Nested code/output scrollers hand vertical wheel input
to their ancestor at boundaries. History anchoring survives deferred row heights.

Sessions initially open at the bottom. Latest appears only when the final message
is entirely outside the viewport. Tool-backed thinking and tool activity share
one drawer; the latest thinking line also has a slot above the composer. Assistant
prose appears on completion. Active drawer following respects manual scrolling.

## Editors and highlighting

Swift Markdown parses blocks and inline content. SwiftTreeSitter highlights code
fences separately from the file editor, caching theme-independent captures.
Unsupported languages, parser failures, and oversized blocks retain complete
plain text. CodeEditLanguages supplies the shared compiled grammars and queries,
avoiding duplicate grammar symbols. Resource bundles ship inside the app.

CodeEditSourceEditor is behind a Kit-owned adapter. Its String binding seeds text
and publishes edits but does not apply arbitrary external replacements. The
read-only adapter replaces content identity while preserving external selection
and scroll state, clamping selections to the new document. Editable server-backed
files require explicit text-storage, undo, conflict, and selection policies.

The pinned editor's stock theme mapping combines function and variable captures;
operators and punctuation may use base text. Full Kit syntax-palette parity needs
an upstream extension or maintained adaptation. Its fixed language catalog does
not expose arbitrary grammar registration. Use explicit sRGB fallback colors:
the dependency's minimap color handling assumes a convertible color space even
when hidden. Xcode builds the CodeEditSymbols asset resources required by the
editor; plain SwiftPM CLI builds do not provide the same resource path.

## Files and working directories

File mentions and the file picker share a session/cwd-scoped index cache and
in-flight request. The picker filters directories out; mentions retain them.
Consumers share cancellation ownership. Truncated indexes display an incomplete
index notice. Refresh bypasses the cache, and obsolete responses cannot replace
newer index state.

File content comes from the server workspace read contract, never the client's
filesystem. The bounded preview cache retains old content on refresh failure,
marks stale revisions, and preserves selection/scroll through content updates.
Directory changes invalidate file panes and caches while preserving composer and
scratchpad text. Cwd mutations retain their operation identity across uncertain
retries so relative paths are not applied twice. After acknowledgement, retries
only refresh workspace state.

Repository metadata also comes from the server. Activity coalesces VCS refreshes;
prose deltas do not cause reads. Missing repositories or metadata failures leave
the cwd visible. The footer shows branch or detached identity and the dirty marker.

## Session operations

Reload reads agent sources on the server and returns selectable warnings and
source diagnostics. It resynchronizes after success or failure. Refreshing an
acknowledged result retries the read, not the mutation. Cwd change and reload are
separate operations.

Explicit compaction keeps an operation identity through recovery and refreshes
the snapshot/stream afterwards. Its pending state belongs in the footer, with no
secondary modal. Automatic compaction events are matched to their current identity.

Prompt-template completion uses server metadata and submits arguments unchanged
to the prompt-command endpoint. Expansion happens on the server. Prompt commands
are idle-only and do not accept attachments/review notes through that endpoint.

## Direct shell execution

`!command` includes the execution in model context; `!!command` excludes it. The
composer hides that prefix and highlights the command as Bash. Escape leaves
shell mode with text preserved. Backspace at the start changes excluded to
included. Enter executes; Cmd+Enter adds a line. Attachments remain for the next
message. Output is plain monospace text, not Bash-highlighted source.

The server runs commands in the session cwd. A client-generated execution ID is
retained through uncertain admission; lookup resolves uncertainty and explicit
retries reuse the ID. Stop targets that execution and waits for authoritative
terminal status. Snapshot polling discovers active shell work, and bounded local
results merge without replacing newer assistant content.

The current shell contract has no lifecycle SSE events or execution-list API.
An excluded execution completed in another client between polls cannot be
discovered or restored after app restart. Included results recover from context
boundaries. Closing an attachment cancels its polling, not server execution.

## Subagents and cross-session activity

Subagent inspection is read-only. Direct user-to-subagent messaging stays disabled
by product decision; dismissal is supported. Roster status drives tab activity,
and effective configuration belongs to the selected subagent conversation.

Created-session and peer-session tool cards present structured summaries and
canonical navigation targets, not raw JSON or internal request IDs. Links retain
the originating server identity and focus already-open sessions. Recorded initial
run IDs do not establish current run status, and peer wait timeouts do not imply
cancellation. Standalone peer-query tracking is tracked by `MAC-PEER-002` in the
[native macOS backlog](../../backlog/macos.md).

`peer_query`, `peer_result`, and `subagent_result` context records are intentionally
hidden from the transcript until a dedicated presentation is chosen.

## File annotations

Protocol 34 annotation operations use the existing session annotation capability.
`FileAnnotation` projects bounded summaries, full records, and immutable accepted
snapshots. `AnnotationState` owns unsaved comment text and mutation recovery per
session. Ordered events update drafts; submitted events refresh the snapshot to
retrieve frozen message evidence because `message.user` carries only a summary.
Failures preserve comment text and perform read-only reconciliation, with no
implicit mutation retries. Stale replacements create first and delete the old ID
only after a definite creation acknowledgement.

The read-only `AnnotatedFileEditor` hosts native note/input views in vertical
layout gaps after their source ranges. A vendored CodeEditTextView 0.12.1
adaptation adds per-line block heights, a post-layout callback, and subview hit
testing for interactive blocks; source storage, UTF-16 offsets, syntax fragments,
and logical line numbers stay intact.
CodeEditSourceEditor 0.15.2 uses a local dependency manifest so the same adapted
text view serves the entire app. Its minimap hit testing respects hidden views,
preventing the disabled minimap from intercepting inline controls. Both retain their
upstream licenses and pinned revisions in `Vendor/*/KIT-ADAPTATION.md`.

The coordinator owns hosted views, a gutter overlay, and scroll observation.
The overlay tracks row hover across the viewport, handles clicks and bidirectional
range drags only in the gutter, and scrolls at viewport edges during a drag. It
excludes inline layout gaps and the trailing empty line from source anchors.
Its teardown removes tracking, timers, observers, and callbacks. View widths and note
heights follow the native viewport; source selection crosses note gaps without
including comment text. Chip reveal uses the retained file tab, scrolls to the
note, and briefly highlights it. No block is attached to a different revision.
Stale evidence is available through an inline warning or chip in a bounded sheet;
replacement retains the body until a new range is saved successfully.

`AnnotationDesignPreview` is the isolated design specimen; rendered
`InlineAnnotationEditorTests` cover the production adapter in both appearances.
Live temporary-session checks cover CRUD, stale previews, and cross-client SSE.
The user manually verified annotation-only prompt acceptance on September 18, 2026.
Automated agent-run acceptance verification is separately opt-in through
`KIT_ANNOTATION_SEND_LIVE_TEST`; it requires explicit authorization to start a run.

Diff annotation projections retain the source side, target ID, target revision,
file revision, and optional pinned committed endpoints in live and submitted
evidence. Their chips reveal the guarded diff using annotation-authorized reconstruction,
falling back to captured evidence if that revision is unavailable. Deferred evidence validation is displayed separately
from stale state; the server remains responsible for validation at submission.
The retained Diff pane reads the target catalog and accepts only server-issued
working-tree, branch, and commit references. Changed files and semantic hunks
load in bounded pages; continuations carry the exact target/file revisions.
Files appear as individually titled sections in one continuous vertical scroll
surface, with addition/deletion counts and a Files menu that jumps to a section.
Visible sections load on demand through serialized reads and retain independent
line pagination. Each native editor sizes to its source and inline-note height;
vertical wheel events pass to the containing diff scroll view.
The header offers Unified/Split layout and line wrapping controls, retained by the
workspace across target changes. Split mode pairs replacement runs and leaves
unmatched rows blank without assigning them source coordinates. Both native
editors share row heights so wrapped source and inline notes stay aligned.
Comments remain pinned to their original source side; switching presentation
preserves an unsaved draft.
The pane shows explicit loading, empty, partial, nontext, complex, stale, and
failure states. Visible diff panes poll every 30 seconds, deferring while a comment is being edited or saved. Unchanged revisions retain expanded pages and editor position; hidden panes do not poll. Workspace invalidation discards in-flight
results, and operation generations prevent older reads from replacing new state.

The native editor shares annotation inputs and notes with file tabs. A semantic
old/new gutter projects source-side coordinates independently of displayed rows,
hunk headers, continuation fragments, and inline note gaps. Deletions anchor the
old side, additions the new side, and context accepts either number column.
The hover comment button sits at the code-facing edge of the gutter and uses a
pointing-hand cursor.
Ranges cannot span omitted context or exceed 200 source lines. Ordinary native
selection, syntax highlighting, scrolling, and source-copy behavior are retained.
Refreshing preserves unsaved comments at their original revisions and requires
explicit reselection. Saved notes use the shared composer submission contract.

`DiffTests` cover source-side projection, pinned pagination, cancelled/stale reads,
and annotation-authorized reveal. `InlineAnnotationEditorTests` exercise the rendered native
rail and inline editor in both appearances. `KIT_DIFF_LIVE_TEST=1` opts into a
disposable Git repository/session test for all three target kinds, paging, stale
reads, and annotation CRUD; this check never starts an agent run.
