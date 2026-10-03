# Core and protocol backlog

This ledger owns client-neutral behavior. A client backlog may depend on these
IDs but must not redefine server, persistence, or protocol semantics.

## Release scope

### Production data and configuration

- [ ] CORE-AUTH-001 — Provide headless API-key and Anthropic credential login,
  replacement, and logout. Verify that legacy provider and MCP auth files do not
  block reauthentication through the supported login UX or require users to
  manually delete files.
- [ ] CORE-SET-001 — Validate and persist shared settings, apply changes
  immediately where safe, and return actionable save errors.
- [ ] CORE-SET-002 — Persist and resolve production defaults for model/thinking
  selection, retry behavior, guided questions, and diff layout without
  client-local drift. Renderer-specific workspace layout is client state, not a
  shared setting.

### Daemon, sessions, and runtime

- [ ] CORE-LIFE-002 — Bound MCP transport reads before result conversion so an
  oversized server response fails closed instead of buffering without a limit.
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
- [ ] CORE-FORK-001 — Accept an optional first prompt when forking a settled
  session, and admit that prompt only on the new child.
- [ ] CORE-RUN-005 — Include a bounded, classified failure reason in
  failed-compaction events, persisted compaction state, and the explicit
  compaction error. Use reviewed reasons such as `summary_truncated`,
  `provider_error`, `not_adaptable`, and `canceled` instead of provider text or
  the generic "Context compaction failed" message. Clients must be able to
  explain the failure after reconnect without credentials or prompt content.
- [ ] CORE-RUN-006 — Fix multimodal context estimation for image/file content.
  The provider-neutral estimator counts inline `data:` URLs as text
  (approximately two encoded bytes per token), so a ~1.8 MB image returned by
  `show_image` was estimated at ~900K tokens and repeatedly caused automatic
  compaction to fail before the next model response. Images explicitly supplied
  for model inspection still consume provider input tokens, but their encoded
  transport bytes are not text tokens. Budget image content using provider/model/
  detail-aware accounting or a bounded conservative modality estimate, whether
  supplied as a URL, file ID, or inline data. Cover images in an unconsumed
  tool-call tail, compaction trigger/replacement decisions, and manual-versus-
  automatic compaction with regression tests. Preserve safe limits without
  treating Base64 length as token count.
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

- [ ] CORE-PROTO-004 — Paginate transcripts and mutable collections and recover
  records too large for an individual event or response.
- [ ] CORE-PROTO-005 — Correlate mutating commands with the client's preceding
  event cursor and keep admission ordering deterministic when that cursor is
  stale.
- [ ] CORE-PROTO-006 — Bound client queues and disconnect clients that cannot
  keep up without blocking authoritative session work.
- [ ] CORE-PROTO-007 — Implement the public typed Go client accepted in ADR
  0038, migrate the TUI, print mode, and RPC bridge to it, and retire the
  existing internal client stacks.

### Workspace data, tools, attachments, and interactions

- [ ] CORE-TOOL-001 — Route URL opening, including MCP authorization URLs,
  through a validated attached client or platform port instead of launching a
  browser on the server host. Define safe behavior when no capable client is
  attached.
- [ ] CORE-TOOL-002 — Manually verify the approval and interceptor lifecycle
  beyond automated allow, reject, nested-dialog cancellation, and recovery,
  including that a client cannot bypass server-owned tool policy.

### Headless and release safety

- [ ] CORE-HEAD-001 — Define headless-safe behavior for built-ins, MCP, and
  unavailable user interactions.
- [ ] CORE-HEAD-002 — Verify SIGINT/SIGTERM cleanup and documented exit codes.
- [ ] CORE-TEST-001 — Confirm the plugin-tool fixture that isolates tool-free
  naming requests passes the Linux and macOS CI build, vet, test, and race gates.
- [ ] CORE-TEST-002 — Cover malformed and adversarial protocol records with fuzz
  or property tests.

### GPT-6 provider and runtime capabilities

