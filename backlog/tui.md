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
- [~] TUI-COOPER-004 — Composer: prompt submission, Escape abort, editing
  bindings, the follow-up queue, and attachments are complete. ↑
  restores queued follow-ups (and their attachments) before the draft, Enter
  on an empty draft sends them to the running turn, and the footer shows
  `N queued · ↑ restore` below stream recovery. A pasted or dropped file list
  uploads as attachments shown as chips above the composer (removed with `×`
  or Ctrl+C) and named in the user's transcript row; as in the vaxis client,
  a file list that arrives as an ordinary text change, or a path completed
  across changes, is attached too. Attachments in a user message are
  `📎 name` rows; clicking an image's row toggles a 12-row inline preview
  (one expanded at a time; a just-sent prompt's first image starts expanded,
  history starts collapsed), and clicking the preview opens a private
  temporary copy in the default application. Ctrl+Shift+W deletes the next
  word. ↑ on a blank draft with nothing queued opens message history in an
  inline picker above the composer: up to 500 recent prompts without repeats,
  oldest first with the newest selected, filtered by the draft; Enter or a
  click recalls a prompt and Escape closes it. Tool-result image previews
  belong to TUI-COOPER-005 and a workspace image pane to TUI-COOPER-008.
  Outstanding:
  - [x] Bash mode: `!command` runs directly as a transcript bash entry whose
    result is added to context; `!!command` excludes the result. One command
    runs at a time, alongside a turn; Escape stops it before the turn, a
    failed start returns it to the composer, and a command running at attach
    is followed again. Unlike the vaxis client, finished `!` runs are drawn
    from the session's persisted context, so they keep their place across
    snapshots and reattaching; `!!` runs are not persisted and leave at the
    next snapshot.
  - [ ] Bash history: ↑/↓ in bash mode open earlier commands in an inline
    picker.
  - [ ] File mentions: `@` opens an inline file picker that inserts
    `@path`.
  - [ ] Session mentions: `#` opens an inline session picker that inserts
    `#[session:<id>]`.
  - [ ] Annotation chips: pending annotations show above the composer and
    are sent with the prompt; queued ones return with restored follow-ups.
- [ ] TUI-COOPER-005 — Tool activity: work chips, inline activity, tool output
  wells, and file navigation from tool results.
- [ ] TUI-COOPER-006 — Command palette, inline pickers, and configuration,
  theme, and model pickers, with shortcuts discovered from Cooper keymaps.
- [x] TUI-COOPER-007 — Interaction dock for pending server-owned requests:
  model confirm, input, select, and guided requests, and plugin confirm,
  input, and select requests with their labels, default choice, initial
  value, and empty answers. Filterable plugin selections use the plain list,
  as in the vaxis client.
- [ ] TUI-COOPER-008 — Workspace panes: tabs, File, Diff, scratchpad, and
  annotations.
- [ ] TUI-COOPER-009 — Subagents: activity, picker, conversation tabs, and
  dismissal.
- [ ] TUI-COOPER-010 — Sessions: picker and explorer (`kit sessions`), rename,
  delete, details, and forking.
- [~] TUI-COOPER-011 — Provider login flows and API-key entry. The signed-out
  provider picker, Codex device login, Claude browser/manual-code login, and
  Anthropic, OpenAI, and OpenCode Go API-key entry are complete. Remaining:
  connect another provider from an already-ready session.
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
- [ ] TUI-COOPER-014 — MCP status, plugin footer contributions, and plugin
  commands.
- [ ] TUI-COOPER-015 — Replace `internal/tui` and `cmd/kit`: build releases
  from `apps/cli`, run its checks in CI, and remove the vaxis client.

Upstream dependencies:

- Cooper: a CUI
  `text_area` submit callback (TUI-COOPER-004; currently a keymap binding).

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
