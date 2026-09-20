# Core and protocol backlog

This ledger owns client-neutral behavior. A client backlog may depend on these
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
- [ ] CORE-MIG-004 — Preserve supported user settings, agent definitions, prompt
  templates, MCP configuration, and plugin manifests with documented precedence;
  deferred surfaces remain untouched and discoverable Post-R1. Theme migration
  is a documented user-managed workflow.
- [ ] CORE-MIG-005 — Switch the production default from `~/.kit-v2` to `~/.kit`
  only through the explicit migration and release process.
- [~] CORE-AUTH-001 — Complete headless API-key and Anthropic credential
  management, credential migration, and consistent private, locked, atomic,
  generation-checked storage for every supported provider.
- [ ] CORE-SET-001 — Validate and persist shared settings, apply changes
  immediately where safe, and return actionable save errors.
- [ ] CORE-SET-002 — Persist and resolve R1 defaults for model/thinking
  selection, retry behavior, guided questions, and diff layout without
  client-local drift. Renderer-specific workspace layout is client state, not a
  shared setting.

### Daemon, sessions, and runtime

- [~] CORE-LIFE-002 — Enforce explicit resource and backpressure limits across
  HTTP, event replay and subscriptions, direct tools, MCP, and clients.
- [ ] CORE-LIFE-003 — Produce crash-safe logs and actionable diagnostics without
  leaking credentials or protocol output.
- [ ] CORE-LIFE-004 — Meet documented cold-start and warm-attach acceptance
  thresholds on supported release platforms.
- [ ] CORE-LIFE-005 — Enforce a hard shutdown deadline for provider streams,
  direct tool processes, and subagent runtimes that ignore cooperative
  cancellation, without allowing late cleanup to access closed shared storage.
- [ ] CORE-LIFE-006 — Attach configured MCP managers to their owning runtimes and
  close in-flight calls, sessions, and transports within the shutdown deadline.
- [ ] CORE-SESSION-001 — Let multiple clients observe and control one
  authoritative session without lost updates or client-global active-session
  state. Broadcast follow-up queue changes to attached clients and expose failed
  automatic queue admission instead of leaving a silently blocked queue.
- [~] CORE-FORK-001 — Transactionally publish a settled session fork as a linked
  child through droids semantic forking, with client-visible lineage, attachment
  preservation, and an optional first child prompt, without changing the
  session viewed by unrelated clients.
- [x] CORE-PEER-001 — Let persistent session droids discover eligible peers and
  exchange bounded, durable, correlated queries through independently scheduled
  recipient turns, with restart-safe delivery, terminal replies, cycle limits,
  and no peer lifecycle authority, as defined by ADR 0014.
- [~] CORE-RUN-001 — Complete streaming and recovery semantics for text,
  thinking, messages, tool activity, usage, provider errors, terminal state, and
  reconnecting clients.
- [~] CORE-RUN-002 — Reconstruct active and historical turns consistently after
  reconnect and restart, including rich ordered content and tool identity.
- [~] CORE-RUN-003 — Propagate abort and cooperative cancellation through
  providers, tools, MCP, and subagents with deterministic terminal state.
- [x] CORE-RUN-004 — Expose bounded provider retry countdowns and recovery state
  to clients.
- [~] CORE-RUN-005 — Persist proactive and overflow-driven compaction
  checkpoints and expose pending, completed, and failed lifecycle state.
- [ ] CORE-SESSION-002 — Automatically assign useful session names without
  overwriting explicit user names.
- [ ] CORE-SESSION-003 — Define transcript replacement and corruption-recovery
  semantics.
- [ ] CORE-USAGE-001 — Verify that cumulative historical cost remains unchanged
  across model changes and is projected consistently after restart.

