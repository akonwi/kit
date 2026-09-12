# Core and protocol roadmap

This ledger owns client-neutral behavior. A client roadmap may depend on these
IDs but must not redefine server, persistence, or protocol semantics.

## R1 required

### Production data and configuration

- [ ] CORE-MIG-001 — Inventory production `~/.kit`, create a backup, and perform
  an idempotent migration before v2 reads or mutates production state.
- [ ] CORE-MIG-002 — Migrate sessions, turns, model/thinking selections,
  attachments needed for history, scratchpads, subagent records, and metadata;
  migrated sessions must open and continue through droids.
- [ ] CORE-MIG-003 — Preserve records that cannot be migrated and report each
  failure clearly without corrupting old or newly migrated state.
- [ ] CORE-MIG-004 — Preserve supported user settings, themes, agent definitions,
  prompt templates, MCP configuration, and plugin manifests with documented
  precedence; deferred surfaces remain untouched and discoverable Post-R1.
- [ ] CORE-MIG-005 — Switch the production default from `~/.kit-v2` to `~/.kit`
  only through the explicit migration and release process.
- [~] CORE-AUTH-001 — Complete headless API-key and Anthropic credential
  management, credential migration, and consistent private, locked, atomic,
  generation-checked storage for every supported provider.
- [ ] CORE-SET-001 — Validate and atomically persist shared settings, apply
  changes immediately where safe, and return actionable save errors.
- [ ] CORE-SET-002 — Persist and resolve R1 defaults for model/thinking
  selection, retry behavior, guided questions, diff layout, and workspace layout
  without client-local drift.

### Daemon, sessions, and runtime

- [~] CORE-LIFE-001 — Shut down gracefully and bound forced cleanup of providers,
  tools, MCP connections, and other owned child work.
- [~] CORE-LIFE-002 — Enforce explicit resource and backpressure limits across
  HTTP, event replay and subscriptions, direct tools, MCP, and clients.
- [ ] CORE-LIFE-003 — Produce crash-safe logs and actionable diagnostics without
  leaking credentials or protocol output.
- [ ] CORE-LIFE-004 — Meet documented cold-start and warm-attach acceptance
  thresholds on supported release platforms.
- [ ] CORE-SESSION-001 — Let multiple clients observe and control one
  authoritative session without lost updates or client-global active-session
  state.
- [~] CORE-RUN-001 — Complete streaming and recovery semantics for text,
  thinking, messages, tool activity, usage, provider errors, terminal state, and
  reconnecting clients.
- [~] CORE-RUN-002 — Reconstruct active and historical turns consistently after
  reconnect and restart, including rich ordered content and tool identity.
- [~] CORE-RUN-003 — Propagate abort and cooperative cancellation through
  providers, tools, MCP, and subagents with deterministic terminal state.
- [~] CORE-RUN-004 — Expose bounded provider retry countdowns and recovery state
  to clients.
- [~] CORE-RUN-005 — Persist proactive and overflow-driven compaction
  checkpoints and expose pending, completed, and failed lifecycle state.
- [ ] CORE-SESSION-002 — Automatically assign useful session names without
  overwriting explicit user names.
- [ ] CORE-SESSION-003 — Define transcript replacement and corruption-recovery
  semantics.

### Protocol and shared clients

- [~] CORE-PROTO-001 — Negotiate protocol versions and capabilities with
  canonical wire-safe records validated at every boundary.
- [~] CORE-PROTO-002 — Keep server-scoped and immutable session-scoped APIs
  separate and preserve revision/generation guards for mutable state.
- [~] CORE-PROTO-003 — Complete snapshot/high-water synchronization, ordered
  exactly-once reduction, replay, resync, and snapshot fallback across supported
  local transports.
- [ ] CORE-PROTO-004 — Paginate transcripts and mutable collections and recover
  records too large for an individual event or response.
- [~] CORE-PROTO-005 — Correlate commands with preceding events and preserve
  deterministic admission ordering.
- [ ] CORE-PROTO-006 — Bound client queues and disconnect clients that cannot
  keep up without blocking authoritative session work.
- [~] CORE-PROTO-007 — Share conformance tests across server and session client
  implementations used by R1.

### Workspace data, tools, attachments, and interactions

- [ ] CORE-WORK-001 — Expose bounded, client-safe directory listings and file
  reads rooted in and unable to escape the session workspace, with canonical
  paths, revisions, truncation, and explicit missing, binary, permission, and
  stale-data errors.
- [ ] CORE-DIFF-001 — Expose bounded, revision-aware file and hunk diff data for
  working-tree and agent edit results without requiring clients to execute Git
  or parse presentation-oriented tool output.
- [~] CORE-TOOL-001 — Route URL opening through validated client/platform ports
  and define safe behavior when no capable client is attached.
- [ ] CORE-ATT-001 — Validate local image and attachment inputs, enforce provider
  capabilities and bounds, persist durable references, and support submission,
  restoration, transcript projection, and cleanup.
- [ ] CORE-ATT-002 — Implement validated `show_image` results and a client-safe
  transcript representation. Depends on `CORE-ATT-001`.
- [x] CORE-INT-001 — User-interaction request and result contracts. Retained for
  `WEB-INT-001`.
- [x] CORE-INT-002 — Session-owned, reconnect-safe pending interactions. Retained
  for `WEB-INT-001`.
