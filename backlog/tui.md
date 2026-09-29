# Native TUI backlog

This ledger owns terminal presentation and interaction. Server behavior belongs
in the [core backlog](core.md); dependencies below refer to its stable IDs.

## Release scope

### Composer, sessions, and commands

- [~] TUI-FORK-001 — Add `/fork [message]`, switch only the invoking TUI to the
  linked child returned by the server, optionally submit the message as its
  first new prompt, and expose recoverable failures. Depends on
  `CORE-FORK-001`.
- [ ] TUI-CMD-004 — Tab completes the selected command's name in the palette
  query followed by a space and keeps the palette open. The list stays pinned
  to that command while arguments are typed, until the command name is edited.
- [ ] TUI-CMD-005 — Edit the palette query conventionally: move the cursor with
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
- [x] TUI-PICK-001 — The palette picker widget
  (`internal/tui/palette_picker.go`), its items (`internal/tui/picker.go`),
  and the shared key model (`internal/tui/picker_keys.go`) are the contract
  for all palette pickers, including the session explorer's hierarchy.
- [ ] TUI-PICK-008 — Add the inline picker for lists anchored to the composer
  or another point in the UI, sharing `pickerItem`, `filterPickerItems`, row
  rendering, and navigation keys with the palette picker while the composer
  owns the query.
  Migrate the file and session mention menus, message and bash history, and
  the transcript reading section list.
- [ ] TUI-CMD-002 — Add production-release command surfaces for settings,
  MCP, logout, and release/update information with clear availability rules.
- [ ] TUI-SET-001 — Present immediate setting changes, validation, and inline
  persistence failures. Depends on `CORE-SET-001`.
- [ ] TUI-SET-002 — Expose production settings for default model/thinking,
  retry behavior, guided questions, and preferred diff layout. Depends on
  `CORE-SET-002`.

### User interaction and integrations

- [ ] TUI-AUTH-001 — Present provider login, API-key replacement, logout, and
  actionable failures without exposing credentials. Depends on `CORE-AUTH-001`.
- [~] TUI-TERM-001 — Complete clipboard, terminal title, notifications, image
  capabilities, attention/progress state, and clean restoration on every exit.
- [ ] TUI-HEAD-001 — Make diagnostics and unavailable interaction behavior clear
  when transitioning between TUI and headless workflows.

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
- [ ] TUI-ATT-003 — Add bounded binary clipboard image ingestion when vaxis
  exposes a typed clipboard payload contract.
- [ ] TUI-THREAD-001 — Present cached `#thread` suggestions, escaping, bounded
  expansion, and cancellation. Depends on `CORE-THREAD-001`.
- [ ] TUI-PAGER-001 — Implement pager sectioning, auto-open behavior, notes,
  draft attachments, restoration, submission, and failure recovery.
- [x] TUI-SCRATCH-001 — Add the retained singleton scratchpad workspace with
  guarded autosave, silent clean reconciliation, and inline conflict review.
  Depends on `CORE-SCRATCH-001`. See
  [ADR 0025](../docs/adrs/0025-share-database-backed-scratchpads-across-session-families.md).
- [x] TUI-ANN-001 — Add retained File-pane line/range selection and inline
  workspace-file annotations with bounded editing, anchor navigation, stale
  treatment, keyboard and gutter-mouse interaction, and server-authoritative
  synchronization. Depends on `CORE-ANN-001`. See
  [ADR 0022](../docs/adrs/0022-model-draft-annotations-as-session-inputs.md).
- [x] TUI-ANN-002 — Project each live annotation as a synchronized composer chip
  on every tab, preserve explicit ordering, submit annotation IDs with prompts,
  remove accepted drafts, and render immutable submitted snapshots. Depends on
  `CORE-ANN-002`. See
  [ADR 0022](../docs/adrs/0022-model-draft-annotations-as-session-inputs.md).
- [ ] TUI-REVIEW-001 — Extend File and Diff tabs with target-scoped annotations,
  plus modal review-target and changed-file navigation; do not add a separate
  Review tab. Depends on `CORE-REVIEW-001`, `CORE-ANN-001`, and `CORE-ANN-002`.
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
- [x] TUI-GH-001 — Present cached GitHub pull-request metadata and a safe
  click-through URL. The built-in location displays the server-cached PR number
  and opens its validated URL; renderer, interaction, and stale-response tests
  cover the behavior. Manual desktop/terminal verification remains. Depends on
  `CORE-GH-001`.
- [x] TUI-PLUGIN-001 — Present plugin contributions only in the bottom-right
  footer, supporting composition and replacement of default cwd/Git content
  through scoped hide claims. Preserve theme tokens, bounded layout,
  failure, cleanup, and reload; keep header and bottom-left status
  protected. Depends on `CORE-PLUGIN-004`, `CORE-PLUGIN-005`, and
  `CORE-PLUGIN-006`. See
  [ADR 0026](../docs/adrs/0026-scope-plugin-processes-and-route-plugin-ui.md).
  Static set/update/clear, aggregate hide/show, generation revocation, semantic
  text styling, and labeled bounded overflow are now projected through the
  session snapshot. Host, protocol, daemon, and presentation tests cover this
  slice. The user manually verified rendering, overflow, and lifecycle behavior.
  Plugin click dispatch and URL
  routing are outside native scope, not deferred work; built-in PR links remain.

- [ ] TUI-SET-003 — Expose deferred pager defaults when that workflow exists.
