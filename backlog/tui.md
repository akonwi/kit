# Native TUI backlog

This ledger owns terminal presentation and interaction. Server behavior belongs
in the [core backlog](core.md); dependencies below refer to its stable IDs.

## Release scope

### Composer, sessions, and commands

- [ ] TUI-FORK-001 — Tell the user why `/fork` did not start when the session is
  not ready or already has active work, and when a completed fork is discarded
  because the viewed session changed.
- [ ] TUI-CMD-004 — Tab completes the selected command's name in the palette
  query followed by a space and keeps the palette open. The list stays pinned
  to that command while arguments are typed, until the command name is edited.
- [x] TUI-CMD-005 — Edit the palette query conventionally: move the cursor with
  Left/Right/Home/End, insert and delete at the cursor, and delete by word.
  List navigation keeps its current keys; no additional bindings are added.
- [ ] TUI-CMD-007 — Show argument hints for built-in commands that accept
  arguments (`cd`, `name`, `fork`). Running a command without a required
  argument (such as `cd`) prompts for it in the palette's dialog frame instead
  of closing the palette.
- [ ] TUI-CMD-008 — Commands whose follow-up is a simple list or text prompt
  (`model`, `thinking`, `theme`, `name` without an argument, and `logout` once
  `TUI-CMD-002` adds it) continue in the palette's dialog frame, with the same
  position and size, instead of closing it and opening a separately sized
  modal. Esc closes the dialog.
- [ ] TUI-CMD-009 — Show plugin commands in the palette by their short command
  name instead of the full canonical ID, keeping the owning plugin in the
  metadata column. Commands from different plugins that share a short name
  must both remain listed and runnable.
- [ ] TUI-CMD-002 — Add production-release command surfaces for settings,
  MCP, logout, and release/update information with clear availability rules.
- [ ] TUI-SET-001 — Present immediate setting changes, validation, and inline
  persistence failures. Depends on `CORE-SET-001`.
- [ ] TUI-SET-002 — Expose production settings for default model/thinking,
  retry behavior, guided questions, preferred diff layout, and prompt cache
  retention. Depends on `CORE-SET-002`.

### User interaction and integrations

- [ ] TUI-AUTH-001 — Present provider login, API-key replacement, logout, and
  actionable failures without exposing credentials. Depends on `CORE-AUTH-001`.
- [ ] TUI-HEAD-001 — Make diagnostics and unavailable interaction behavior clear
  when transitioning between TUI and headless workflows.

## Cooper client parity

The Ard and Cooper terminal client in `apps/cli`
([ADR 0035](../docs/adrs/0035-build-the-terminal-client-with-ard-and-cooper.md))
replaces the vaxis/ui client in `internal/tui` once it reaches parity with it.
Parity means the same visible content, layout, focus, and interaction as the
vaxis client, asserted by exact-row headless Cooper tests derived from the
vaxis client's tests. Until then, `cmd/kit` ships the vaxis client. Known,
accepted differences: centered odd-width content can sit one cell right,
because Cooper rounds half-cell layout edges up (Cooper ADR 0020).

- [x] TUI-COOPER-001 — Session bootstrap and shell chrome: resolve, resume, or
  create the session; header, dividers, footer location and bash state,
  loading, failed (with `r` retry), and signed-out states; Ctrl+C clear and
  detach. Ctrl+C acts only outside modals and text fields other than the
  composer, where it clears a draft or detaches.
- [x] TUI-COOPER-002 — Live session: `Watch` bridge, batched event dispatch to
  the UI thread, run lifecycle, reconnect and recovery footer states, and the
  incompatible-daemon state. An incompatible server freezes the attachment,
  keeping its transcript and draft, and the footer asks for a restart; there
  is no recheck. A lost turn stream is recovered as in the vaxis client: with
  backoff while the turn runs, showing `Reconnecting activity…` while the
  server is unreachable, and through `Syncing final transcript…` with up to six
  final-snapshot attempts before the turn is settled from its status.
- [x] TUI-COOPER-003 — Transcript: port the transcript projection
  (`transcript_model.go`) to Ard; render user, assistant, thinking, and bash
  entries with Markdown and highlighted code in a virtual list; follow the
  bottom and latest-message shortcut (now driven by committed virtual-list
  updates); open tall assistant replies at their start when they arrive or
  when attaching; load earlier history a page at a time within 40 rows of
  the top, including when the first page does not fill the view, keeping
  loaded pages across later snapshots. The vaxis reading-section strip and
  "Beginning of conversation" row are intentionally not ported.
