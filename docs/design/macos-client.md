# macOS client implementation reference

This document records integration constraints, interaction behavior, and
lifecycle decisions for the native client. Visual design belongs to the
[Paper "Kit App" file](https://app.paper.design/file/01M4MCMMAEY5AWN7E5J0PK6X1K): its notes, app flow, themes, tokens, and
explorations. Architecture belongs in [ADR 0013](../adrs/0013-native-macos-client.md)
and [ADR 0044](../adrs/0044-tile-the-macos-session-workspace.md), and unfinished
capabilities in [`macos` issues](https://github.com/akonwi/kit/issues?q=is%3Aopen+label%3Amacos).

## Interaction behavior

These rules cover behavior that the Paper designs do not record.

### Launcher and relaunch

When a window has no open session, it shows a session launcher instead of
opening the latest session. The launcher connects in the background and lists
the window's sessions that are working or awaiting a response under Active,
then the rest under Recent with their folders and how long ago they were
active. Search, arrow keys, and Return open a session without a pointer; ⌘R and
⌘⌫ rename and delete the highlighted one. A server with no sessions shows only
the way to start the first one. The transcript, composer, and workspace panes
stay hidden until a session is explicitly chosen.

New session opens a form over the whole window. It starts in the home folder,
with the default model, or the most recent session's when there is none, at
medium thinking when the model offers it. Model and Thinking open the
composer's popovers below their fields; a model that lacks the chosen level
falls back the same way. Return creates and Esc cancels. While the server
answers the fields lock, and a temporary session the server didn't confirm
must be disposed of before the form closes. When the server offers no models,
the form says to sign in to a provider and links to Settings › Providers.

### First run and providers

While the server has no provider connected, each window shows first run in
place of its tabs: Kit's wordmark over one row per sign-in method, browser
sign-ins last. Choosing one replaces the list with its step in the same column:
a device code to copy and open, a browser page to approve (or a code to paste
back), or an API key. Every step ends on Connected, which picks the default
model for new sessions; Continue opens the workspace and Add another returns to
the list. Once a provider is connected, its rows read Connected and a Continue
below the list opens the workspace; choosing a connected row signs in again. Back cancels a sign-in the server is still waiting on. First run
shows even when sessions exist, so a server whose credentials were removed
asks for one again.

Settings › Providers lists each provider once with its status. Sign in replaces
the list with the same steps; Sign out removes the stored credential. A key
from the server's environment shows as connected without Sign out, since the
app can't remove it. The `login` command opens this section.

Relaunch restores only intentionally opened sessions: each window's session tabs,
its selected session, each tab's tile arrangement, and the sidebar's collapsed
families. Closing a session removes
it, and its arrangement, from restoration intent; quitting keeps the open
sessions for the next launch. Layout changes are recorded half a second after
they settle, and immediately when the app quits.

A restored arrangement is repaired rather than rejected. Panes the app no longer
knows are dropped, a tile left without tabs collapses into its sibling, the
Agent tab returns to the first tile if missing, ratios are clamped, and zoom
needs more than one tile. Arrangements are stored as an optional field of the
version 2 window manifest, so an earlier build still restores windows and tabs.

### Session tabs

The sidebar lists a window's session tabs as a tree. Each session sits under the
session it came from, whether a fork or an agent's work, so a family reads as
one unit. Families keep the order of their tabs: a family sits where its
earliest tab is, and siblings follow the same rule. Launcher tabs follow every
family. The working directory isn't in the sidebar; the session bar shows it.

- A parent whose tab is closed stays as a dimmed row while any child is open.
  Choosing it opens its tab again, or focuses the window that has it. The row
  leaves once its last open child closes. Closed ancestors between an open tab
  and its nearest open ancestor appear the same way. A session whose parent the
  catalog doesn't know, as when the parent was deleted, sits at the top level.
- A caret after the title collapses a family. A collapsed row shows how many
  open tabs it hides and the most urgent of their markers: input, then work,
  then a finished turn. Hovering adds the close button after the caret.
- Selecting a hidden tab, by shortcut, palette, or link, expands its family.
- Dragging a row moves it, with its descendants, among its siblings; at the top
  level that moves whole families. A line marks where it will land, and Esc
  cancels the drag.
- ⌘1–⌘9 follow the visible rows, so the number matches the row's position. On
  a dimmed parent it reopens the tab, as choosing the row does. Next/previous
  follow the visible tab rows, skipping dimmed parents and collapsed children.
  With top tabs, both follow the strip.
- Each level indents 32 pt, so a child's row starts at its parent's title and
  the family's rail runs under that title. Indentation stops after three
  levels; deeper sessions share the third level's rail.
- Top tabs stay flat, in family order. A tab's tooltip names its parent.
- Closing the selected tab selects the next tab in family order. A family keeps
  its place when its parent closes.

### Composer

The composer starts with one text line and grows with its content, with no
internal scroll container or arbitrary height cap. The send and stop control
keeps a fixed size at the bottom right, and Cmd+Return stays supported without a
label beside it. The placeholder and typed text use the same font and text
origin. Height is measured from the editor's own text layout rather than
approximated with a separate SwiftUI text view, and native selection,
insertion, multiline editing, and keyboard behavior are preserved. Very long
drafts still need a layout policy that keeps all content and actions reachable
without an internal scrollbar.

The controls row holds one bordered chip on the left and send or stop on the
right. The chip is split by a hairline into the model and the thinking level; a
model without thinking levels shows only the model. Files are attached only by
dropping or pasting them; there is no attach button. Context usage is shown in
session details, not in the composer. In shell mode the chip gives way to the
shell controls.

Each half opens its own popover 6 pt above it, aligned to its left edge, with
no arrow. The model half lists the session's models; while a turn runs they
are dimmed under "Model can change when the turn ends". The thinking half lists
the model's levels under a THINKING caption, the current one checked. Up and
Down move, Return picks, and a pick applies at once and closes the popover. Esc
or a click outside closes it without a change.

### Pickers and overlays

The command palette and other pickers take keyboard focus when opened, support
navigation and dismissal, and keep background controls from accepting input.
Closing a picker returns focus to the editor or tile that had it, unless the
picker's action moved focus itself: a mention returns to the composer, and an
opened file takes focus in its tile.

### Command palette

The palette opens with ⌘P, or by typing / into an empty composer. It sits
a quarter of the window's height below its top, like the TUI's pickers,
centred on the window, with no scrim, so the
workspace stays readable behind it. It lists the TUI's catalog (built-in,
prompt, and plugin commands) plus the Mac's browser and tile commands, sorted
by name, in the TUI's row layout: monospace name, argument hint, description,
and the prompt's source or the command's plugin on the right. Plugin commands
show their name within the plugin. A command that can't run now stays listed,
dimmed, with its reason before the description, such as "idle only" while a
turn runs; choosing it shows the reason as a toast.

Matching follows the TUI: the command word is ranked against names first, then
descriptions and aliases, so typing arguments never changes the selection. A
leading / is ignored. When the list overflows, the last visible row is cut in
half to show that it scrolls; there is no "more" row. When nothing matches, the
list area shows a centred message naming the query, and the card keeps its
height.

Tab completes the selected command into the search field, where its arguments
are typed inline and Return runs it. `cd path`, `name title`, and
`fork message` run directly, and their form stays open to show progress or a
failure to retry; without an argument they open the form. A fork that finishes
after the palette closes offers to open it from a toast. Prompt and plugin
commands receive their arguments literally. `model`, `thinking`, `tabs`, and
`theme` open a list in the same style, and `debug`, `mcp`, `sessions`, and
`subagents` open nested screens with a 48 pt title row. Esc closes the palette
from any screen. The footer shows only the tab complete hint.

`theme` picks the theme for the appearance in use from every theme: Kit
dark, Kit light, then each installed theme, re-read from `$KIT_HOME/themes` each time
the list opens. Moving through the list
previews each theme on the whole app; Return saves it for that mode only,
and Esc or clicking away restores the saved theme. Settings › Appearance
still sets both modes.

### Toasts

Toasts report outcomes, warnings, and errors that aren't part of the
conversation. They float at the top right of the workspace, 12 pt below the
session bar and in from the right edge, over whatever tile is there, so they
don't move when tiles split or resize. Each session has its own stack.

A toast is a 360 pt surface card with a 1 pt border in its tone's colour,
4 pt corners, and a soft shadow. Its icon and medium-weight title use the
tone's colour: accent for info, warning, and danger. A plugin's id sits beside
the title. The detail is muted and clamped to two lines; Details appears only
when the text runs past them, and opens it in place, scrolling past 220 pt.
A reload report's Details widens its card to 560 pt. Actions are 22 pt
bordered buttons, and choosing one also dismisses the toast.

Every toast has ×. Toasts that need acknowledgement stay until it's clicked;
the rest also expire after 10 s. Expiry pauses while the pointer is over the
stack, while a toast has focus, and while the window is inactive. Up to four
toasts stack newest first, 8 pt apart. From five, the newest shows with two
card edges below it and an "N more" button that lists them all, with
Collapse to fold them again; expiry keeps running underneath. Clicking a
toast's body does nothing, so its text can be selected and copied.

### Tile focus

Focusing a tile, by its tab strip, a command, or a dropped tab, moves the
keyboard to its selected pane: the Agent composer, or the Scratchpad and file
editors. Panes without one, such as Diff, only take the keyboard
away from other tiles and hidden tabs. Clicking into a tile's composer,
editor, or transcript focuses that tile. A request to focus the Agent
composer while its tab is hidden waits until the tab is shown; quoting a
selection into it shows the tab.

Settings › Workspace has **Focus follows the pointer**, off by default. When
on, moving the pointer into a tile focuses it as a click would, moving the
keyboard there too. Only pointer movement counts: a tile that moves under a
still pointer, as when tiles split, close, or resize, doesn't take focus, and
dragging with a button held doesn't either. The pointer doesn't focus tiles
while the palette or file picker is open, a sheet is up, a tab is being
dragged, or the window isn't key.

Hidden tabs stay mounted at zero opacity with hit testing off, so switching
tabs never remounts an editor. SwiftUI keeps a hidden AppKit view out of hit
testing but leaves it in the view tree and able to become first responder, so
the focus bridge moves the keyboard away from it. Views that must also stop
drawing, such as terminals, are detached from hidden hosts instead.

### Tile commands

The Tile menu, the command palette, and keyboard shortcuts act on the focused
tile and its selected tab. Shortcuts follow Ghostty's split bindings where they
overlap.

| Command | Shortcut |
| --- | --- |
| Split right / down | ⌘D / ⇧⌘D |
| Zoom or unzoom the focused tile | ⇧⌘↩ |
| Equalize tiles | ⌃⌘= |
| Focus the tile left, right, up, or down | ⌥⌘ + arrow |
| Move the selected tab to the neighboring tile | ⇧⌥⌘ + arrow |
| Move the nearest divider toward the arrow | ⌃⌘ + arrow |
| Previous / next tab in the tile | ⌃⇧Tab / ⌃Tab |
| Close the selected tab | ⌘W |
| Close the session tab | ⇧⌘W |
| Open file | ⌘O |
| Command palette | ⌘P |

Focus moves by on-screen position, not tree order, and ends a zoom. Moving a
tab past the workspace's edge splits it into a new tile on that side, if its
tile has other tabs. A keyboard resize moves 40 pt and stops before a tile
would shrink below 160 × 120 pt. The Agent tab can't close, so ⌘W on it
closes the session tab. Esc cancels a tab drag.

There is no footer below the tiles. Status that the footer showed (compaction,
provider retry countdown, replay, local notices) appears in the Agent pane's
activity card. Diff, Scratchpad, files, and the subagent list open from the
Tile and Session menus and the command palette. The + after each tile's tabs
opens a menu of panes to add to that tile: Browser, Diff, Scratchpad, then
Open file…. Panes already open are dimmed, and choosing one selects it.

The activity card shows one line, chosen in this order: a failed stop, a failed
turn start, a provider retry with its live countdown and attempt, compaction,
the live activity (latest thinking line, or "Working…"), then a local notice.
Failures appear in red with a Retry button; local composer validation stays at
the composer. The card has no thinking panel: the complete reasoning is in the
transcript's tool drawers.

### Browser tabs

A browser tab shows a web page in a tile, for checking a development server or
reading a linked page beside the conversation. It is a tile tab like any other.
It opens from the tile + menu (blank, with the address field focused), the
palette's `browser [url]`, and clicking a web link in the transcript, which
opens it beside the transcript's tile; ⌘-clicking a link uses the default
browser instead. There is no shortcut.

The toolbar under the tab strip has back, forward, reload (stop while
loading), the address field, and Open in Default Browser. A 2 pt accent line
along the toolbar's bottom edge shows load progress. At rest the field shows
the host and port, then the rest of the address in muted text, without the
scheme; editing shows the whole address, selected. The field takes addresses
only and never searches: a host without a scheme gets http for localhost,
`*.localhost`, loopback addresses, and `*.local`, and https otherwise. Other
text shows Kit's message in the pane, "Can't open ‘text’" / "That isn't a
web address." A page that can't load shows "Can't reach host" with the
engine's reason and Try again, and the tab is titled with the host. The tab
shows the site's favicon and page title, capped at 180 pt; the browser icon
and host stand in until they load.

The page draws with its own colours. A link that opens a window opens a new
browser tab after its opener in the same tile, keeping its opener, and a page
that closes its window closes its tab. Login prompts, page alerts, confirms,
prompts, and file choosers use standard macOS sheets on the window; page
dialogs offer "Don't allow more dialogs from this page" until the next page
loads, and a page that isn't on screen gets none (alerts are dismissed,
confirms and logins cancelled). A download starts only when the person asked
for it: within 5 s of clicking or typing in the page, as a link click or form
submission does, or from a navigation started with Kit's own controls. Others are blocked with a warning toast
naming the site. Downloads save to `~/Downloads`, numbering a
name already taken ("report 2.pdf"), and are quarantined as web downloads; a
finished download shows the info toast with Show in Finder, and a failed one a
persistent error toast. A clicked mail, phone, or app link asks before opening
the app that handles it; pages can't open other apps on their own. Site icons
are fetched over http(s) only, size-limited, and decoded at tab size. A page
whose process ends reloads once, then shows the failure with Try again.
Saved and listed addresses leave out any user name and password.

