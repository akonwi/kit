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
- [x] parity verified
- [-] intentionally removed or superseded by an accepted decision

A feature reaches `[x]` only when its behavior is implemented in the applicable
clients, persistence/migration implications are covered, and automated or
recorded manual verification exists.

## Accepted differences

- [-] `web-tui` is removed.
- [x] OpenTUI is replaced with `vaxis/ui` as the native TUI framework.
  Remaining behavior and visual parity are tracked under Native TUI shell.
- [x] The TypeScript/Pi runtime is replaced by the Kit-private Go
  `internal/droids` SDK, and Kit sessions now host one autonomous droid with a
  dedicated SQLite Store. ADR 0006 makes that Store authoritative for turns,
  executions, messages, files, boundaries, outcomes, and droid events; Kit's
  SQLite database retains the session registry rather than durable projections.
- [x] Store new v2 runtime-owned conversation data in dedicated droid SQLite
  stores.
- [ ] Back up and idempotently migrate existing production `~/.kit` JSONL
  session data into the v2 SQLite stores.
- [ ] Subagents become concurrent supervised executions with durable mailboxes.
- [ ] Custom plugins are supported through the subprocess RPC protocol only;
  in-process TypeScript plugin loading is not restored.
- [x] Automatic context discovery uses `AGENTS.md` only; `CLAUDE.md` and
  immediate-child scanning are intentionally not restored. See ADR 0010.

## Foundation and distribution

- [x] One Go module and one `kit` executable composition root.
- [ ] CGO-free macOS and Linux release builds; Windows is unsupported.
- [~] Embedded, versioned database migrations.
- [ ] Embedded production web assets; users need no Bun/Node runtime.
- [ ] Version metadata, update checks, release notes, and release packaging.
- [ ] Install and upgrade paths for existing npm-installed users.
- [x] Initial Cobra-backed native command tree for root TUI startup, `new`,
  `sessions` (`threads` alias), `print`, `auth`, `daemon`, `version`, and help,
  with exact root/print help contracts, command-specific help coverage,
  command-local validation, verified command-layer exit behavior, and the
  internal daemon role hidden.
- [ ] Shell completion, stdio RPC, semantic web serving, `kit attach`, and
  noninteractive `kit sessions list`, `rename`, and `delete` commands.
- [ ] Cold-start and warm-attach performance acceptance checks.
- [ ] Crash-safe logs and actionable diagnostics.

## Application paths and migration

- [~] Development defaults to `~/.kit-v2` with a `KIT_HOME` override.
- [~] Store provider credentials with private permissions, locked atomic writes,
  generation-checked OAuth rotation, headless Codex login/logout, and native TUI
  entry for OpenAI/Anthropic API keys; headless API-key management, web
  presentation, and migration remain.
- [ ] Preserve user-editable settings, theme, prompt, skill, agent, template,
  MCP, and plugin-manifest surfaces.
- [ ] Inventory existing `~/.kit` data before migration.
- [ ] Create a backup before migration.
- [ ] Idempotently migrate sessions, turns, model/thinking selections,
  scratchpads, attachments needed for history, subagent records, and metadata.
- [ ] Verify migrated sessions can be opened and continued through droids.
- [ ] Preserve old data when a record cannot be migrated and report it clearly.
- [ ] Switch the production default from `~/.kit-v2` to `~/.kit` only after the
  migration and parity suite pass.

## Daemon and server lifecycle

- [x] Dispatch client and daemon roles from the same executable through a
  Cobra command tree, staging a private atomic run-directory copy before detach
  so `go run` cleanup cannot unlink the live daemon image. The internal daemon
  role remains hidden from public help.
- [~] Coordinate concurrent daemon startup with an inter-process lock.
- [~] Authenticate every local health, management, HTTP, and WebSocket request.
- [x] Atomically publish and validate PID, instance, version, protocol, and
  address metadata.
- [x] Detect stale registrations and incompatible daemon versions safely.
- [x] Implement `kit daemon start`, `status`, `stop`, and `restart`.
- [~] Keep active parent and subagent work running after all clients detach.
- [~] Distinguish detach, turn abort, ephemeral-session disposal, and daemon
  shutdown.
- [~] Support graceful shutdown and bounded forced cleanup of tools/plugins.
- [ ] Add explicit resource and backpressure limits.

## Session directory and concurrency

- [x] List, create, open, rename, delete, and resume sessions; the native TUI
  lists all saved sessions in an on-demand responsive explorer, and `kit new`
  starts it with a newly persisted session for the selected working directory
  without consulting resumable sessions. The explorer switches the local TUI
  binding to an exact selected session and renames or deletes non-attached
  sessions.