- [ ] CORE-TOOL-002 — Implement approval/interceptor behavior without allowing a
  client to bypass server-owned tool policy.

### MCP and integrations

- [ ] CORE-MCP-001 — Merge supported MCP configuration locations with
  deterministic precedence and actionable validation errors.
- [~] CORE-MCP-002 — Complete proxy-first list/search/describe/call behavior and
  configured stdio/HTTP transports around bounded lazy connections.
- [ ] CORE-MCP-003 — Persist bounded metadata caches and invalidate them when
  configuration or authentication changes.
- [ ] CORE-MCP-004 — Support MCP OAuth, browser timeout/fallback, private
  credential persistence, one retry after rejected saved auth, and logout.
- [ ] CORE-MCP-005 — Expose canonical MCP status and diagnostics to attached and
  headless clients.
- [ ] CORE-VCS-001 — Provide bounded repository status and production-equivalent
  low-latency refresh without requiring clients to run Git themselves.

### Headless and release safety

- [ ] CORE-HEAD-001 — Define headless-safe behavior for built-ins, MCP, and
  unavailable user interactions.
- [ ] CORE-HEAD-002 — Verify SIGINT/SIGTERM cleanup and documented exit codes.
- [ ] CORE-TEST-001 — Pass the full Go build, vet, test, and race-detector gates
  required by `AGENTS.md`.
- [ ] CORE-TEST-002 — Cover malformed and adversarial protocol records with fuzz
  or property tests.
- [~] CORE-TEST-003 — Extend migration tests to imported data, repetition,
  partial failure, and recovery, and prove current user data is untouched before
  explicit migration.
- [ ] CORE-TEST-004 — Complete authenticated R1 smoke coverage for coding tools,
  attachments/images, MCP, interactions, signals, and migration.

## Post-R1

### Remote clients and automation

- [ ] CORE-REMOTE-001 — Provide a separately configured remote listener with
  token-authenticated CLI clients and secure browser sessions.
- [ ] CORE-REMOTE-002 — Validate Host and Origin, retain safe loopback defaults,
  document proxy/TLS deployment, and preserve single-user semantics.
- [ ] CORE-REMOTE-003 — Bound remote requests, uploads, connections,
  interactions, and memory, and prevent client-machine workspace execution.
- [ ] CORE-RPC-001 — Provide protocol-clean long-lived stdio RPC with malformed
  input recovery, asynchronous acceptance, settlement events, and stderr-only
  diagnostics.
- [ ] CORE-REMOTE-004 — Support `kit attach` over the public client boundary.

### External plugins

- [ ] CORE-PLUGIN-001 — Discover and validate manifest-v1 user and project
  plugins with deterministic precedence.
- [ ] CORE-PLUGIN-002 — Launch plugin processes without a shell from the
  installation working directory, reserve stdout for RPC, continuously drain
  bounded stderr, and enforce startup/shutdown deadlines.
- [ ] CORE-PLUGIN-003 — Implement full-duplex versioned RPC, request
  correlation, cancellation, crash cleanup, and per-session process ownership.
- [ ] CORE-PLUGIN-004 — Support commands, tools, interception, prompt slots,
  subagent definitions, lifecycle events, UI requests, URL opening, and message
  submission through typed contributions.
- [ ] CORE-PLUGIN-005 — Remove contributions atomically after plugin failure and
  fail active calls with typed errors rather than leaving stale ownership.
- [ ] CORE-PLUGIN-006 — Support explicit reload/restart plus graceful and forced
  shutdown deadlines.
- [ ] CORE-PLUGIN-007 — Define headless behavior while preserving clean
  stderr/stdout boundaries.
- [ ] CORE-PLUGIN-008 — Publish schemas, fixtures, examples, and
  language-neutral conformance tests while preserving the trusted-code,
  non-sandbox security model.

### Deferred workflows and compatibility

- [ ] CORE-HANDOFF-001 — Implement session handoff and lineage without changing
  the active session of unrelated clients.
- [ ] CORE-THREAD-001 — Expand bounded `#thread` references with escaping and
  active-session exclusion.
- [ ] CORE-SCRATCH-001 — Implement guarded scratchpad reads/edits, autosave,
  context injection, and fork/handoff copying.
- [ ] CORE-REVIEW-001 — Provide revision-pinned working-tree, commit, and branch
  review data with staged, unstaged, representable untracked files, and explicit
  skipped sections.
- [ ] CORE-REVIEW-002 — Define structured review-feedback attachments with
  immediate projection, removal/restoration, target identity, and stale-revision
  guards.
- [ ] CORE-CMD-001 — Add compact synthetic transcript identity for discovered
  prompt commands and Claude-compatible command discovery/namespacing.
- [ ] CORE-CMD-002 — Support dynamically registered commands with canonical
  ownership and generations.
- [ ] CORE-CONFIG-001 — Support Markdown template overrides with project/global
  precedence.
- [ ] CORE-SUB-001 — Add compatibility definition locations and
  plugin-contributed subagent definitions.
- [ ] CORE-CLI-001 — Add shell completion and noninteractive session list,
  rename, and delete commands.
- [ ] CORE-GH-001 — Fetch bounded, cached GitHub pull-request metadata through
  `gh` with silent degradation.
