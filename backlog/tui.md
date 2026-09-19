# Native TUI backlog

This ledger owns terminal presentation and interaction. Server behavior belongs
in the [core backlog](core.md); dependencies below refer to its stable IDs.

## R1 required

### Transcript and activity

- [ ] TUI-TRANSCRIPT-002 — Add syntax highlighting and drag-safe in-app link
  activation while preserving selectable text and terminal-native copying.
- [~] TUI-TRANSCRIPT-003 — Present model/thinking selection, context
  pressure/capacity, retry countdowns, compaction lifecycle, cancellation, and
  terminal run state without obscuring retained evidence. Depends on
  `CORE-RUN-003`, `CORE-RUN-004`, and `CORE-RUN-005`.

### Composer, sessions, and commands

- [~] TUI-COMP-001 — Complete durable drafts, composer history, and attachment
  editing around the existing multiline composer, follow-up queue, abort, paste,
  and cursor behavior. Mark queued annotations read-only in workspace/picker
  controls with a restore action; the server already enforces captured queue
  ownership. Depends on `CORE-ATT-001`.
- [ ] TUI-SESSION-001 — Expose session creation, opening, switching, naming,
  deletion, automatic names, and recovery errors through bounded native flows.
- [~] TUI-FORK-001 — Add `/fork [message]`, switch only the invoking TUI to the
  linked child returned by the server, optionally submit the message as its
  first new prompt, and expose recoverable failures. Depends on
  `CORE-FORK-001`.
- [~] TUI-CMD-001 — Complete palette filtering, completion, arguments, nested
  pickers, keyboard/mouse behavior, and required command sources.
- [ ] TUI-CMD-002 — Add the R1 command surfaces for settings, MCP, logout, and
  release/update information with clear availability rules.
- [ ] TUI-SET-001 — Present immediate setting changes, validation, and inline
  persistence failures. Depends on `CORE-SET-001`.
- [ ] TUI-SET-002 — Expose R1 settings for default model/thinking, retry
  behavior, guided questions, and preferred diff layout. Depends on
  `CORE-SET-002`.

### User interaction and integrations

- [ ] TUI-AUTH-001 — Present provider login, API-key replacement, logout, and
  actionable failures without exposing credentials. Depends on `CORE-AUTH-001`.
- [ ] TUI-MCP-001 — Present MCP connection, authentication, failure, and debug
  state without exposing credentials. Depends on `CORE-MCP-004` and
  `CORE-MCP-005`.
- [~] TUI-VCS-001 — Complete footer repository state, production-equivalent
  refresh behavior, and silent fallback. Depends on `CORE-VCS-001`.
- [~] TUI-TERM-001 — Complete clipboard, terminal title, notifications, image
  capabilities, attention/progress state, and clean restoration on every exit.
- [ ] TUI-HEAD-001 — Make diagnostics and unavailable interaction behavior clear
  when transitioning between TUI and headless workflows.
- [ ] TUI-TEST-001 — Pass deterministic presentation, keyboard/mouse, focus,
  directory/file/diff, attachment/image, interaction, reconnect, and narrow/wide
  layout suites.

## R1 decision

- [ ] TUI-KEY-001 — Decide whether configurable keybindings and current
  keybinding configuration compatibility are required for R1.

Regardless of that decision, R1 requires documented defaults, deterministic
focus and overlay precedence, conflict-free built-in bindings, conventional
composer editing, and keyboard access to every essential action. If configuration
is required, split implementation into follow-up `TUI-KEY-*` requirements before
resolving this decision.

## Post-R1

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
- [ ] TUI-SCRATCH-001 — Add a scratchpad workspace with guarded edits and
  autosave feedback. Depends on `CORE-SCRATCH-001`.
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
  when their owning Post-R1 capabilities are implemented.
- [ ] TUI-GH-001 — Present cached GitHub pull-request metadata and a safe
  click-through URL. Depends on `CORE-GH-001`.
- [ ] TUI-PLUGIN-001 — Present plugin header/footer contributions, theme tokens,
  hide claims, activation, failure, cleanup, reload, and restart. Depends on
  `CORE-PLUGIN-004`, `CORE-PLUGIN-005`, and `CORE-PLUGIN-006`.
- [ ] TUI-SET-003 — Expose Post-R1 pager defaults when that workflow exists.
