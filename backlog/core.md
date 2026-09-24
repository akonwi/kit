# Core and protocol backlog

This ledger owns client-neutral behavior. A client backlog may depend on these
IDs but must not redefine server, persistence, or protocol semantics.

## R1 required

### Production data and configuration

- [-] CORE-MIG-001 — No migration command, automatic configuration conversion,
  compatibility scanner, or migration-marker startup gate is required. Compatible
  configuration is reused in place.
- [-] CORE-MIG-002 — Legacy runtime-data import and session continuation are
  intentionally excluded, not deferred. Native Kit starts with fresh sessions;
  legacy sessions, turns, attachments, scratchpads, subagent records, and runtime
  metadata are not imported.
- [ ] CORE-MIG-003 — Leave legacy runtime data untouched and keep native storage
  isolated from it when reusing `~/.kit`.
- [ ] CORE-MIG-004 — Publish a short migration guide and embedded, version-matched
  skill covering actual configuration differences and user-directed adjustments.
  Explain unchanged paths/formats, unsupported settings and plugin methods,
  discovery/validation differences, fresh sessions, and provider/MCP reauthentication.
  Do not require automated compatibility reporting or credential conversion.
- [~] CORE-AUTH-001 — Complete headless API-key and Anthropic credential
  management and consistent private, locked, atomic, generation-checked storage
  for every supported provider. Credential import is not required; verify that
  legacy provider and MCP auth files do not block reauthentication through the
  supported login UX or require users to manually delete files.
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
- [x] CORE-SESSION-002 — Automatically assign useful session names without
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
- [~] CORE-TOOL-002 — Implement approval/interceptor behavior without allowing a
  client to bypass server-owned tool policy. Generation-owned plugin interception
  now gates session and session-owned child tools on the server. Automated
  allow/reject, nested-dialog cancellation, and recovery coverage is in place;
  broader manual lifecycle verification remains.

### Headless and release safety

- [ ] CORE-HEAD-001 — Define headless-safe behavior for built-ins, MCP, and
  unavailable user interactions.
- [ ] CORE-HEAD-002 — Verify SIGINT/SIGTERM cleanup and documented exit codes.
- [ ] CORE-TEST-001 — Pass the full Go build, vet, test, and race-detector gates
  required by `AGENTS.md`. The default-home switch validation passes build, vet,
  and ordinary tests. A full race rerun also passes, but earlier race validation
  reproduced an intermittent failure in
  `TestPluginToolFixtureReachesModelAndDurableTranscript`: the shared fake provider
  observes the title-generation request and returns `tool-demo__echo` not found.
  The fixture uses an explicit temporary home; isolate naming requests from its
  tool-execution state and rerun the race gate.
- [ ] CORE-TEST-002 — Cover malformed and adversarial protocol records with fuzz
  or property tests.
- [~] CORE-TEST-003 — Use isolated production-shaped fixtures to verify in-place
  configuration reuse, fresh native sessions, and provider/MCP reauthentication.
  Prove isolated development/tests leave real `~/.kit` untouched and upgrade
  leaves legacy runtime data untouched and unimported.
- [ ] CORE-TEST-004 — Complete authenticated R1 smoke coverage for model and
  thinking selection, coding tools, attachments/images, MCP, interactions,
  signals, and the existing-installation upgrade workflow.

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

Process ownership and plugin UI routing follow
[ADR 0026](../docs/adrs/0026-scope-plugin-processes-and-route-plugin-ui.md).

- [x] CORE-PLUGIN-001 — Discover and validate manifest-v1 user and project
  plugins with deterministic precedence and automatic trusted-code loading.
  Initialize in the background without blocking turns; isolate startup failures
  and report persistent failures. Use the resolved v2 user home.
  The daemon composes a lazy session host using resolved v2 paths and canonical
  reserved command domains. Runtime publication starts background discovery;
  rejected loads never launch processes. Bounded warning snapshots report
  manifest, launch, initialization, runtime, and cleanup failures. Completed
  instance failures additionally emit one persistent notification to attached
  clients and log the bounded summary and stderr tail to the private server log.
- [x] CORE-PLUGIN-002 — Launch plugin processes without a shell from the
  installation working directory, reserve stdout for RPC, continuously drain
  bounded stderr, and enforce startup/shutdown deadlines.
  Low-level launch, bounded stderr, context-owned supervision, and process-group
  TERM/KILL cleanup are implemented and subprocess-tested. Generation-bound
  instances now perform background v1 initialization with a ten-second deadline,
  synchronous result validation/admission gating, and bounded graceful shutdown
  before group termination. Subprocess tests cover runtime behavior;
  Linux-specific manual verification is not a native-scope completion gate.
  Runtime-host composition and shutdown/disposal ownership are implemented.
