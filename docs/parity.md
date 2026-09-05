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
- [ ] The TypeScript/Pi runtime is replaced by the Kit-private Go
  `internal/droids` core seeded from the standalone droids repository.
- [ ] Runtime-owned JSONL session data moves to SQLite through an idempotent,
  backed-up migration.
- [ ] Subagents become concurrent supervised executions with durable mailboxes.
- [ ] Custom plugins are supported through the subprocess RPC protocol only;
  in-process TypeScript plugin loading is not restored.

## Foundation and distribution

- [~] One Go module and one `kit` executable composition root.
- [ ] CGO-free macOS and Linux release builds; Windows is unsupported.
- [~] Embedded, versioned database migrations.
- [ ] Embedded production web assets; users need no Bun/Node runtime.
- [ ] Version metadata, update checks, release notes, and release packaging.
- [ ] Install and upgrade paths for existing npm-installed users.
- [ ] Shell completion and help output for the final CLI surface.
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

- [~] Dispatch client and daemon roles from the same executable.
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

- [~] List, create, open, rename, delete, and resume sessions.
- [~] Resume the most recent session for the current cwd by default.
- [ ] Support exact long/short session identifiers and explicit ephemeral
  sessions.
- [~] Persist cwd, name, parent lineage, model, thinking level, timestamps, and
  usage metadata.
- [~] Run different top-level sessions concurrently.
- [~] Serialize one parent run per session while preserving queue semantics.
- [ ] Allow several clients to observe/control one authoritative session.
- [ ] Remove all process-global cwd, active-session, and model-cache state.
- [ ] Support cwd retargeting with explicit server-side workspace context.
- [ ] Implement handoff and lineage without globally switching other clients.
- [ ] Implement session explorer/picker workflows and responsive presentation.
- [ ] Preserve scratchpad behavior across forks/handoffs.

## Agent runtime and transcript

- [~] Construct persisted Kit sessions around `internal/droids.Droid`.
- [~] Stream text, thinking, assistant messages, tool calls, tool updates, usage,
  errors, and terminal run state; droids events are now projected into a bounded
  durable session journal and the native TUI follows text, thinking, complete
  tool plans with bounded arguments, append-only structured tool updates,
  authoritative results/details, execution state, and terminal state. Push
  subscriptions, usage events, richer error recovery, and full multi-client
  synchronization remain.
- [~] Persist explicit turn and stable message identities; assistant message IDs
  now remain stable from live start/deltas through the persisted snapshot.
- [~] Render active and historical turns consistently after reconnect/restart;
  transcript snapshots now preserve ordered content blocks, tool call/result
  identity, bounded arguments, details, errors, and stop reasons, and the native
  TUI reconstructs an attached active run from its durable event journal. Rich
  turn entries and atomic multi-client synchronization remain.
- [ ] Steering, follow-up queueing, promotion, restoration, and generation
  guards.
- [~] Abort and cooperative cancellation through providers, tools, plugins, and
  subagents.
- [ ] Retryable provider errors with user-visible countdown/status and limits.
- [ ] Proactive and overflow-driven compaction with durable checkpoints.
- [~] Context usage and model limit reporting; session snapshots project the
  latest persisted usage and model window, and the TUI renders a bare header
  percentage when meaningful.
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
- [x] Shell execution, cancellation, output bounds, exit status, and cwd.
- [ ] Direct composer `!`/`!!` bash execution and per-session history.
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
- [~] Transcript with selectable Markdown, streaming output, code, tool drawers,
  and compact historical entries; the native TUI now preserves assistant prose,
  consolidates tool work into one-line chips, and opens stable Activity sections
  with lifecycle glyphs, disclosure rows, and bounded nested output while keeping
  live thinking/turn state above the composer. Markdown, code treatment, and
  enriched tool rendering remain.
- [ ] Mermaid inline rendering and safe visual fallback, or an explicitly
  reviewed native equivalent.
- [~] Fixed composer with multiline editing, cursor behavior, drafts, history,
  attachments, pending queue, and abort state; the focused full-width composer
  starts at one row, grows to ten rows with multiline input, submits prompts,
  preserves text while busy, and exposes abort state.