- [ ] CORE-CATALOG-001 — Expose a server-wide session catalog snapshot and
  change stream so clients can maintain live session lists without polling or
  subscribing to every session. Publish additions, removals, and summary changes
  including names, cwd, activity ordering, and run status. Define snapshot/cursor
  handoff, ordered delivery, bounded replay, and resynchronization after gaps or
  daemon restart; preserve catalog visibility rules for temporary sessions.

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

- [x] CORE-WORK-001 — Expose bounded, client-safe directory listings and file
  reads rooted in and unable to escape the session workspace, with canonical
  paths, revisions, truncation, and explicit missing, binary, permission, and
  stale-data errors, as defined by ADR 0019.
- [x] CORE-DIFF-001 — Expose bounded, revision-aware file and hunk diff data for
  working-tree changes, including agent edits, without requiring clients to
  execute Git or parse presentation-oriented tool output.
- [~] CORE-TOOL-001 — Route URL opening through validated client/platform ports
  and define safe behavior when no capable client is attached.
- [x] CORE-ATT-001 — Validated local image and attachment inputs, provider
  capability and bounds enforcement, durable references, submission,
  restoration, transcript projection, and cleanup on session deletion.
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
- [ ] CORE-TEST-004 — Complete authenticated R1 smoke coverage for model and
  thinking selection, coding tools, attachments/images, MCP, interactions,
  signals, and migration.

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

- [ ] CORE-BASH-001 — Let clients discover direct shell executions started and
  completed by another client, including executions excluded from model context.
  Expose replayable lifecycle events or a bounded execution listing with a cursor
  so short executions cannot be missed between polls. Define retention and
  reconnect/gap recovery without adding excluded output to model context.
- [-] CORE-HANDOFF-001 — Superseded by the renamed R1 `/fork` workflow tracked
  by `CORE-FORK-001`.
- [ ] CORE-DROIDS-001 — Decide whether to keep droids internal, maintain an
  independent fork, or extract selected changes after the rewrite stabilizes.
- [ ] CORE-LIFE-007 — Add bounded idle session and daemon eviction policies.
- [ ] CORE-INT-003 — Recover pending user interactions across server restarts.
- [ ] CORE-THREAD-001 — Expand bounded `#thread` references with escaping and
  active-session exclusion.
- [x] CORE-SCRATCH-001 — Provide database-backed family-owned scratchpads with
  guarded human writes, transactional agent edits, snapshots, bounded events,
  and explicit path-free model tools without implicit prompt inclusion. See
  [ADR 0025](../docs/adrs/0025-share-database-backed-scratchpads-across-session-families.md).
- [x] CORE-ANN-001 — Provide bounded, session-owned draft annotations with
  server-allocated monotonic IDs, typed revision-pinned anchors, authoritative
  previews, mutation, deletion, stale guards, persistence, snapshots, and events,
  beginning with workspace-file line ranges. See
  [ADR 0022](../docs/adrs/0022-model-draft-annotations-as-session-inputs.md).
- [x] CORE-ANN-002 — Accept ordered annotation IDs with structured prompts,
  atomically validate and snapshot them into accepted messages, remove submitted
  drafts, and project immutable submitted annotations through transcript
  contracts. See
  [ADR 0022](../docs/adrs/0022-model-draft-annotations-as-session-inputs.md).
- [ ] CORE-REVIEW-001 — Provide revision-pinned working-tree, commit, and branch
  review data with staged, unstaged, representable untracked files, explicit
  skipped sections, and target-scoped annotation workflows. See
  [ADR 0024](../docs/adrs/0024-generalize-diff-review-targets.md).
- [ ] CORE-REVIEW-002 — Let agents add revision-validated diff annotations that
  use the session-owned review annotation lifecycle and can be dismissed or
  hidden by clients. Depends on `CORE-REVIEW-001`.
- [ ] CORE-REVIEW-003 — Evaluate optional Ataraxy review triage and semantic
  navigation without replacing Kit's exact patch data or making an external
  binary a required dependency. See the
  [focused integration note](ataraxy-review-integration.md).
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