- [~] TUI-COOPER-005 — Tool activity: work chips, inline activity, tool output
  wells, tool-result image previews, and file navigation from tool results.
  Remaining: a subagent's conversation tab from its name (TUI-COOPER-009).
  Rows follow the vaxis client (`presentToolCall` in
  `internal/tui/transcript_model.go`) unless marked new. Titles and
  summaries of the tools vaxis knows are ported as `ffi/toolpresentation`;
  the open items below are what remains for each tool.
  Shared:
  - [x] Work chip: one row per run of tool calls (`▸ 3 tool calls · 1
    failed`), a spinner while running and closed, and a click to open it,
    closing any other, including one that opened on its own. A running run of up to five calls opens on its own
    until clicked. Runs are keyed by turn and first call, so a live run
    keeps its state when persisted. While open it lists the plain tool rows
    until the activity list replaces them. (vaxis's `N steps` label is not
    ported: a run always starts with a tool call.)
  - [x] Activity list: an open chip lists, for each of the run's messages,
    its thinking (muted italic Markdown, aligned with the tool rows) and then
    a row per tool call. Rows come from the calls, so a call without a result
    reads planned, or not run after an abort. Prose ends a run and stays
    outside it, as in vaxis; unlike vaxis, a message with only thinking
    stays in its run and its thinking is listed. A finished reply that only
    thinks leads the run that follows in its turn, or else shows on its own
    in the same muted italic.
  - [x] Tool row: a state icon (spinner, blank when done, `✗` failed, `⊘`
    not run), the title in the accent color (danger when failed, muted when
    aborted), and a summary chip. A chip's rows share a title column as wide
    as their longest title, from 18 to 32 cells (vaxis fixes it at 18).
    Below 60 columns the chip takes a second line and paths are cut from the
    start.
  - [x] Row selection: clicking a row fills it; closing or opening a chip
    clears the selection. Unlike vaxis, opening a chip selects nothing.
  - [x] Unknown tools: the humanized name (`my_tool` → "My Tool") with the
    `command`, `path`, or `agent` argument, else compact arguments, "no
    arguments", or "arguments truncated".
  - [x] Output (new; vaxis has unused output wells and previews): the
    click that selects a row also shows or hides its result below it, in a
    well of at most 14 rows that scrolls on its own and hands the wheel to
    the transcript at its edges. A running call's output streams in and the well follows its end.
    A footer counts overflowing lines and notes truncated output or omitted
    details. Several wells may be open; opening or closing chips closes
    them. Lines keep their shape: wider output scrolls sideways, with a
    horizontal scrollbar while it overflows.
  Coding tools:
  - [x] `bash`: Run command · the command (multi-line commands summarized).
  - [x] `bash` output: the full command, highlighted as bash, then its
    output: highlighted in a file's language when the command only prints
    that file (`cat`, `head`, `tail`, `sed -n`), in diff colors when it is a
    unified diff, and plain otherwise or while running.
  - [x] `read`: Read file · `path:start–end`, `· empty`, `· truncated`.
  - [x] `read` output: the lines read, highlighted in the file's language.
  - [x] `write`: Write N lines · path.
  - [x] `write` output: the written content, highlighted, or the result
    when the server cut the arguments.
  - [x] `edit`: Edit N sections · path.
  - [x] `edit` output: each edit's old and new text as removed and added
    lines on the theme's diff fills, highlighted in the file's language.
  - [x] `read`, `write`, `edit`: clicking the summary opens the file in a
    File tab, at the first line read for `read`.
  - [x] `ls`: List directory · path.
  - [x] `grep`: Search · `pattern in path`.
  - [x] `find`: Find files · `pattern in path`.
  Session tools:
  - [x] `change_cwd`: Change directory · path.
  - [x] `change_cwd`: the client follows `session.cwd.changed`, from the
    tool, `/cd`, or another client: the session's working directory, the
    footer location, and the file-mention index. As in vaxis, only `/cd`
    toasts.
  - [x] `read_scratchpad` (new title): Read scratchpad, without a chip.
  - [x] `edit_scratchpad`: Update scratchpad · N edits.
  - [x] `confirm_from_user`, `input_from_user`, `select_from_user`,
    `guided_questions` (new titles): Confirm, Ask (input and select), and
    Ask questions · the request's title. The request itself is in the
    interaction dock (TUI-COOPER-007).
  - [x] `create_session`: Create session · `name · cwd`.
  - [x] `activate_skill`: Load skill · name.
  - [x] `peer_session`: Discover sessions, Ask session, Inspect peer query,
    or Wait for peer query by action · the message, request, or session.
  - [x] `subagent`: Start, Message, Wait for, Cancel, Dismiss, or Inspect
    agent by action · the agent name.
  - [ ] `subagent`: the agent name opens its conversation tab
    (TUI-COOPER-009).
  - [x] `show_image` (new title): Show image · path, or caption without one.
  - [x] `show_image`: the image below its chip, open or closed, in 12
    reserved rows with its caption beneath; clicking opens it. It is the
    only tool-result image the server keeps an attachment for, so it covers
    tool-result image previews.
  - [x] `inspect_image` (new title): Inspect image · path. The image goes to
    the model only.
  Subagent tools, shown in subagent conversation tabs (TUI-COOPER-009):
  - [x] `subagent_inbox`, `subagent_send`, `subagent_reply`,
    `subagent_inspect` (new titles): Check inbox (without a chip), Message
    sibling, Reply, Inspect request · the agent or receipt.
  Runtime tools:
  - [x] MCP namespaces, one tool per server named after it (new titles): List
    `<server>`, Search `<server>` · query, Describe or Call `<server>` ·
    tool, Log out of `<server>`, recognized by the session's configured
    servers.
  - [x] Plugin tools, named `<plugin>__<tool>` (new titles): the humanized
    tool name · the plugin.