- [~] Command palette with filtering, completion, arguments, nested pickers,
  keyboard, and mouse behavior; the initial single-ranked palette opens from
  `Ctrl+P` or an empty-composer `/`, supports fuzzy matching, identity-stable
  wraparound navigation, Enter/Escape, full-row mouse activation, a quiet empty
  state, and an undimmed modal boundary. Completion, arguments, nested pickers,
  and dynamic command sources remain.
- [ ] Layered focus, configurable intent keybindings, conflict reporting, and
  overlay precedence.
- [~] Toasts, confirmation/input/select dialogs, fatal/error screens, and
  interactive tool surfaces; the initial shell includes startup failure,
  three-provider selection, obscured API-key entry, and cancellable Codex
  device-flow surfaces.
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
- [ ] Clipboard, terminal title, notifications, image capabilities, and clean
  terminal restoration.

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
  protocol v5 adds validated structured transcript content, explicit bounded
  tool-argument truncation, and stable live assistant message IDs. TypeScript
  contracts remain.
- [ ] Snapshot plus high-water synchronization without listener races.
- [ ] Ordered, exactly-once client reduction with duplicate/gap handling.
- [~] Bounded event journal, replay, resync, and snapshot fallback; session
  events now receive durable monotonic SQLite sequences with bounded retention,
  and local run clients poll/replay from a cursor. Atomic snapshot subscription,
  gap recovery, and push transport remain.
- [ ] Paginated transcripts and generation-guarded mutable collections.
- [ ] Chunked recovery for individually oversized messages/interactions.
- [~] Command correlation and ordering relative to preceding events.
- [ ] Bounded client queues and disconnect-on-backpressure behavior.
- [~] Server/session client conformance tests shared across local and remote
  transports; the local client now consumes validated session snapshots and an
  atomic prompt-admission endpoint.
- [ ] Stdio RPC bridge with protocol-clean stdout and diagnostics on stderr.
- [~] Print client with piped stdin, exit codes, session options, and exact final
  assistant text.
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

- [ ] Global/project walk-up `AGENTS.md` and `CLAUDE.md` context loading with
  documented precedence.
- [ ] Immediate-child context discovery and debug visibility.
- [ ] Skill discovery, prompt summaries, `activate_skill`, and source-relative
  file behavior.
- [ ] Kit user/project prompt commands, frontmatter, quoted argument expansion,
  and compact transcript identity.
- [ ] Claude command compatibility and `cc:` namespacing.
- [ ] User/project subagent definition discovery.
- [ ] Reload behavior after cwd/config changes.

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
  palette currently exposes login, conditional abort, and quit.
- [ ] Dynamic command registration with canonical ownership and generations.
- [ ] `/cd`, `/settings`, `/pager`, `/code-review`, `/handoff`, `/login`,
  `/logout`, `/model`, `/name`, `/new`, `/reload`, `/debug`, `/sessions`,
  `/tree`, `/thinking`, `/quit`, `/compact`, and release/MCP commands.
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

- [ ] `kit -p` persistent and `--no-session` behavior.
- [ ] `--session`, `--model`, option delimiter, and piped stdin behavior.
- [ ] Headless-safe built-ins/plugins and unavailable interaction semantics.
- [ ] Final stdout, diagnostics stderr, nonzero error/abort exits, and signals.
- [ ] Long-lived stdio RPC framing, malformed input recovery, async acceptance,
  and settlement events.
- [~] Headless Codex device-login, non-secret auth status, and logout commands.
- [ ] Authenticated manual smoke test covering prompts, tools, plugins, signals,
  subagents, and ephemeral storage.

## Quality gate

- [ ] `gofmt`, `go vet`, `go test`, and `go build` pass for all Go packages.
- [ ] Browser format, lint, typecheck, unit, integration, and accessibility tests
  pass.
- [ ] Race detector passes for daemon/session/subagent/plugin concurrency tests.
- [ ] Protocol fuzz/property tests cover malformed and adversarial records.
- [ ] SQLite migration tests cover empty, current, repeated, partial-failure, and
  imported-data paths.
- [ ] macOS and Linux release artifacts pass launch and daemon lifecycle smoke
  tests.
- [ ] No current user data is mutated before explicit migration.
- [ ] Documentation and help match shipped behavior.

## Rolling target-branch delta

Add every relevant behavior merged after `5c6e112` here. Do not silently expand
or ignore the parity target.

| Change | Decision | Tracking |
| --- | --- | --- |
| _None recorded yet_ | — | — |