- [x] Resume the most recent session for the current cwd by default, with
  `kit new` as the explicit create-instead escape hatch.
- [x] Support exact long/unique-short session identifiers and explicit
  `--temp` sessions backed by in-memory droid stores and disposed after orderly
  foreground TUI or print exit; ADR 0009 deliberately defers logical owner
  leases for abnormal owner loss.
- [x] Persist session cwd, name, creation time, and monotonic activity time;
  accepted prompts, direct bash commands, cwd changes, and renames advance the
  activity timestamp used for directory ordering and default resume.
- [x] Persist the exact model and valid thinking-level selection at session
  creation and restore those selections while they remain supported.
- [ ] Change model and thinking level on an existing session, including
  quiescent runtime replacement, target-model context adaptation, and reported
  clamping of stale saved thinking levels.
- [~] Persist droid-owned provider usage; per-message and per-turn usage are
  durable, while a cumulative session total and protocol/client projection
  remain. Implementation is tracked in
  [`backlog/session-model-thinking-usage.md`](../backlog/session-model-thinking-usage.md).
- [~] Run different top-level sessions concurrently.
- [~] Serialize one parent run per session while preserving queue semantics.
- [ ] Allow several clients to observe/control one authoritative session.
- [x] Keep cwd, active-session, and model-selection state explicitly owned by
  managers, session runtimes, or clients rather than process-global variables.
- [x] Support persisted cwd retargeting through an explicit synchronized
  server-side workspace scope, without process-global cwd mutation or
  cross-session interference.
- [ ] Implement handoff and lineage without globally switching other clients.
- [x] Implement session explorer/picker workflows and responsive presentation;
  the native dialog loads the global directory, centers selection on the
  attached session, supports bounded keyboard/mouse navigation, prioritizes
  title, activity time, cwd, then short id as width permits, atomically replaces
  the local binding from an authoritative target snapshot, and provides
  validated retryable rename and confirmed-delete dialogs. `kit sessions`
  reuses the explorer as a bounded primary-screen mini-TUI, preserves shell
  scrollback while cleaning up its live region, permits management before
  attachment, and opens an exact selection in the normal TUI.
- [ ] Preserve scratchpad behavior across forks/handoffs.

## Agent runtime and transcript

- [x] Construct persisted Kit sessions around `internal/droids.Droid`; droids
  owns prompt admission, turns, outcomes, history, retries, compaction, tool
  loops, recovery, and abort, while Kit projects protocol views directly without
  persisting parallel run or transcript records.
- [~] Stream text, thinking, assistant messages, tool calls, tool updates, usage,
  errors, and terminal run state; droids events are projected into a bounded
  runtime-local session stream. The native TUI buffers text deltas and reveals
  assistant prose atomically on message completion while following thinking,
  complete tool plans with bounded arguments, append-only structured tool
  updates, authoritative results/details, execution state, and terminal state.
  Push subscriptions, usage events, richer error recovery, and full multi-client
  synchronization remain.
- [~] Preserve droid-owned turn and stable message identities directly;
  assistant message IDs remain stable from live start/deltas through snapshots.
- [~] Render active and historical turns consistently after reconnect/restart;
  transcript snapshots now preserve ordered content blocks, tool call/result
  identity, bounded arguments, details, errors, and stop reasons, and the native
  TUI reconstructs an attached active run from its runtime event stream. Rich
  turn entries and atomic multi-client synchronization remain.
- [ ] Steering, follow-up queueing, promotion, restoration, and generation
  guards.
- [~] Abort and cooperative cancellation through providers, tools, plugins, and
  subagents.
- [ ] Retryable provider errors with user-visible countdown/status and limits.
- [ ] Proactive and overflow-driven compaction with durable checkpoints.
- [~] Current context pressure and model limit reporting, distinct from
  cumulative provider usage; session snapshots project the estimated active
  context and model window, and the TUI renders a bare header percentage when
  meaningful.
- [~] Model selection, exact provider/model IDs, persisted session model, and
  default-model precedence.
- [~] Thinking-level discovery, selection, and persistence.
- [~] OpenAI/Anthropic API-key providers and OpenAI Codex OAuth transport with
  Kit-owned persistent credentials and refresh rotation, headless Codex device
  login/logout, and native TUI selection/login for all three providers; model
  selection and broader credential management remain.