Each page stays alive while its tab is open, including in hidden tabs, moved
tabs, and session tabs that aren't shown: one live view per tab moves into
whichever host shows it, so switching never reloads the page. Focusing a
browser tab's tile gives the page the keyboard, or the address field for a
blank tab or one showing a failure. Relaunch restores each browser tab at its last address, without
back history; a restored page loads when its session tab is first shown.
Cookies and site data persist across launches in Kit's own store, separate
from Safari's. Browser tabs work the same with remote servers, since pages
load on the Mac. Only `Sources/Kit/Browser/WebKitBrowserEngine.swift` uses
WebKit; the rest of the app uses the `BrowserEngine` interface.

Find in page (⌘F) and page zoom (⌘+, ⌘−, ⌘0) are drawn on Paper ("Explore ·
browser tab states") and not built yet.

### Appearance and themes

Appearance is **System**, **Light**, or **Dark**, with a theme assigned to Light
and another to Dark. System follows macOS and activates the matching
assignment; forced modes use their assignment consistently. Settings previews
both assignments so the pair can be judged without switching the whole
application's appearance. Either mode may use any theme, including Kit dark
or Kit light. Window chrome follows the theme in use, light or dark, as well
as content: native controls keep system drawing with readable contrast over
the theme's surfaces.

Imports read Kit's `tokens` and `syntaxPalette` JSON structure. An import copies
theme data into application preferences, validates the whole batch before
changing any preference, and leaves source files untouched; legacy Kit state is
never scanned implicitly. An imported theme's initial assignment comes from its
opaque `tokens.bg` color, not its filename, and themes without a usable opaque
background take the active mode. The imported definition is kept whole even
when some roles have no native consumer yet.

### Icons

Icons are Phosphor Regular (`@phosphor-icons/core` 2.1.1), bundled as SVGs in
`Sources/Kit/Resources/Icons` and named by the `Icon` enum. They draw as
template images tinted by the role colour, at the Paper box sizes: 14 pt for tab,
toolbar and tool-call icons, 12 pt for status, metadata and composer icons, and
10 pt for carets, links and inline close buttons. SF Symbols are not used.
Spinners, status dots and traffic lights are drawn shapes. Every tile tab shows
its icon, accent-coloured on the focused tile's selected tab and muted
otherwise, so selecting a tab never shifts the strip. Hovering a closable tab,
or focusing it from the keyboard, swaps its icon for a 12 pt close button in
the same slot; tabs never grow a trailing close button. To add an icon, copy
its regular SVG from the same Phosphor release and add a matching `Icon` case.

### File annotation interaction

Hovering a source line reveals a gutter + control. Clicking it annotates that
line; dragging across the gutter selects a range with a live highlight, and
releasing opens the input. Source-text selection with Annotate selection is the
keyboard-accessible alternative. The input sits beneath the selected range with
composer styling and shows no line labels or filename. Enter saves, Cmd+Enter
inserts a newline, and Escape cancels. Saved comments use proportional text
with Edit and Delete on hover, while source and line numbers stay monospace.

A strip above the Agent composer shows each note's filename, range, and a short
comment preview, with a bounded overflow list for many notes. Activating a chip
reveals the note in its retained file tab and briefly highlights it. Closing a
tab leaves its server-owned notes intact. Annotations are composition inputs,
including when the text draft is empty, and accepted messages show expandable,
immutable annotation groups with the filename, range, comment, and captured
source.

A stale note stays visible marked File changed and cannot be sent. Activating
it shows the frozen evidence with Select new range and Delete actions. A note is
never silently relocated to newer code, and there is no permanent annotation
pane.

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

Queued messages sit in a tray joined to the top of the Agent composer, or above
the interaction dock while one is open. Its header reads "N queued" with Edit
(restore to the composer), Clear, and Send now (steer the running turn). It
shows the server's first three previews, numbered, then "N more". Rows aren't
clickable. As in the TUI, Return in an empty composer sends the queue while a
turn runs.

### Interaction dock

A pending request takes the Agent composer's place until it is answered or
cancelled; the draft and attachments come back after. The dock is a frame
surface card with a header (warning question icon, title, then the plugin id,
the guided step trail, and "1 of N" when more wait), the muted Markdown detail
and the answer, and a footer of buttons. Back sits on the left from the second
guided question; Skip appears for optional ones. Only y and n are hinted, inside
their buttons. While a response is sent the buttons lock and the primary one
spins. The activity card is hidden while a request waits.

Keys follow the TUI dock, except that option letters are keys rather than j
and k. Esc or × cancels. y and n answer a confirmation; a
plugin's confirmation, or one with a default, highlights an answer that Tab or
the arrows switch and Return chooses, and otherwise the affirmative is primary.
Decline sits left of confirm. A
single choice answers on click, Return, Space, or its letter (A–Z), so a plain
select has no footer; a multiple choice toggles on click, Space, or its letter,
and Return continues. The arrows move the highlight. Tab
and Shift-Tab move between guided questions, and nothing is sent until the last
one. Options are lettered A, B, C with a shared label column; six rows show, then
the list scrolls with the hidden count at each edge. A request this client can't
present shows that it can only be cancelled.

The dock takes focus when it appears only if the Agent tile is focused. It grows
to half the pane before its body scrolls.

Consecutive requests share one dock. After an answer is accepted the dock stays
in its sending state for 300 ms; a request arriving in that window, such as a
plugin's next question, replaces it in place, and otherwise the composer
returns. Moving forward through guided questions, or on to a plugin's next
request, slides the body in from the trailing edge; Back slides it the other
way. The title cross-fades and the height animates; the dock rises in when it appears and fades back
to the composer when it goes. Reduce Motion keeps only the fades.

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
one drawer; the latest thinking line also appears in the activity card above
the pane's composer. Assistant
prose appears on completion. Active drawer following respects manual scrolling.

Clicking a transcript image, attached or shown by the agent, opens it in an
image tab beside the transcript's tile: in the neighbouring tile, preferring
the one to its right, or in a new tile on the right. An image already open is
selected instead. The tab shows the image at full resolution, fitted inside a
20 pt margin and never enlarged past 100%, over a checkerboard for transparency.
Its header gives the pixel and file size, the zoom, Fit and 100%, Copy, and
Save As. Pinching zooms and scrolling or dragging pans; ⌘+ and ⌘− step the zoom,
⌘0 shows actual size, and ⌘9 fits, while the image's tile has focus. Image
tabs restore with the layout; images in workspace files are not shown, since
the server reads only text files.

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
the cwd visible. The session bar shows branch or detached identity and the dirty marker.

## Session operations

Reload reads agent sources on the server and returns selectable warnings and
source diagnostics. It resynchronizes after success or failure. Refreshing an
acknowledged result retries the read, not the mutation. Cwd change and reload are
separate operations.

Explicit compaction keeps an operation identity through recovery and refreshes
the snapshot/stream afterwards. Its pending state belongs in the Agent pane's
activity card, with no secondary modal. Automatic compaction events are matched to their current identity.

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

Subagent panes show the subagent's activity card and queued task count but no
composer. A 36 pt header row under the tab holds the subagent's own split
model and thinking chip, the composer's chip with its popovers opening below.
While the subagent has active or queued work, its model picker is locked with
a note, and thinking can still change. Direct user-to-subagent messaging stays disabled: a subagent's result
is delivered to the parent agent rather than back to the user who messaged it.
Dismissal is supported. Interaction requests carry no conversation identity, so
every interaction docks in the Agent pane. Roster status drives tab activity,
and effective configuration belongs to the selected subagent conversation.

Created-session and peer-session tool cards present structured summaries and
canonical navigation targets, not raw JSON or internal request IDs. Links retain
the originating server identity and focus already-open sessions. Recorded initial
run IDs do not establish current run status, and peer wait timeouts do not imply
cancellation.

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
A 30 pt header row holds the target button, the file count with total
additions and deletions, and a trailing ⋯ button. The target button opens a
searchable picker below it; the ⋯ button opens a view menu, aligned to its
trailing edge, with Wrap lines, Unified, and Side by side. Both are arrowless
anchored cards, and a pick applies and closes. Side by side needs a pane at least
640 pt wide; narrower panes fall back to Unified and dim the option with "Tile
is too narrow". Layout and wrapping are retained by the workspace across target
changes.
Files appear as titled sections in one continuous vertical scroll surface. Each
34 pt file header shows its path and counts, pins to the top while its section
scrolls, and collapses the section on click. Hunks within a file are separated by
a ⋯ gap row rather than a leading hunk header.
Files load through serialized reads and retain independent line pagination.
After the file list arrives, the pane prefetches files' first pages in order,
one at a time, until 50 files or 5,000 rows are loaded; past that, a file
loads when it comes into view, along with the next three. A file you scroll
to waits behind at most the one read in flight. Each native editor sizes to
its source and inline-note height. The pane keeps each file's laid-out height
with the file, and estimates an unwrapped file's height from its rows, so a
file arrives at its size and keeps it when the lazy stack rebuilds its view.
Vertical wheel events pass to the containing diff scroll view.
Split mode pairs replacement runs and leaves
unmatched rows blank without assigning them source coordinates. Both native
editors share row heights so wrapped source and inline notes stay aligned.
Comments remain pinned to their original source side; switching presentation
preserves an unsaved draft.
The pane shows explicit loading, empty, partial, nontext, complex, stale, and
failure states. Loading uses a braille spinner. A clean working tree reads
"Working tree is clean"; an empty branch or commit reads "No changes". Server
refusals use the TUI's wording with a second explanatory line where it helps.
Visible diff panes poll every 5 seconds without an indicator, deferring while a
comment is being edited or saved. Unchanged revisions retain expanded pages and
editor position. A new revision keeps every fully loaded file whose file
revision is unchanged, with its editor and height, and reloads only the files
that changed. Collapsed files stay collapsed across refreshes; hidden panes do
not poll. Workspace invalidation discards in-flight
results, and operation generations prevent older reads from replacing new state.

The native editor shares annotation inputs and notes with file tabs. A semantic
old/new gutter projects source-side coordinates independently of displayed rows,
hunk headers, continuation fragments, and inline note gaps. Deletions anchor the
old side, additions the new side, and context accepts either number column.
The hover comment button sits at the code-facing edge of the gutter and uses a
pointing-hand cursor. The comment input ends with Cancel and a prominent
Comment (Save when editing) button. Saved notes are raised cards that show Edit
and Delete on hover; a note whose file changed reads "File changed" and offers
"Select new range".
Ranges cannot span omitted context or exceed 200 source lines. Ordinary native
selection, syntax highlighting, scrolling, and source-copy behavior are retained.
Refreshing preserves unsaved comments at their original revisions and requires
explicit reselection. Saved notes use the Agent composer's submission contract.

`DiffTests` cover source-side projection, pinned pagination, cancelled/stale reads,
and annotation-authorized reveal. `InlineAnnotationEditorTests` exercise the rendered native
rail and inline editor in both appearances. `KIT_DIFF_LIVE_TEST=1` opts into a
disposable Git repository/session test for all three target kinds, paging, stale
reads, and annotation CRUD; this check never starts an agent run.
