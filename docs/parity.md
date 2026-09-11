# Kit v2 parity ledger

## Purpose

Kit v2 is a full replacement. The walking skeleton may be intentionally thin,
but this branch is not mergeable until required behavior and UX are at parity
with the previous implementation.

The fixed starting baseline is Git commit [`5c6e112`](https://github.com/akonwi/kit/tree/5c6e112).
Features added to the merge target after that commit must be entered in the
rolling-delta section and explicitly ported, deferred, or rejected before merge.

Historical documentation in `app/docs/features/` is a behavioral reference,
not an architectural constraint. The v2 architecture is
[`adrs/0001-native-go-architecture.md`](adrs/0001-native-go-architecture.md).

## Status legend

- [ ] not started
- [~] in progress or foundation only
- [-] intentionally removed or superseded by an accepted decision

Completed features are removed once their behavior is implemented in the
applicable clients, persistence and migration implications are covered, and
automated or recorded manual verification exists.

## Accepted differences

- [-] `web-tui` is removed.
- [ ] Back up and idempotently migrate existing production `~/.kit` JSONL
  session data into the v2 SQLite stores.
- [ ] Subagents become concurrent supervised executions with durable mailboxes.
- [ ] Custom plugins are supported through the subprocess RPC protocol only;
  in-process TypeScript plugin loading is not restored.

## Foundation and distribution

- [~] Replace the legacy release workflow with CGO-free macOS and Linux builds;
  the native Go executable already cross-builds without CGO, and Windows remains
  unsupported.
- [ ] Embedded production web assets; users need no Bun/Node runtime.
- [ ] Version metadata, update checks, release notes, and release packaging.
- [ ] Install and upgrade paths for existing npm-installed users.
- [ ] Shell completion, stdio RPC, semantic web serving, `kit attach`, and
  noninteractive `kit sessions list`, `rename`, and `delete` commands.
- [ ] Cold-start and warm-attach performance acceptance checks.
- [ ] Crash-safe logs and actionable diagnostics.

## Application paths and migration

- [~] Store provider credentials with private permissions, locked atomic writes,
  generation-checked Codex and Anthropic OAuth rotation, headless Codex
  login/logout, and native TUI entry for OpenAI/Anthropic API keys plus Claude
  Pro/Max browser OAuth; headless API-key/Anthropic management, web presentation,
  and migration remain.
- [ ] Preserve user-editable settings, theme, agent, template, MCP, and
  plugin-manifest surfaces.
- [ ] Inventory existing `~/.kit` data before migration.
- [ ] Create a backup before migration.
- [ ] Idempotently migrate sessions, turns, model/thinking selections,
  scratchpads, attachments needed for history, subagent records, and metadata.
- [ ] Verify migrated sessions can be opened and continued through droids.
- [ ] Preserve old data when a record cannot be migrated and report it clearly.
- [ ] Switch the production default from `~/.kit-v2` to `~/.kit` only after the
  migration and parity suite pass.

## Daemon and server lifecycle

- [x] Authenticate local SSE requests consistently with the authenticated
  health, management, and session HTTP routes.
- [ ] Keep active subagent work running after all clients detach.
- [~] Support graceful shutdown and bounded forced cleanup of tools/plugins.
- [~] Complete resource and backpressure limits across every runtime and client;
  HTTP, event replay/subscriptions, direct bash, MCP results, and several other
  current boundaries are already bounded.

## Session directory and concurrency

- [~] Run different top-level sessions concurrently.
- [~] Serialize one parent run per session while preserving queue semantics.
- [ ] Allow several clients to observe/control one authoritative session.
- [ ] Implement handoff and lineage without globally switching other clients.
- [ ] Preserve scratchpad behavior across forks/handoffs.

## Agent runtime and transcript

- [~] Stream text, thinking, assistant messages, tool calls, tool updates, usage,
  errors, and terminal run state; droids events are projected into a bounded
  runtime-local session stream. The native TUI buffers text deltas and reveals
  assistant prose atomically on message completion while following thinking,
  complete tool plans with bounded arguments, append-only structured tool
  updates, authoritative results/details, absolute cumulative usage, execution
  state, and terminal state. Session-bound SSE push subscriptions now replace
  event polling; richer error recovery and full multi-client synchronization
  remain.
- [~] Render active and historical turns consistently after reconnect/restart;
  transcript snapshots now preserve ordered content blocks, tool call/result
  identity, bounded arguments, details, errors, and stop reasons, and the native
  TUI reconstructs an attached active run from its runtime event stream. Rich
  turn entries and atomic multi-client synchronization remain.
- [~] Expose droids' bounded durable steering and generation guards through Kit,
  then add follow-up queueing, promotion, and restoration.
- [~] Abort and cooperative cancellation through providers, tools, plugins, and
  subagents.
- [~] Project droids' bounded retry scheduling into user-visible provider-error
  countdown and status surfaces.
- [~] Proactive and overflow-driven compaction with durable checkpoints; droids
  performs threshold-driven automatic compaction and protocol v19 projects its
  pending, completed, and failed lifecycle so the native TUI provides notice.
- [~] OpenAI/Anthropic API-key providers, OpenAI Codex OAuth, and Claude Pro/Max
  OAuth transport with Kit-owned persistent credentials and refresh rotation,
  headless Codex device login/logout, and native TUI selection/login; broader
  credential management remains.
- [ ] Automatic session naming.
- [ ] Session transcript replacement/recovery semantics.
- [ ] Message and composer history.

## Built-in coding tools

- [~] Move the native TUI's existing validated URL opening behind
  client/platform ports.
- [ ] `show_image` local-image validation and transcript presentation.
- [ ] Attachment/image inputs with validation and provider capability handling.
- [ ] Tool approval/interceptor behavior and remote interaction routing.

## Native TUI shell

- [~] Establish the v2 design language and recreate Kit's semantic theme,
  hierarchy, spacing, and terminal capability behavior in vaxis/ui. Current-UI
  research and first-run/auth explorations live under `docs/design/`; the
  viewport-native vaxis shell, terminal-derived base theme, empty/error states,
  and responsive auth surfaces are implemented. Full semantic theme parity and
  user overrides remain.
- [~] Transcript with selectable Markdown, atomically revealed assistant prose,
  code, tool drawers, and compact historical entries; mouse selection and
  `Super+C` copying are wired through the native shell. Text deltas remain in the
  runtime stream but pending prose is not rendered. The native TUI consolidates
  tool work into one-line chips and opens stable Activity sections with lifecycle
  glyphs, disclosure rows, bounded nested output, read/write code views, and
  semantic edit diffs. Live thinking is Markdown-rendered in the fixed status
  slot and retained as full Markdown evidence in tool-backed Activity without
  creating a zero-tool drawer. A cached
  Goldmark CommonMark/GFM pipeline now renders headings, inline emphasis/code,
  literal safe links, lists/tasks, quotes, rules, tables, and language-preserving
  fenced code in transcript and Activity prose/thinking. Syntax
  highlighting and drag-safe in-app link activation remain.
- [ ] Mermaid inline rendering and safe visual fallback, or an explicitly
  reviewed native equivalent.
- [~] Fixed composer with multiline editing, cursor behavior, drafts, history,
  attachments, pending queue, and abort state; the focused full-width composer
  starts at one row, grows to ten rows with multiline input, accepts bracketed
  paste without triggering commands or submission, submits prompts, preserves
  text while busy, and exposes abort state.
- [~] Command palette with filtering, completion, arguments, nested pickers,
  keyboard, and mouse behavior; the initial single-ranked palette opens from
  `Ctrl+P` or an empty-composer `/`, supports fuzzy matching, identity-stable
  wraparound navigation, Enter/Escape, full-row mouse activation, a quiet empty
  state, and an undimmed modal boundary. It exposes cwd navigation, login,
  active-safe session-context reload and naming, searchable `/model` and
  supported-level `/thinking` selectors, explicit `/compact`, always-available
  `/debug` session details with live cumulative usage, session exploration,
  quit, and
  dynamically discovered idle-only prompt commands with quoted arguments; session exploration opens the
  native saved-session listing and switches the attached TUI session. Completion,
  nested pickers, and non-prompt dynamic command sources remain.
- [ ] Layered focus, configurable intent keybindings, conflict reporting, and
  overlay precedence.
- [~] Toasts, confirmation/input/select dialogs, fatal/error screens, and
  interactive tool surfaces; the native toast stack presents stacked,
  auto-expiring info/warning/error feedback above all overlays with semantic
  borders and 300 ms eased slide-in motion; only persistent toasts expose
  manual dismissal. Cwd navigation shows a normal warning suggesting `/reload`
  when agent context should be refreshed. Copy and browser/device-code feedback
  use it.
  The initial shell also includes startup failure, three-provider selection,
  obscured API-key entry, and cancellable Codex device-flow surfaces.
- [~] Wide split workspace, draggable remembered ratio, narrow tabs, retained
  pane state, and focus cycling; Activity now uses a singleton responsive host
  with split layout at 125+ columns and labeled narrow tabs. Draggable remembered
  ratios and the general pane registry remain.
- [~] Activity, Scratchpad, Code Review, file, subagent, MCP, release-note, and
  other registered workspace panes; the first native Activity pane opens from
  transcript chips with retained source replacement and responsive layout.
- [~] Header/footer status, model/thinking/context indicators, VCS/PR location,
  plugin chrome, and update action; the shell preserves session/model header and
  status/cwd/Git footer ownership, with compact hoverable model/thinking controls
  sharing the command selector paths and width-aware complete-segment hiding.
- [~] Clipboard, terminal title, notifications, image capabilities, and clean
  terminal restoration; selectable transcript and composer text copy through
  the terminal clipboard, while terminal chrome now shows idle, animated
  Braille running, and `?` agent-feedback states, maps those states to
  removed/indeterminate/paused Ghostty progress, emits completion attention,
  and restores idle state on exit. Image
  capabilities and remaining platform integration work remain.

## Semantic web client

- [ ] Build Solid/Mica assets and embed them in the Go executable.
- [ ] Same-origin CSP, untrusted-text rendering, authenticated assets/APIs, and
  no CDN dependency.
- [ ] Connection phases, synchronization, ordered reduction, reconnect, replay,
  snapshot fallback, and stale async-result guards.
- [ ] Transcript, streaming activity, tool state, composer, queue, abort, model,
  thinking, session naming, and command palette.
- [ ] Native confirm, input, select, and guided-question interactions.
- [ ] Attachments with upload validation, quotas, authenticated reads, and
  responsive previews.
- [ ] Responsive mobile layout, software-keyboard handling, safe-area support,
  touch targets, and coarse-pointer submission behavior.
- [ ] Workspace panes for activity, scratchpad, code review, and subsequent
  parity-required surfaces.
- [ ] Browser-local theme choice and persistent per-session drafts.
- [ ] Accessibility and keyboard verification.

## Protocol and clients

- [~] Versioned server capability negotiation.
- [~] Separate server-scoped and immutable session-scoped APIs.
- [~] Canonical wire-safe records with runtime validation in Go and TypeScript;
  protocol v19 exposes retry-safe client-selected persisted or temporary
  session IDs, validated session rename, persisted cwd mutation, and
  archival/disposal deletion,
  session-scoped context reload metadata and diagnostics, renderer-safe prompt
  command catalogs and server-owned prompt-command execution, droid-owned turn
  identity, direct canonical history, context/pending boundaries, runtime stream
  synchronization metadata, bounded tool arguments, stable live assistant
  message IDs, validated cumulative usage snapshots/updates, model capabilities,
  revision-guarded session configuration, idempotent explicit compaction, and
  session/cwd-correlated volatile VCS status. TypeScript contracts remain.
- [~] Snapshot plus high-water synchronization; snapshots now bind active runs
  to runtime stream identity, cursor, and replay availability. Broader
  multi-client conformance remains.
- [~] Complete ordered, exactly-once client reduction across transports; current
  protocol validation and the local reducer reject duplicate, gapped, and
  out-of-order event batches.
- [~] Bounded event replay, resync, and snapshot fallback; session events now
  receive runtime-local sequences without SQLite writes, and local clients bind
  SSE delivery to snapshot stream metadata under ADR 0011. Richer gap recovery
  remains.
- [ ] Paginated transcripts and generation-guarded mutable collections.
- [ ] Chunked recovery for individually oversized messages/interactions.
- [~] Command correlation and ordering relative to preceding events.
- [ ] Bounded client queues and disconnect-on-backpressure behavior.
- [~] Server/session client conformance tests shared across local and remote
  transports; the local client now consumes validated session snapshots,
  supports cancellation-safe session reload, and uses atomic ordinary and
  prompt-command admission.
- [ ] Stdio RPC bridge with protocol-clean stdout and diagnostics on stderr.
- [ ] `kit attach` native remote TUI.

## Remote serving and security

- [ ] Provide a separately configured remote listener.
- [ ] Token authentication for CLI clients and secure browser session cookies.
- [ ] Host and Origin validation for browser requests and SSE connections.
- [ ] Safe loopback defaults and explicit non-loopback/insecure choices.
- [ ] Reverse-proxy/tunnel deployment documentation; Kit does not terminate TLS.
- [ ] Single-user semantics throughout; no account or tenant APIs.
- [ ] Request size, upload, connection, interaction, and memory bounds.
- [ ] Ensure remote clients never execute workspace operations on the client
  machine accidentally.

## External plugins

- [ ] Preserve manifest v1 discovery and validation for user and project
  plugins.
- [ ] Launch without a shell, use installation cwd, reserve stdout for JSON-RPC,
  and continuously drain bounded stderr diagnostics.
- [ ] Full-duplex JSON-RPC request correlation and cancellation.
- [ ] Initialization/version negotiation and deterministic load order.
- [ ] Per-running-session plugin process instances.
- [ ] Commands, tools, tool-call interception, system-prompt slot, subagent
  definitions, lifecycle events, UI requests, URL opening, and message submit.
- [ ] Header/footer contributions, theme tokens, hide claims, and activation.
- [ ] Atomic contribution cleanup and typed active-call failure after crash.
- [ ] Explicit reload/restart and graceful/forced shutdown deadlines.
- [ ] Headless behavior and clean stderr/stdout boundaries.
- [ ] Protocol schemas, fixtures, examples, and language-neutral conformance
  tests.
- [ ] Preserve the documented trusted-code/non-sandbox security model.

## Subagents

- [ ] Discover Kit and compatibility agent definitions with deterministic
  precedence.
- [ ] Start a durable concurrent execution and return its task identity without
  blocking for completion.
- [ ] One independently cancellable context, droids runtime, transcript, event
  stream, and status per conversation.
- [ ] Bounded global/per-session concurrency.
- [ ] Inspect, continue/message, wait, cancel, and dismiss lifecycle operations.
- [ ] Persist full subagent activity without injecting its transcript into the
  parent model context.
- [ ] Deliver completion through the durable parent mailbox at a safe turn
  boundary.
- [ ] Do not automatically start a parent model call when completion arrives
  while idle.
- [ ] Mark in-flight work interrupted after daemon failure and expose explicit
  recovery/retry.
- [ ] Roster and retained transcript workspace tabs in TUI and web.
- [ ] Compact delegation markers and live status in parent activity surfaces.
- [ ] Prevent nested delegation until deliberately designed.

## Guidance and reusable prompts

- [~] Kit user/project prompt-command discovery, frontmatter descriptions,
  quoted argument expansion, server-owned execution, reload, and native palette
  contribution are complete; compact synthetic transcript identity remains.
- [ ] Claude command compatibility and `cc:` namespacing.
- [ ] User/project subagent definition discovery.

## Composer references and attachments

- [ ] Lazy `@file` suggestions respecting Git and Kit ignore rules.
- [ ] Provisional trigger timing, `@@` escaping, cancellation, replacement, and
  invalidation behavior.
- [ ] Cached `#thread` suggestions, `##` escaping, active-session exclusion, and
  bounded expansion.
- [ ] Image and text attachment staging, restore, queue, submit, transcript, and
  cleanup behavior.
- [ ] Structured code-review and pager-feedback attachments.
- [ ] Revision-staleness protection for full-file feedback attachments.

## Commands, settings, and themes

- [~] Core command catalog and transport-neutral command subset; the native
  palette currently exposes cwd navigation, login, reload, model/thinking
  configuration, session naming, explicit compaction, session diagnostics,
  session exploration, quit, and server-discovered global/project prompt
  commands with arguments.
- [ ] Dynamic command registration with canonical ownership and generations.
- [ ] `/settings`, `/pager`, `/code-review`, `/handoff`, `/logout`, `/new`,
  `/tree`, and release/MCP commands.
- [ ] Immediate settings application, validation, atomic persistence, and inline
  save errors.
- [ ] Theme discovery/selection and current custom-theme compatibility.
- [ ] Default model, diff view, guided questions, auto naming, pager, retry,
  workspace ratio, and keybinding settings.
- [ ] Markdown template overrides with project/global precedence.
- [ ] Configurable keybinding catalog, disabling, sequences, layer precedence,
  command bindings, and conflict diagnostics.

## User interaction and reading workflows

- [ ] `confirm_from_user`, `input_from_user`, and `select_from_user` with exact
  cancellation result shapes.
- [ ] `guided_questions` one-question flow with text/select/multiselect/boolean,
  navigation, structured answers, and policy guidance.
- [ ] Multi-client first-valid-response interaction resolution.
- [ ] Pending interaction replay/pagination across reconnects.
- [ ] Pager sectioning, auto-open setting, notes, draft attachment, restore,
  submit, and failure recovery.
- [ ] Scratchpad guarded reads/edits, the core `edit_scratchpad` tool, autosave,
  context injection, and fork/handoff copying.

## Code review and workspace files

- [ ] Working-tree review includes staged, unstaged, and representable untracked
  files.
- [ ] Changed-file tree, whole-file diff, unified/split views, skipped sections,
  navigation, and responsive layouts.
- [ ] File, line, and same-side range notes with inline editors.
- [ ] Immediate structured attachment projection, submission, removal, restore,
  and per-session draft behavior.
- [ ] Working, commit, and branch targets with pinned full revisions and target
  picker behavior.
- [ ] Per-target drafts and stale-revision submission guards.
- [ ] Full-file panes with syntax highlighting, repository drawer, deduplication,
  revision-pinned comments, and feedback attachments.
- [ ] Automatic refresh without resetting unchanged local state.
- [ ] Browser code-review parity where currently supported.

## MCP and integrations

- [ ] Merge all documented MCP config locations with precedence.
- [~] Wire the private droids proxy-first list/search/describe/call foundation
  into Kit session runtime and configuration surfaces.
- [~] Add configured stdio and HTTP transport factories around the existing lazy
  MCP connection lifecycle.
- [ ] Persistent metadata cache.
- [ ] OAuth browser flow, timeout, credential persistence, one retry after
  rejected saved auth, and logout.
- [ ] TUI/web status and debug surfaces.
- [~] Native TUI footer local VCS status; server-owned structured
  repository/head/dirty probes render branch, detached/unborn head, and `*`
  dirty state with bounded Git subprocesses and silent fallback. The TUI
  refreshes after attachment, session/cwd changes, tool and bash completion,
  agent settlement, and a five-second fallback poll. Production's debounced
  filesystem watcher for lower-latency external changes remains.
- [ ] Current GitHub PR metadata through `gh`, with asynchronous cached refresh,
  a click-through PR URL, and silent degradation when unavailable.
- [ ] Update checks and paginated release history without delaying startup.

## Headless and automation verification

- [ ] Headless-safe built-ins/plugins and unavailable interaction semantics.
- [ ] End-to-end SIGINT/SIGTERM exit-code verification.
- [ ] Long-lived stdio RPC framing, malformed input recovery, async acceptance,
  and settlement events.
- [~] Authenticated manual smoke testing covers native temporary print
  execution and artifact-free disposal; tools, plugins, signals, and subagents
  remain.

## Quality gate

- [ ] Browser format, lint, typecheck, unit, integration, and accessibility tests
  pass.
- [~] Current CLI, daemon, session, session-client, and TUI test suites pass
  under the race detector; subagent/plugin concurrency suites remain future
  work.
- [ ] Protocol fuzz/property tests cover malformed and adversarial records.
- [~] Extend SQLite migration tests beyond empty databases and versioned schema
  upgrades to repeated, partial-failure, and imported-data paths.
- [ ] macOS and Linux release artifacts pass launch and daemon lifecycle smoke
  tests.
- [ ] No current user data is mutated before explicit migration.
- [~] README, ADRs 0008/0009/0010, native CLI help, session context guidance,
  session cwd, skills, and prompt-command documentation match the implemented
  surface;
  feature documentation for deferred RPC, web, plugin, and interaction behavior
  remains to be reconciled.

## Rolling target-branch delta

Add every unresolved or intentionally superseded behavior merged after
`5c6e112` here. Remove ported entries once parity is verified; do not silently
expand or ignore the parity target.

Last audited against production `main` at `c9abdf2` (Kit v0.35.1). Version-only
commits are omitted.

| Change | Decision | Tracking |
| --- | --- | --- |
| Present confirm, input, select, and guided questions in the composer dock (`9f7bbed`) | Port | Native TUI interaction surfaces; User interaction and reading workflows |
| Add validated `show_image` and explicit transcript image previews (`a2c7434`) | Port | Built-in coding tools; Native TUI shell image capabilities |
| Open transcript images in retained workspace panes (`e6c181f`) | Port | Native TUI workspace panes |
| Submit structured code-review feedback without a generic prompt preamble (`0fc9f94`) | Port | Code review and workspace files |
| Upgrade the TypeScript Pi runtime to 0.85 (`1971d71`) | Superseded | Accepted native droids runtime difference; ADRs 0001 and 0006 |