- [ ] Automatic session naming.
- [ ] Session transcript replacement/recovery semantics.
- [ ] Message and composer history.

## Built-in coding tools

- [x] Read files with bounded output and line addressing.
- [x] Exact/surgical edit behavior and useful conflict errors.
- [x] Full-file writes with parent-directory creation.
- [x] Directory listing, glob finding, and content search.
- [x] Shell execution, cancellation, output bounds, exit status, and dynamic
  session cwd, including the synchronous sequential `change_cwd` tool; relative
  coding tools and direct bash snapshot the current workspace scope at admission.
- [~] Direct composer `!`/`!!` bash execution; included terminal results enter
  droid boundary history, while in-flight and excluded executions are transient.
- [ ] Git-aware operations used by review and workspace features.
- [ ] URL/open-browser and platform operations behind client/platform ports.
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
  state, and an undimmed modal boundary. It exposes cwd navigation, login, idle
  session-context reload, conditional abort, session exploration, quit, and
  dynamically discovered idle-only prompt commands with quoted arguments; session
  exploration opens the native saved-session listing and switches the attached
  TUI session. Completion, nested pickers, and non-prompt dynamic command
  sources remain.
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
  plugin chrome, and update action; the initial shell preserves session/model
  header and status/cwd/Git footer ownership.
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
  protocol v15 exposes retry-safe client-selected persisted or temporary
  session IDs, validated session rename, persisted cwd mutation, and
  archival/disposal deletion,
  session-scoped context reload metadata and diagnostics, renderer-safe prompt
  command catalogs and server-owned prompt-command execution, droid-owned turn
  identity, direct canonical history, context/pending boundaries, runtime stream
  synchronization metadata, bounded tool arguments, and stable live assistant
  message IDs. TypeScript contracts remain.
- [~] Snapshot plus high-water synchronization; snapshots now bind active runs
  to runtime stream identity, cursor, and replay availability. Broader
  multi-client conformance remains.
- [ ] Ordered, exactly-once client reduction with duplicate/gap handling.
- [~] Bounded event replay, resync, and snapshot fallback; session events now
  receive runtime-local sequences without SQLite writes, and local clients bind
  polling to snapshot stream metadata. Push transport and richer gap recovery
  remain.
- [ ] Paginated transcripts and generation-guarded mutable collections.
- [ ] Chunked recovery for individually oversized messages/interactions.
- [~] Command correlation and ordering relative to preceding events.
- [ ] Bounded client queues and disconnect-on-backpressure behavior.
- [~] Server/session client conformance tests shared across local and remote
  transports; the local client now consumes validated session snapshots,
  supports cancellation-safe session reload, and uses atomic ordinary and
  prompt-command admission.
- [ ] Stdio RPC bridge with protocol-clean stdout and diagnostics on stderr.
- [x] Explicit `kit print` client with piped stdin, exact/implicit/new/temporary
  session choices, name/model/thinking/cwd creation selection, foreground
  cancellation and abort, protocol-clean stdout, stable command exit codes, and
  exact final assistant text.
- [ ] `kit attach` native remote TUI.

## Remote serving and security

- [ ] Keep the private daemon listener loopback-only.
- [ ] Provide a separately configured remote listener.
- [ ] Token authentication for CLI clients and secure browser session cookies.
- [ ] Host and Origin validation for browser and WebSocket requests.
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

- [x] Server-owned global and Git-root-to-cwd `AGENTS.md` loading with bounded
  diagnostics, local-guidance priority, and documented precedence. ADR 0010
  intentionally excludes `CLAUDE.md`, siblings, and immediate-child scanning.
- [x] Explicit idle session reload applies current context, skills, prompt
  commands, and immutable tool contributions atomically, preserves droid history,
  retains the dynamic workspace scope, and forces event-stream resynchronization.
- [x] User-global and project `SKILL.md` discovery, deterministic precedence,
  bounded diagnostics, model-visible prompt summaries, source-relative location
  guidance, the reserved embedded `kit-customization` skill, and the stable
  `activate_skill` tool.
- [~] Kit user/project prompt-command discovery, frontmatter descriptions,
  quoted argument expansion, server-owned execution, reload, and native palette
  contribution are complete; compact synthetic transcript identity remains.
- [ ] Claude command compatibility and `cc:` namespacing.
- [ ] User/project subagent definition discovery.
- [x] Cwd navigation immediately retargets relative filesystem operations while
  explicit idle reload refreshes context, skills, and prompt commands from the
  new cwd.

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
  palette currently exposes cwd navigation, login, idle session-context reload,
  conditional abort, session exploration, quit, and server-discovered
  global/project prompt commands with arguments.
