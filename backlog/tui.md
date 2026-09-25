# Native TUI backlog

This ledger owns terminal presentation and interaction. Server behavior belongs
in the [core backlog](core.md); dependencies below refer to its stable IDs.

## Release scope

### Composer, sessions, and commands

- [x] TUI-COMP-001 — Recall the full session's direct-bash history from the
  composer. This item is complete when every subitem below is complete.
  - [x] TUI-COMP-001a — Persist every settled direct-bash execution in the
    authoritative session history, including its command, execution status,
    output metadata, and `!`/`!!` context mode. Done in
    `internal/storage/migrations/0010_direct_bash_history.sql` and
    `internal/session/bash.go`; tracked by
    [ADR 0006](../docs/adrs/0006-droids-as-session-data-authority.md).
  - [x] TUI-COMP-001b — Persist excluded `!!` executions as durable session
    history without projecting them into the transcript. Direct shell work has
    its own sequence space, separate from droid history, so it is recalled
    through the bash-history projection rather than spliced into the paginated
    transcript. Included `!` executions already appear once, as droid context
    boundaries.
  - [x] TUI-COMP-001c — Read the complete durable history in the
    composer-anchored picker rather than only the loaded transcript and live
    projection, and show it newest-first with each entry's `!`/`!!` context
    mode. `GET /v1/sessions/{id}/bash-history` serves a bounded newest-first
    page; the TUI opens with one ten-entry page and loads older entries on
    demand when navigation reaches the oldest loaded row, within bounded depth.
    Filtering, navigation, insertion without execution, and dismissal continue
    to work for that complete history.
  - [x] TUI-COMP-001d — Present recalled history after reload and restart
    through the same picker, including `!!` executions that never enter droid
    history, and assert the exact expected rows. Durable history, not the loaded
    transcript, admits the picker, so recall works when the projection holds no
    direct shell rows. An empty picker presents an in-flight read until the
    durable page answers.
- [ ] TUI-SESSION-001 — Expose session creation, opening, switching, naming,
  deletion, automatic names, and recovery errors through bounded native flows.
- [~] TUI-FORK-001 — Add `/fork [message]`, switch only the invoking TUI to the
  linked child returned by the server, optionally submit the message as its
  first new prompt, and expose recoverable failures. Depends on
  `CORE-FORK-001`.
- [~] TUI-CMD-001 — Complete palette filtering, completion, arguments, nested
  pickers, keyboard/mouse behavior, and required command sources.
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
- [ ] TUI-TEST-001 — Pass deterministic presentation, keyboard/mouse, focus,
  directory/file/diff, attachment/image, interaction, reconnect, and narrow/wide
  layout suites.

## Scope decisions

- [-] `TUI-KEY-001` — User-configurable keybindings and compatibility with
  current keybinding configuration are not part of the production release.
  Documented defaults, deterministic focus and overlay precedence,
  conflict-free built-in bindings, conventional composer editing, and
  keyboard access to every essential action remain required. Any future
  configurable-keybinding work requires a new stable `TUI-KEY-*` requirement.

## Deferred scope

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