- [x] CORE-PLUGIN-003 — Implement full-duplex versioned RPC, request
  correlation, cancellation, crash cleanup, and per-session process ownership.
  Standalone JSON-RPC transport supports bounded framing/queues, nested and
  out-of-order calls, batches, exact request IDs, bidirectional cancellation,
  ordered notifications, and connection-failure cleanup; covered by race and
  real-subprocess tests. Instance lifecycle now couples RPC/process failures,
  verifies version negotiation before following work dispatches, revokes call
  admission, and cancels both directions on stop. Instance identity fixes session,
  plugin, and generation; bounded diagnostic snapshots retain failure and stderr.
  Runtime hosts own cwd/name/reload/disposal transitions, retain user instances
  across cwd changes, reconcile initialization to current context, and fence
  handlers by host lifetime plus instance/transition generations. Method-specific
  adapters implement the supported native profile and reject excluded methods.
- [x] CORE-PLUGIN-004 — Support commands, tools, interception,
  subagent definitions, lifecycle events, and UI requests through typed
  contributions. Apply accepted contribution changes
  live at the earliest applicable UI/model-request/dispatch boundary, without
  waiting for turn completion or requiring reload. Give plugin interactions explicit
  ownership independent of model tool calls, route responses through the shared
  session broker. Plugin footer click callbacks, declarative URL actions, and
  `kit/system/open-url` are outside native scope, not deferred work.
  Present session-scoped notifications without replaying stale transient toasts.
  Restrict chrome items and hide claims to the bottom-right footer, allowing
  composition and replacement of default location content; reject protected
  targets and unsupported placement explicitly.
  The host now registers/unregisters bounded, namespaced command metadata and
  executes literal arguments against the selected instance generation, with
  context-ordered admission, cancellation, and strict null-result validation.
  The real v1 Python plugin-demo fixture covers execution, cwd updates, and
  validated toast callbacks. Separate session/protocol catalogs and cancellable
  HTTP/bound-client execution now carry opaque instance selections, reject stale
  or cross-session owners, and report bounded typed failures without starting a
  model run. The native palette now presents namespaced commands and argument
  hints, requires explicit reselection after owner replacement, and executes
  literal arguments without disturbing composer drafts or model turns. One
  attachment-owned invocation runs at a time with a two-minute deadline and
  scoped progress/error feedback; switching sessions cancels its wait. Successful
  execution clears progress without adding a completion toast.
  Plugin-emitted toasts now use bounded live-only session fan-out, an authenticated
  NDJSON stream, and attachment-owned native notification rendering. Detached
  clients receive no transient backlog; persistent notices also retain the host
  diagnostic projection. Real plugin-demo tests cover daemon and bound-client
  delivery, and UI tests cover presentation and stale-callback suppression.
  The user manually verified the available command/toast and session/cwd
  workflows on macOS. Dialog and remaining contribution workflows were verified
  as their native slices landed.
  Host-side confirm/input/select adapters now validate bounded v1 payloads,
  preserve opaque JSON choice values, distinguish unavailable UI from user
  cancellation, and fence callbacks/results by instance lifetime. The real
  ui-api-demo flow passes with a scripted host observer, including revocation
  cancellation. The daemon now routes dialogs through the shared session broker
  with explicit plugin-generation ownership and no fabricated model run or tool
  identities. Choice values remain host-private; clients receive option metadata
  and opaque response IDs. Native docks support custom confirmation labels/default
  focus, initial/empty input, and plugin provenance. Select dialogs use plain
  option lists in both clients, even when a plugin requests filtering. Runless metadata
  delivery is separate from model-run interaction delivery to prevent stale replay.
  Real ui-api-demo daemon tests cover the complete dialog sequence, concurrent
  first-answer-wins responses, empty input, and reload revocation; broker tests
  cover shutdown/admission cancellation under runtime authority. Native keyboard
  and presentation tests cover labels, focus traversal, initial values, and
  plain-list selection.
  The user confirmed the ui-api-demo dialog flow works in the TUI on macOS.
  Host-side footer set/update/clear and hide/show adapters now validate bounded
  styled segments and public theme tokens, preserve first-registration order,
  and fence items/claims by generation. Only the bottom-right region and
  `kit.footer.location` built-in target are configurable; left/header operations,
  click callbacks, and URL actions remain unsupported. Overlapping hide claims
  compose, and revocation restores defaults when no active claim remains.
  Real subprocess and race tests cover publication, crashes, reload/cwd cleanup,
  invalid input, snapshot isolation, and capacity limits. Session/protocol footer
  snapshots now reach both clients through metadata invalidation. TUI and macOS
  render styled static items in the bounded bottom-right region with labeled
  overflow; the footer-demo fixture covers set/replace/clear and reload. The user
  manually verified footer rendering, overflow, and lifecycle behavior.
  Tool registration/unregistration now validates the v1 schema profile and
  publishes generation- and registration-owned model tools at the next request
  boundary. Model inputs and text/image/details/terminate results are validated;
  declared execution modes and tool-owned prompt guidance use the existing agent
  loop. Captured callbacks and durable registration identities prevent stale
  model responses or recovered calls from reaching replacement registrations.
  Admitted batch scheduling is durable; a real SQLite close/reopen/resume test
  covers stale plugin ownership and sequential hook/callback ordering.
  A real tool-demo subprocess test covers daemon/model execution and durable
  transcript results. A live-session echo smoke check returned `plugin tools work`;
  the user also verified the broader lifecycle behavior.
  Interceptors now register idempotently, run sequentially in registration order,
  and fail closed on errors or replacement without automatically aborting the
  whole turn. Policy identities are persisted per admitted tool and checked again
  immediately before invoking tool code, including parallel batches. Registry
  changes cancel pending interception; recovered work never reuses an old host's
  approval. Core/plugin tools and session-owned child tools use the same policy.
  Real subprocess-to-daemon tests exercise nested confirmation, explicit rejection,
  approval, and cancellation. SQLite reopen and parallel-ready tests cover stale
  policy admission; test fixtures remain outside automatic plugin discovery.
  Live-session approval, rejection, and lifecycle behavior were manually verified.
  Plugin subagent registration now publishes bounded, canonical generation-owned
  definitions into the existing effective catalog, model prompt, dynamic parent
  tool, and native snapshots. Filesystem conflicts fail synchronously or remove
  a contribution when the base catalog changes. Revocation prevents new starts
  without aborting durable children admitted under the prior definition. Host,
  session, subprocess, prompt, metadata, and race tests cover registration,
  removal, conflicts, reload/cwd cleanup, and live projection.
  Turn-started/completed notifications now follow canonical durable admission and
  terminal settlement, including failure/abort. Only ready generations that
  received start may receive completion; late/replacement instances and recovered
  turns receive no replay. Payloads contain ordered user/assistant text only.
  Session-owned autonomous reactions retain session identity; child subagent turns
  do not fabricate parent events. Completion frames are bounded to 256 KiB and
  projection/settlement waits are bounded; omission produces diagnostics.
  Real subprocess, queued-turn, cancellation, runtime-disposal, and SQLite-reopen
  tests cover ordering and lifecycle ownership. Live-session start/completion,
  failure/reload lifecycle, and text-only projection were manually verified,
  including exclusion of tool arguments/output. Live Git notifications now use one session-owned
  shared Git/PR observer independent of attached clients, with per-generation
  projection
  deduplication and cwd/reload/late-readiness fencing. Real Git/subprocess tests
  cover branch, dirty, and detached HEAD transitions; lifecycle tests cover
  initialization, cwd/null, replacement, and shutdown ownership. The user manually
  verified the broader event lifecycle behavior.
  Print follows ordinary session interaction
  policy under [ADR 0027](../docs/adrs/0027-treat-print-as-an-ordinary-session-client.md).
