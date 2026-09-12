# Native TUI roadmap

This ledger owns terminal presentation and interaction. Server behavior belongs
in the [core roadmap](core.md); dependencies below refer to its stable IDs.

## R1 required

### Theme and shell

- [~] TUI-THEME-001 — Complete Kit's semantic vaxis/ui design language across
  terminal capabilities, surface hierarchy, focus, empty/error states, and
  responsive layouts.
- [ ] TUI-THEME-002 — Discover and select themes, preserve current custom-theme
  compatibility, persist the selection, apply changes immediately, and provide
  semantic fallbacks for incomplete themes. Depends on `CORE-MIG-004` and
  `CORE-SET-001`.
- [~] TUI-SHELL-001 — Complete narrow/wide workspace behavior, retained pane
  state, focus cycling, draggable remembered ratios, and the pane registry used
  by R1 surfaces.
- [~] TUI-SHELL-002 — Complete layered focus and deterministic overlay
  precedence for the built-in keymap.
- [~] TUI-SHELL-003 — Present bounded toasts, confirmations, inputs, selectors,
  startup/auth failures, fatal errors, and interactive tool surfaces with clear
  focus and cancellation behavior.

### Directory, file, and diff workspace

- [ ] TUI-WORK-001 — Register retained directory, file, and diff panes with
  stable identity, deduplication, predictable open/close/focus behavior, and
  preserved selection and scroll state across pane switches and responsive
  narrow/wide transitions.
- [ ] TUI-DIR-001 — Provide a workspace-rooted directory explorer with lazy,
  bounded expansion; path filtering; keyboard and mouse navigation; visible
  loading, empty, and error states; refresh; and direct file opening. Depends on
  `CORE-WORK-001`.
- [ ] TUI-FILE-001 — Provide a selectable, syntax-highlighted file viewer with
  line numbers, vertical and horizontal navigation, truncation/staleness
  feedback, refresh that preserves position when possible, and safe binary or
  unreadable-file handling. Depends on `CORE-WORK-001`.
- [ ] TUI-DIFF-001 — Provide a read-only diff viewer for working-tree and agent
  edits with changed-file and hunk navigation, semantic added/removed/context
  styling, line-number gutters, unified and split layouts where width permits,
  and explicit loading, empty, stale, truncated, and error states. Depends on
  `CORE-DIFF-001`.

### Transcript and activity

- [~] TUI-TRANSCRIPT-001 — Render active and historical Markdown, thinking,
  tool activity, code, structured updates, errors, and usage consistently after
  attach, reconnect, and restart. Depends on `CORE-RUN-001` and `CORE-RUN-002`.
- [ ] TUI-TRANSCRIPT-002 — Add syntax highlighting and drag-safe in-app link
  activation while preserving selectable text and terminal-native copying.
- [~] TUI-TRANSCRIPT-003 — Present model/thinking selection, context
  pressure/capacity, retry countdowns, compaction lifecycle, cancellation, and
  terminal run state without obscuring retained evidence.

### Composer, sessions, and commands

- [~] TUI-COMP-001 — Complete durable drafts, composer history, and attachment
  editing around the existing multiline composer, follow-up queue, abort, paste,
  and cursor behavior. Depends on `CORE-ATT-001`.
- [ ] TUI-SESSION-001 — Expose session creation, opening, switching, naming,
  deletion, automatic names, and recovery errors through bounded native flows.
- [~] TUI-CMD-001 — Complete palette filtering, completion, arguments, nested
  pickers, keyboard/mouse behavior, and required command sources.
- [ ] TUI-CMD-002 — Add the R1 command surfaces for settings, MCP, logout, and
  release/update information with clear availability rules.
- [ ] TUI-SET-001 — Present immediate setting changes, validation, and inline
  persistence failures. Depends on `CORE-SET-001`.
- [ ] TUI-SET-002 — Expose R1 settings for default model/thinking, retry
  behavior, guided questions, preferred diff layout, and remembered workspace
  ratios. Depends on `CORE-SET-002`.

### Images and attachments

- [ ] TUI-ATT-001 — Stage image and text attachments; restore, queue, submit,
  remove, and clean them up without losing composer state. Depends on
  `CORE-ATT-001`.
- [ ] TUI-ATT-002 — Render explicit transcript image previews and open transcript
  and `show_image` images in retained workspace panes with safe fallback.
  Depends on `CORE-ATT-002`.
- [ ] TUI-ATT-003 — Preserve attachment identity and visible failure state across
  session switching, reconnect, and submission retry.

### User interaction and integrations

- [ ] TUI-INT-001 — Present confirm, input, select, and one-question guided
  question flows in the composer dock with exact cancellation behavior. Depends
  on `CORE-INT-001` and `CORE-INT-002`.
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

- [ ] TUI-MERMAID-001 — Render Mermaid diagrams inline with a safe visual
  fallback, or adopt an explicitly reviewed native equivalent.
- [ ] TUI-THREAD-001 — Present cached `#thread` suggestions, escaping, bounded
  expansion, and cancellation. Depends on `CORE-THREAD-001`.
- [ ] TUI-PAGER-001 — Implement pager sectioning, auto-open behavior, notes,
  draft attachments, restoration, submission, and failure recovery.
- [ ] TUI-SCRATCH-001 — Add a scratchpad workspace with guarded edits and
  autosave feedback. Depends on `CORE-SCRATCH-001`.
- [ ] TUI-REVIEW-001 — Extend `TUI-DIFF-001` with review target pickers,
  skipped-file navigation, full-file panes, and revision-pinned file/line/range
  notes. Depends on `CORE-REVIEW-001`.
- [ ] TUI-REVIEW-002 — Project, remove, restore, draft, and submit structured
  review attachments immediately; refresh remote data without resetting
  unchanged local state. Depends on `CORE-REVIEW-002`.
- [ ] TUI-WORK-002 — Add release-note and other approved retained workspace
  panes without duplicating server state.
- [ ] TUI-CMD-003 — Add `/pager`, `/code-review`, `/handoff`, `/tree`, and other
  commands when their owning Post-R1 capabilities are implemented.
- [ ] TUI-GH-001 — Present cached GitHub pull-request metadata and a safe
  click-through URL. Depends on `CORE-GH-001`.
- [ ] TUI-PLUGIN-001 — Present plugin header/footer contributions, theme tokens,
  hide claims, activation, failure, cleanup, reload, and restart. Depends on
  `CORE-PLUGIN-004`, `CORE-PLUGIN-005`, and `CORE-PLUGIN-006`.
- [ ] TUI-SET-003 — Expose Post-R1 pager defaults when that workflow exists.