- [ ] Dynamic command registration with canonical ownership and generations.
- [x] `/cd <path>` changes the authoritative session workspace scope, records a
  durable user-origin cwd boundary for the droid, and offers the same relative,
  absolute, and home-path behavior as `change_cwd`.
- [x] `/login`, `/reload`, `/sessions`, `/quit`, and active-run `/abort` are
  available through the native command palette with tested availability rules.
- [ ] `/settings`, `/pager`, `/code-review`, `/handoff`, `/logout`, `/model`,
  `/name`, `/new`, `/debug`, `/tree`, `/thinking`, `/compact`, and release/MCP
  commands.
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
- [x] Turn Activity retained-pane navigation and live/completed scroll behavior;
  chips open one retained source, historical sources start at the top, live
  sources follow the bottom, and keyed rows preserve disclosure state through
  live-to-snapshot reconciliation.
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
- [ ] Proxy-first list/search/describe/call tool surface.
- [ ] Stdio and HTTP transports with lazy connection.
- [ ] Persistent metadata cache.
- [ ] OAuth browser flow, timeout, credential persistence, one retry after
  rejected saved auth, and logout.
- [ ] TUI/web status and debug surfaces.
- [ ] Git branch/dirty status and current GitHub PR metadata through `gh`.
- [ ] Asynchronous, cached, silent-degradation integration behavior.
- [ ] Update checks and paginated release history without delaying startup.

## Headless and automation verification

- [x] Explicit `kit print` persisted, `--new`, and process-local `--temp`
  behavior, including artifact-free orderly disposal.
- [x] `--session`, `--new`, `--temp`, `--name`, `--model`, `--thinking`,
  `--cwd`, option delimiter, and piped stdin behavior.
- [ ] Headless-safe built-ins/plugins and unavailable interaction semantics.
- [x] Final stdout, diagnostics stderr, nonzero error/abort exits, and
  foreground cancellation.
- [ ] End-to-end SIGINT/SIGTERM exit-code verification.
- [ ] Long-lived stdio RPC framing, malformed input recovery, async acceptance,
  and settlement events.
- [x] Headless `kit auth login openai-codex`, non-secret `kit auth status`, and
  `kit auth logout openai-codex` commands.
- [~] Authenticated manual smoke testing covers native temporary print
  execution and artifact-free disposal; tools, plugins, signals, and subagents
  remain.

## Quality gate

- [x] `gofmt`, `go vet`, `go test`, and `go build` pass for all Go packages.
- [ ] Browser format, lint, typecheck, unit, integration, and accessibility tests
  pass.
- [~] Current CLI, daemon, session, session-client, and TUI test suites pass
  under the race detector; subagent/plugin concurrency suites remain future
  work.
- [ ] Protocol fuzz/property tests cover malformed and adversarial records.
- [ ] SQLite migration tests cover empty, current, repeated, partial-failure, and
  imported-data paths.
- [ ] macOS and Linux release artifacts pass launch and daemon lifecycle smoke
  tests.
- [ ] No current user data is mutated before explicit migration.
- [~] README, ADRs 0008/0009/0010, native CLI help, session context guidance,
  session cwd, skills, and prompt-command documentation match the implemented
  surface;
  feature documentation for deferred RPC, web, plugin, and interaction behavior
  remains to be reconciled.

## Rolling target-branch delta

Add every relevant behavior merged after `5c6e112` here. Do not silently expand
or ignore the parity target.

Last audited against production `main` at `c9abdf2` (Kit v0.35.1). Version-only
commits are omitted.

| Change | Decision | Tracking |
| --- | --- | --- |
| Keep assistant reasoning visible alongside tool activity (`642638d`) | Ported | Native TUI shell transcript/thinking |
| Present confirm, input, select, and guided questions in the composer dock (`9f7bbed`) | Port | Native TUI interaction surfaces; User interaction and reading workflows |
| Add validated `show_image` and explicit transcript image previews (`a2c7434`) | Port | Built-in coding tools; Native TUI shell image capabilities |
| Open transcript images in retained workspace panes (`e6c181f`) | Port | Native TUI workspace panes |
| Submit structured code-review feedback without a generic prompt preamble (`0fc9f94`) | Port | Code review and workspace files |
| Upgrade the TypeScript Pi runtime to 0.85 (`1971d71`) | Superseded | Accepted native droids runtime difference; ADRs 0001 and 0006 |