- [x] CORE-PLUGIN-005 — Remove contributions atomically after plugin failure,
  cancel outstanding calls in both directions and owned interactions, and fail
  affected operations with typed errors. Block calls awaiting failed interceptors
  without automatically aborting the whole turn. Emit a persistent failure
  notification, log bounded failure evidence, and communicate potentially partial
  side effects; never automatically replay interrupted operations.
  Command catalogs now filter revoked owners atomically and publish independently
  of slow plugin initialization; reload/crash cancels calls without retargeting.
  Shared dialogs now fence owner revocation and cancel on reload or session
  deletion. Tool revocation removes future-request schemas and guidance, and
  captured calls fail closed rather than executing a replacement. Commands carry
  monotonic registration identities so same-generation re-registration cannot
  retarget stale client selections. Subagent revocation removes future definitions
  while preserving already-admitted durable children. Interceptor failures reject
  the affected tool and permit normal model continuation; cancellation propagates
  through the RPC request and conforming plugins cancel their nested dialog
  requests.
- [x] CORE-PLUGIN-006 — Implement explicit session reload using
  cancel-and-replace semantics, bounded graceful shutdown, and process-group
  termination. Fence ownership and late responses by instance generation; never
  reassign an old instance's dispatched RPC to its replacement.
  Runtime reload now revokes all generations and rediscovers asynchronously;
  cwd changes revoke project generations without replacing user instances.
  Commands, dialogs, tools, interceptors, subagents, footer state, notifications,
  and events all have generation-fenced disposal. Configuration quarantine routes
  orphaned runtimes through full ordinary cleanup instead of closing only the
  droid and store. Per-plugin restart controls are outside native scope.