- [~] TUI-COOPER-006 — Command palette, inline pickers, and configuration,
  theme, and model pickers, with shortcuts discovered from Cooper keymaps.
  The palette lists built-in, prompt (user, project, and Claude Code
  project), and plugin commands with argument hints, fuzzy search over names
  and aliases, Tab completion, and feedback for commands unavailable while
  busy or without a compatible server. The theme, model, and thinking pickers
  and the composer's inline picker are complete. Remaining: the `subagents`
  command (TUI-COOPER-009), and shortcuts discovered from Cooper keymaps.
- [x] TUI-COOPER-007 — Interaction dock for pending server-owned requests:
  model confirm, input, select, and guided requests, and plugin confirm,
  input, and select requests with their labels, default choice, initial
  value, and empty answers. Filterable plugin selections use the plain list,
  as in the vaxis client.
- [~] TUI-COOPER-008 — Workspace panes: tabs, File, Diff, and scratchpad.
  Images open in the system viewer rather than a pane. Panes are
  mouse-first; keyboard navigation inside them is lighter than in vaxis.
  - [x] Tabs: Agent first, then panes in the order they were opened, and the
    strip only once one is open. Opening an open pane selects it; at most
    32 are open. Closing the selected tab selects its neighbor. Tabs that
    don't fit collapse into `… N more`, which, like `/tabs`, opens a picker
    of every tab (Ctrl+D closes one). Ctrl+] and Ctrl+[ step through the
    tabs, wrapping through Agent. Scratchpad is a tab.
  - [ ] Tab and Shift+Tab move focus between the pane and the composer.
  - [x] File: `/files` (Ctrl+O) picks from the file-mention index (Ctrl+R
    indexes again). The pane is mouse-only: highlighted text with line
    numbers and a cursor line moved by clicking, scrolling sideways with
    Shift+wheel or a horizontal swipe. It loads the current file each time
    it is shown. It shows
    loading, empty, binary, missing, unreadable, busy, and truncated states,
    and freezes once the session leaves its workspace. A tool row's summary
    opens it (TUI-COOPER-005).
  - [x] Diff: `/diff` opens the Diff tab, which shows the working tree of
    the session's current workspace as one document: a section per changed
    file between rules, whose header (a ghost button with its path) stays
    pinned while its lines scroll and collapses the file when clicked. A section loads when it
    scrolls into view, with old and new line numbers, diff fills, and
    highlighting; hunks are separated by `⋯`, without patch headers. Lines
    scroll sideways like the File pane's. While shown, it checks the working
    tree every 5 seconds and reloads what changed, keeping its place.
  - [ ] Diff: commit and branch targets from a target picker in the header,
    split layout where wide enough, and wrapping.
- [ ] TUI-COOPER-016 — Annotations on the File and Diff panes
  (TUI-COOPER-008): create, edit, and delete annotations on file and diff
  lines. Pending annotations show as chips above the composer and are sent
  with the next prompt; queued ones return with restored follow-ups.
  Activating a chip opens its pane at the annotated lines, and a stale one
  opens the annotation picker.
- [ ] TUI-COOPER-009 — Subagents: activity, picker, conversation tabs, and
  dismissal.
- [~] TUI-COOPER-010 — Sessions: picker and explorer (`kit sessions`), rename,
  delete, details, and forking. In a session, the `sessions` explorer searches
  sessions as a fork tree that Left and Right fold, switches to the chosen
  session, and renames (Ctrl+R) and deletes (Ctrl+D) sessions; `name`
  renames, `fork` forks with an optional first message, and `debug` shows
  session details. Remaining: the standalone `kit sessions` picker, which
  still reports that it is unavailable (`pick_session` in `tui.ard`).
- [~] TUI-COOPER-011 — Provider login flows and API-key entry. The signed-out
  provider picker, Codex device login, Claude browser/manual-code login, and
  Anthropic, OpenAI, and OpenCode Go API-key entry are complete, and `login`
  opens the same flow from a ready session. Remaining: connecting from a ready
  session restarts session startup; as in the vaxis client, it should return
  to the session and report "Connected to <provider>".
- [~] TUI-COOPER-012 — Toasts, terminal title, progress, notifications,
  selection copy, and link opening. The terminal title and Ghostty progress
  report the session name, directory, a running turn or manual compaction, and
  a turn waiting for an answer (`?` with paused progress). Remaining:
  turn-completion notifications and bell; an attention alert (bell and desktop
  notification) when a model or plugin request arrives, once per request, so a
  user in another window notices the agent is waiting (not in the vaxis
  client); and confirming toast, selection-copy, and link-opening parity.
- [ ] TUI-COOPER-013 — System theme derived from terminal colors, matching the
  vaxis client's contrast-aware palette, plus user theme tokens. The palette
  derivation comes from Cooper's terminal-theme service, including live host
  updates. Remaining: use the derived surfaces for filled controls such as the
  signed-out "Connect a provider" button.
- [~] TUI-COOPER-014 — MCP status, plugin footer contributions, and plugin
  commands. The `mcp` dialog lists configured servers with their state, each
  MCP configuration warning is shown once as a toast, plugin commands run
  from the palette with their arguments, and plugin notifications, requests
  (TUI-COOPER-007), and session messages are shown. Remaining: plugin footer
  contributions (the snapshot's `PluginFooter` items, which can hide the
  location), and the footer's VCS branch and pull-request link that share the
  footer's right side.
- [ ] TUI-COOPER-015 — Replace `internal/tui` and `cmd/kit`: build releases
  from `apps/cli`, run its checks in CI, and remove the vaxis client.

Upstream dependencies:

- Cooper: a CUI `text_area` submit callback, in place of the composer's
  Enter keymap binding.

## Scope decisions

- [-] `TUI-KEY-001` — User-configurable keybindings and compatibility with
  current keybinding configuration are not part of the production release.
  Documented defaults, deterministic focus and overlay precedence,
  conflict-free built-in bindings, conventional composer editing, and
  keyboard access to every essential action remain required. Any future
  configurable-keybinding work requires a new stable `TUI-KEY-*` requirement.

- [-] `TUI-CMD-010` — A universal palette that also ranks files, panes,
  sessions, and subagents is not planned. The palette lists commands ranked by
  fuzzy score; explorers remain commands that open their own pickers.

## Deferred scope

- [ ] TUI-GPT6-001 — Present live steering submissions as queued, applied, or
  failed using server-owned status; distinguish steering from cancellation and
  retain recoverable input after transport failures. Test keyboard/composer
  interactions, status transitions, reconnect, and narrow layouts. Depends on
  `CORE-GPT6-004` in the [core backlog](core.md).
- [ ] TUI-GPT6-002 — Present pending asynchronous tools and nonblocking user
  questions while model output continues. Make result arrival, wait state,
  dismissal, cancellation, and turn completion understandable without stealing
  composer focus. Cover deterministic content, focus, and interaction tests.
  Depends on `CORE-GPT6-005` in the [core backlog](core.md).
- [ ] TUI-GPT6-003 — Present supported standard/pro mode separately from effort,
  and show effective effort after cached configuration updates using
  server-authoritative state. Explain unavailable combinations without exposing
  provider wire types in the renderer. Test visible settings, persistence errors,
  and model switching. Depends on `CORE-GPT6-007` in the
  [core backlog](core.md); effort history is already described in the
  [feature guide](../docs/features/reasoning-effort-history.md).

- [ ] TUI-PICK-009 — Render the transcript reading section list as an inline
  picker attached to its message, without a query.
- [ ] TUI-PICK-006 — Route diff target picker keys through the app input
  owner so keys typed after Shift+g but before the picker first paints are
  not lost. The picker's query, selection, and catalog live in the diff pane
  state and reach `pickerKeyModel` through the canonical picker's `OnKey`
  hook, which only exists once the picker is painted. Moving that state to
  the app would require the app to own diff-specific picker state.

- [ ] TUI-TRANSCRIPT-006 — Progressively enrich tool-call presentation with
  bounded recorded output and explicit truncated-content and omitted-detail
  evidence, preserving equivalent presentation for live and restored activity.
- [ ] TUI-MERMAID-001 — Render Mermaid diagrams inline with a safe visual
  fallback, or adopt an explicitly reviewed native equivalent.
- [ ] TUI-IMAGE-001 — Add retained `vaxis/ui` image paint operations upstream
  and use them for native Kitty/Sixel transcript rendering with clipping.
- [ ] TUI-ATT-003 — Read an image from the system clipboard into a bounded
  temporary file for attachment. Terminals paste only text and Cooper's
  clipboard is text-only (OSC 52), so this uses `osascript` on macOS and
  `wl-paste` or `xclip` on Linux when installed, and reports when no reader
  or image is available. Shared by TUI-ATT-005 and TUI-ATT-006.
- [ ] TUI-ATT-004 — When a paste consists only of absolute paths and any of
  them no longer exists, explain it (for example, "Device Hub-001023.png no
  longer exists") instead of silently inserting the paths as text. Screenshot
  tools such as CleanShot put a temporary file on the clipboard and delete it
  shortly after, so the pasted path is often stale.
- [ ] TUI-ATT-005 — When a pasted image path no longer exists, attach the
  image still on the clipboard instead, through TUI-ATT-003.
- [ ] TUI-ATT-006 — Ctrl+V attaches an image from the clipboard through
  TUI-ATT-003. Copying image data alone (from a browser, or a screenshot sent
  to the clipboard) makes the terminal paste nothing, so a key is the only
  way in; Cmd+V never reaches the app.
- [ ] TUI-THREAD-001 — Present cached `#thread` suggestions, escaping, bounded
  expansion, and cancellation. Depends on `CORE-THREAD-001`.
- [ ] TUI-PAGER-001 — Implement pager sectioning, auto-open behavior, notes,
  draft attachments, restoration, submission, and failure recovery.
- [ ] TUI-REVIEW-001 — Extend File and Diff tabs with target-scoped annotations,
  plus modal review-target and changed-file navigation; do not add a separate
  Review tab. Depends on `CORE-REVIEW-001`.
  See [ADR 0020](../docs/adrs/0020-file-diff-review-workspace-surfaces.md) and
  [ADR 0024](../docs/adrs/0024-generalize-diff-review-targets.md).
- [ ] TUI-WORK-002 — Add release-note and other approved retained workspace
  panes without duplicating server state.
- [ ] TUI-WORK-003 — Replace the one-level workspace return pane with a real
  retained stack when multi-pane retention becomes a product requirement. See
  the [known limitations](workspace-pane-stack.md).
- [ ] TUI-WORK-004 — Design direct recovery when opening a pane at the workspace
  tab ceiling without silent eviction or stale pending requests. See the
  [focused design note](workspace-tab-ceiling-recovery.md).
- [ ] TUI-PERF-001 — Suspend hidden workspace pane reconciliation and animation
  when measurements show material cost, while preserving retained state. See
  the [focused optimization note](hidden-workspace-pane-suspension.md).
- [ ] TUI-CMD-003 — Add `/pager`, `/code-review`, `/tree`, and other commands
  when their owning deferred capabilities are implemented.
- [ ] TUI-SET-003 — Expose deferred pager defaults when that workflow exists.