These are optional capabilities to revisit as GPT-6 models mature, not release
gates. The [GPT-6 guide](https://developers.openai.com/api/docs/guides/latest-model)
and linked specifications are the starting point; recheck current compatibility
before implementation. Public OpenAI API support does not establish Codex OAuth
endpoint support. Completed temperature compatibility, provider policy-stop
handling, effort history, and prompt-cache controls are documented in the feature guides linked below.

- [ ] CORE-GPT6-004 — Let users steer an active provider response over Responses
  WebSockets, retaining boundary-based steering for unsupported providers.
  Represent accepted, pending, applied, and failed submissions distinctly;
  acceptance alone must not imply application. Keep provider continuations within
  the logical droids turn, with bounded connection-owned queues, tool-result and
  approval handling, durable user input, and disconnect reconciliation that
  neither loses nor duplicates instructions. Preserve session ownership and
  cancellation semantics: steering does not undo actions or cancel running tools.
  Test automatic continuation, required-input waits, late completion, failures,
  reconnect, and repeated updates. See
  [effort history](../docs/features/reasoning-effort-history.md) for the
  supported public endpoint/model gate and
  [mid-turn steering](https://developers.openai.com/api/docs/guides/steering).
- [ ] CORE-GPT6-005 — Support explicit opt-in asynchronous tools that allow model
  progress while calls remain pending across responses. Dispatch only complete
  call items and return results under their original call IDs. Persist pending
  call identity, lifecycle, and delivery state; define bounded concurrency, wait
  semantics, late results, turn settlement, cancellation, restart, model changes,
  forks, and compaction. Preserve tool policy and approval/interceptor enforcement
  before execution; initially enable selected read-only tools rather than all
  tools. Nonblocking user questions must retain their original call until answered
  or explicitly dismissed/timed out, not complete with a display acknowledgment.
  Verify per-model/endpoint support and incompatibilities with programmatic tools
  and provider multi-agent parallel calls. Test result ordering, dependent waits,
  duplicate delivery, failure/recovery, and concurrent-session isolation.
  Preserve the existing [provider policy-stop behavior](../docs/features/provider-policy-stops.md);
  see [async tools](https://developers.openai.com/api/docs/guides/async-tool-calling).
- [ ] CORE-CACHE-001 — Keep valuable prompt caches warm. Before an entry
  expires, replay the last request with a minimal output cap to refresh it,
  while a run is active (for example across long tool calls or subagents) and
  optionally between runs. Refresh only when the expected saved cache-miss cost
  exceeds the refresh cost, bound warming by age, skip requests that cannot be
  replayed without changing the cached prefix, and record refresh usage without
  adding it to context. See [Pi's cache warmer](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/src/core/cache-warmer.ts)
  and [prompt caching](../docs/features/prompt-caching.md).
- [ ] CORE-GPT6-007 — Expose supported standard/pro reasoning execution modes
  independently of reasoning effort and service tier. Validate provider/model
  capability and incompatible combinations, persist the effective configuration,
  and accurately report resulting usage. Do not equate `max` effort with pro
  mode or assume catalog experimental metadata is wired into requests. Cover
  serialization, reconfiguration, and unsupported endpoint behavior. See
  [reasoning modes](https://developers.openai.com/api/docs/guides/reasoning).
- [ ] CORE-GPT6-008 — Evaluate Kit's GPT-6 prompting on representative coding
  workflows for unnecessary clarification, delegation frequency, instruction
  conflicts in skills/context, verbosity, and disproportionate testing. Apply
  evidence-based prompt changes without weakening user authorization, tool policy,
  or required repository checks. Keep behavior guidance in Kit's prompt assembly
  rather than baking application policy into droids. Kit's existing subagents do
  not require adopting OpenAI-hosted multi-agent orchestration.

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

- [ ] CORE-PLUGIN-008 — Publish a standalone language-neutral conformance-vector
  artifact for the native plugin transport profile. Go host, daemon, and
  subprocess fixtures remain the executable coverage until that artifact exists.

- [ ] CORE-PLUGIN-009 — Allow a plugin to inform its owning session without
  submitting a user message or autonomously starting or queuing a model turn.
  Define a bounded, provenance-preserving information channel whose content can
  become available at a deliberate session consumption boundary. It must not
  impersonate user speech, alter an already-dispatched model request, or mutate
  another session. Specify retention, replacement/consumption, client visibility,
  generation cleanup, and behavior across detach, reload, and runtime disposal
  before adding a public protocol method.

- [ ] CORE-PLUGIN-010 — Let plugins submit messages to their owning session to
  start or queue model turns, enabling workflows such as autoresearch kickoff and
  automatic continuation after a settled turn. This is distinct from the passive
  information channel in CORE-PLUGIN-009. Define the supported replacement for
  the currently unsupported `kit/session/submit-message` call used by dot-kit's
  autoresearch plugin. Preserve plugin provenance rather than impersonating user
  speech; enforce session/generation ownership, normal tool policy, bounded queues
  and autonomous-loop limits. Specify busy-session admission, cancellation,
  duplicate/retry handling, and detach/reload/restart behavior. Cover kickoff,
  post-settlement continuation, stale-generation rejection, and concurrent-session
  isolation with real subprocess tests and document the public RPC contract.

### Deferred workflows and compatibility

- [ ] CORE-BASH-001 — Let clients discover direct shell executions started and
  completed by another client, including executions excluded from model context.
  Expose replayable lifecycle events or a bounded execution listing with a cursor
  so short executions cannot be missed between polls. Define retention and
  reconnect/gap recovery without adding excluded output to model context.
- [ ] CORE-PEER-002 — Give each persistent session a bounded, durable peer-message
  inbox with model-accessible operations to list, inspect, and explicitly reply
  to messages after the receiving turn has ended. Route replies back to the
  sender asynchronously under the original correlation identity, without
  requiring the original send call to remain open or forcing either session to
  start a turn. Preserve unread, replied, expired, and terminal state across
  detach and restart; expose new-message availability to clients; and enforce
  session eligibility, cycle, retention, and capacity limits. Verify delayed and
  out-of-order replies, duplicate suppression, restart recovery, and concurrent
  conversations between the same peers.
- [ ] CORE-DROIDS-001 — Decide whether to keep droids internal, maintain an
  independent fork, or extract selected changes after the rewrite stabilizes.
- [ ] CORE-LIFE-007 — Add bounded idle session and daemon eviction policies.
- [ ] CORE-INT-003 — Recover pending user interactions across server restarts.
- [ ] CORE-THREAD-001 — Expand bounded `#thread` references with escaping and
  active-session exclusion.
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
- [ ] CORE-SUB-001 — Discover subagent definitions from the Pi compatibility
  locations `~/.pi/agent/agents/*.md` and `<cwd>/.pi/agents/*.md`, after
  Kit-native locations, with first-definition-wins precedence.
- [ ] CORE-CLI-001 — Add shell completion and noninteractive session list,
  rename, and delete commands.
- [ ] CORE-GH-002 — Use the daemon-owned GitHub observer that supplies built-in
  footer status to keep the model informed when the current pull request's CI
  checks change, without requiring the user or model to explicitly fetch them.
  Deliver bounded, deduplicated, provenance-labeled updates at a safe model
  consumption boundary without impersonating user speech or interrupting an
  in-flight request; fence stale results across branch, repository, PR, and
  session changes, and degrade silently when GitHub status is unavailable.
  Verify pending, successful, and failed check transitions plus reconnect and
  stale-update behavior.