CORE-PLUGIN-007 is retired, not deferred: a separate noninteractive plugin
policy is excluded by [ADR 0027](../docs/adrs/0027-treat-print-as-an-ordinary-session-client.md).

- [~] CORE-PLUGIN-008 — Publish schemas, fixtures, examples, and
  language-neutral conformance tests while preserving the trusted-code,
  non-sandbox security model. Document the bottom-right-only chrome surface and
  explicit rejection of unsupported header/left-footer operations. Include host
  resource limits for outgoing calls, notification queues, batch element counts,
  retained batch data, per-session plugin installations and command registrations,
  command argument/metadata sizes, and toast payload restrictions in the published
  transport profile. Document live-toast subscription limits (32 per session),
  bounded queues (16 per subscriber), nonblocking overflow drops, 32 KiB NDJSON
  frame limits, and the explicit absence of transient replay/offline storage.
  Include the host's eight pending plugin interactions, 64 KiB encoded request
  cap, bounded dialog text/option metadata and raw JSON values, and structured
  interactivity-unavailable errors in the profile before claiming UI conformance.
  The [native implementation profile](../app/docs/plugin-protocol/native-v2.md)
  now publishes implemented methods, resource bounds, ownership and cancellation,
  live-only toast delivery, unsupported chrome, and explicit conformance gaps.
  The UI fixture documents its native workflow; real daemon tests also cover
  answering/cancelling from another client connection and session deletion.
  Dedicated plugin diagnostics and dismissal surfaces are outside native scope;
  persistent failure notifications plus the private server log provide evidence.
  A standalone language-neutral conformance-vector artifact remains outstanding;
  current executable conformance coverage lives in the Go host/daemon suites and
  real subprocess fixtures.

- [ ] CORE-PLUGIN-009 — Allow a plugin to inform its owning session without
  submitting a user message or autonomously starting or queuing a model turn.
  Define a bounded, provenance-preserving information channel whose content can
  become available at a deliberate session consumption boundary. It must not
  impersonate user speech, alter an already-dispatched model request, or mutate
  another session. Specify retention, replacement/consumption, client visibility,
  generation cleanup, and behavior across detach, reload, and runtime disposal
  before adding a public protocol method.

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
- [~] CORE-SUB-001 — Add compatibility definition locations and
  plugin-contributed subagent definitions. Native plugin register/unregister is
  live and generation-owned; compatibility definition locations remain.
- [ ] CORE-CLI-001 — Add shell completion and noninteractive session list,
  rename, and delete commands.
- [x] CORE-GH-001 — Fetch bounded, cached GitHub pull-request metadata through
  `gh` with silent degradation. The daemon-owned native adapter enriches the
  VCS projection asynchronously without delaying local Git status. Named-branch
  lookups run from the captured repository root with explicit branch identity,
  a 2.5-second deadline, bounded output, and validated PR number/HTTP(S) URL.
  Positive and negative results are cached for 60 seconds by cwd/root/branch;
  storage and concurrency are bounded, and daemon shutdown cancels/joins work.
  PR completion invalidates the session observer immediately; authenticated native
  VCS streams and plugin initialization/git.changed share the combined state.
  Local Git observation runs every ten seconds; clients no longer poll. Streams
  provide initial snapshots, deduplicated bounded latest-only updates, heartbeats,
  and fresh-state reconnects. Cwd changes clear metadata and fence stale probes.
  Unit/race and real Git/fake-gh daemon tests cover caching, failure, cancellation,
  branch isolation, detached heads, and nonblocking first responses.
