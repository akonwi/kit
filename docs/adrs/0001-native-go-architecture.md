# 0001: Native Go application architecture

## Status

Accepted

## Context

Kit is being rebuilt from the ground up as a fast, native coding-agent CLI. It
must retain the current terminal and semantic web workflows, support local and
remote sessions, continue work after clients disconnect, and allow custom
plugins written in any technology without allowing a plugin failure to crash
Kit.

The previous implementation combined a Bun/OpenTUI application, a TypeScript
agent runtime, browser clients, persistence, and process-hosted plugins. Its
accepted server/client direction remains useful, but the rewrite is not bound to
its implementation or ADR history. Historical decisions remain available at
Git commit `5c6e112` and on the branches that contain the previous
implementation.

The rewrite has these hard constraints:

- users install one Go executable and do not need Bun or Node;
- the agent core is the Kit-private `internal/droids` package, initially seeded
  from `github.com/akonwi/droids`;
- subagents are concurrent supervised executions, not blocking nested tool
  calls;
- Kit retains a native TUI and semantic browser client, but drops `web-tui`;
- custom plugins are isolated child processes using a language-neutral RPC
  protocol;
- the system is always single-user, including remote deployments;
- the rewrite is mergeable only after feature and UX parity, except for
  explicitly accepted changes.

## Decision

### Product and distribution

Kit ships as one Go executable for macOS and Linux. Windows is not a supported
target. The executable contains the CLI, local daemon, remote server, session
runtime, plugin supervisor, native TUI, and embedded web assets.

The browser client may use TypeScript, Solid, Mica, and Bun at build time. Its
compiled static assets are embedded into the Go executable. No JavaScript
runtime is required on the user's machine.

Custom plugin executables and tool processes are intentionally separate from
Kit and are not part of the one-executable distribution guarantee.

### System shape

```text
                       one Kit executable

  native TUI ───────┐
  print/RPC client ─┼── session protocol ── Kit server
  semantic web UI ──┤                         ├── session supervisors
  remote TUI ───────┘                         │    ├── parent droids runtime
                                              │    ├── subagent supervisor
                                              │    └── plugin supervisors
                                              ├── SQLite store
                                              └── user-editable files
```

The server is authoritative. Clients render state and submit intent; they do
not own agent runtimes, read server-side session files directly, or infer
shared state from local UI state.

Go packages keep renderer, protocol, orchestration, and persistence concerns
separate. The intended dependency direction is:

```text
CLI composition root
  -> daemon/server
      -> session orchestration
          -> Kit-local droids core
          -> subagents
          -> plugin host
          -> persistence ports
  -> client contracts
      -> native TUI / print / RPC

semantic web client
  -> versioned wire protocol only
```

Types belong to the layer that defines their meaning. Droids messages, stored
records, wire records, client snapshots, and renderer models are projected
explicitly rather than shared as one universal domain type.

### Executable roles and local daemon

The executable dispatches into several process roles:

```text
kit                  local client bootstrap + native TUI
kit -p               print client
kit --rpc             stdio protocol bridge/client
kit web               embedded browser client gateway
kit attach <server>   native TUI attached to a remote server
kit serve             explicitly exposed remote server
kit daemon ...        local daemon lifecycle commands
kit __daemon          internal detached daemon role
```

A normal local invocation discovers or starts one persistent daemon for the
current user. Starting the daemon means executing the same binary in the
internal `__daemon` role; it does not require a separately installed `kitd`.
Before detaching, the launcher atomically stages a private copy under the run
directory so development launchers such as `go run` cannot unlink the daemon's
executable while it is still serving. The daemon owns SQLite, running sessions,
droids instances, subagents, and plugin processes.

The local daemon:

- binds only to loopback on an ephemeral port;
- requires a cryptographically random credential even on loopback;
- publishes atomic process metadata beneath the Kit run directory;
- is started under an inter-process startup lock;
- remains alive after clients detach;
- distinguishes client detach, turn abort, ephemeral-session disposal, and
  server shutdown;
- exposes explicit status, start, stop, and restart operations.

Closing a TUI or browser connection never implicitly aborts active agent or
subagent work.

### Server and session clients

Server-scoped operations and session-scoped operations are separate. A server
client discovers, creates, and attaches to sessions. A session client binds to
exactly one session for its lifetime.

```text
server scope
  health, capabilities, list sessions, create session, attach session

session scope
  synchronize, prompt, steer, follow up, abort, transcript, model,
  interactions, attachments, commands, feature operations, events
```

A session connection cannot switch its authoritative binding. Switching a UI
to another session means opening another session client and replacing or adding
a renderer view.

A client receives a run handle only after its generation is durably reserved.
Explicit abort names that run generation, so delayed cancellation can neither
be lost before admission nor cancel a successor run. Stopping a wait on the
handle remains a detach operation and does not itself abort execution.

Local and remote clients use the same canonical, wire-safe protocol semantics.
The native local TUI connects through authenticated loopback rather than using
a privileged direct-runtime path. A transport atomically establishes an event
subscription and returns a snapshot plus high-water cursor. Ordered events use
monotonic session stream sequences, with bounded replay and snapshot fallback.
Command responses are correlated and connection-scoped.

Multiple clients may attach to the same session. Multiple top-level sessions
may execute concurrently. Each session serializes its own parent-run commands,
while other sessions, tool batches, and bounded subagents can run in parallel.
There is no process-global active session or cwd.

### Agent runtime

Kit composes its agent loop through the private `internal/droids` package,
seeded from the standalone droids repository and allowed to evolve with the
rewrite. Droids provider, message, stream, tool, and event types remain behind
Kit's runtime boundary. The server projects droids events into Kit-owned session
events and persists Kit-owned records. See
[ADR 0002](./0002-internalize-agent-core.md) for provenance and synchronization
policy.

Each top-level session has at most one active parent run. Existing steering,
follow-up, queue, abort, retry, tool, and compaction behavior is rebuilt around
that invariant. Context cancellation is threaded through provider calls, tools,
subagents, plugins, and shutdown.

Global and per-session limits bound provider calls, tools, and subagents. These
limits protect a persistent daemon from one session exhausting all resources.

### Concurrent subagents

A subagent is a supervised child execution with its own droids instance,
context, transcript, status, and event stream. It runs in a goroutine and does
not hold a parent tool call open for its lifetime.

A model-facing operation may start a subagent, but it returns a durable task ID
promptly. The session runtime owns subsequent lifecycle operations: inspect,
message, wait, cancel, and result delivery.

Subagent state and completion are durable. Completion is placed in the parent
session's mailbox and surfaced immediately to attached clients. It is injected
into an active parent only at a safe boundary between model turns. If the parent
is idle, completion waits for the next user-initiated run and does not trigger a
new model call automatically.

Client disconnects do not affect subagents. After a daemon crash, in-flight
work is recorded as interrupted; Kit does not claim that an arbitrary provider
stream or side-effecting tool resumed exactly. Explicit retry or reconstruction
may be offered where safe.

### Persistence and migration

SQLite is authoritative for runtime-owned durable data, including:

- sessions and turns;
- messages and content projections;
- parent runs and subagent executions;
- durable parent mailboxes;
- stream identities, ordered event records, and high-water cursors;
- schema migration history.

SQLite uses transactional schema migrations and foreign-key enforcement. A
bounded append-only event journal supports reconnect and diagnostics, but Kit
is not implemented as a system where every state table must be rebuilt from an
event log.

On daemon startup, executions left `running` or durably queued by a previous
process are marked `interrupted` transactionally. Messages from failed, aborted, interrupted, or
otherwise incomplete parent turns remain available for diagnostics and UI
history, but only completed turns are rehydrated into a new droids model
transcript. A non-completed live runtime is discarded so its in-memory context
cannot diverge from that replay rule.

Provider credentials remain in a separate private, machine-managed auth file
with locked atomic writes and generation-checked OAuth rotation; see
[ADR 0003](./0003-provider-credential-storage.md).

Human-editable configuration remains file-based:

- settings;
- themes;
- prompts;
- skills;
- plugin manifests;
- MCP configuration and other deliberate user-owned surfaces.

During rewrite development, all paths default to `~/.kit-v2`. Tests use an
isolated temporary home, and `KIT_HOME` can override the root. The rewrite must
not silently read or mutate `~/.kit` while under development.

Before replacement of the current implementation, Kit will provide an
idempotent migration from `~/.kit` with a backup and explicit version marker.
The migration never rewrites the only copy of old data in place. After migration
and parity validation, the production default can return to `~/.kit`.

### Native TUI

The terminal client is rebuilt in Go with `go.rockorager.dev/vaxis/ui`. It
preserves Kit's visual language and workflows rather than mechanically
translating TypeScript components.

The TUI consumes only session-client and platform contracts. Focus, overlays,
composer drafts, workspace tabs, scroll positions, terminal dimensions, theme,
and keybindings remain renderer-owned state. Runtime updates from goroutines
enter the vaxis event loop through its dispatch mechanism.

The TUI must not import SQLite implementations, droids runtime types, concrete
server internals, or plugin process implementations.

### Semantic web client

The semantic browser client retains Solid and Mica. Transport, protocol
reduction, and service layers remain independent of Solid where practical. The
Go build embeds the compiled HTML, JavaScript, and CSS and serves them from the
same authenticated server as the HTTP and WebSocket APIs.

Remote and persisted content is rendered as untrusted text unless it crosses an
explicit sanitization boundary. Production assets are same-origin and require
no CDN. The browser remains semantic and accessible; terminal-byte `web-tui` is
removed.

### Custom plugins

Built-in features are compiled Go packages using internal interfaces. Only
custom user and project plugins use the public subprocess boundary.

External plugin protocol v1 remains the initial compatibility contract:
JSON-RPC 2.0 over newline-delimited stdio, a language-neutral manifest, schemas
as normative wire artifacts, stderr for diagnostics, and no shell wrapping of
launch commands.

Each running session initially owns its plugin process instances. Process
identity is effectively `(session ID, plugin ID)`. This preserves v1's
single-session context and keeps tool and UI operations unambiguous. A future
protocol may multiplex explicit session contexts through daemon-wide plugin
processes.

Plugin resilience initially means isolation and graceful degradation:

- invalid output, initialization failure, timeout, or process exit cannot crash
  Kit;
- owned contributions are removed atomically;
- active requests fail with typed errors;
- bounded diagnostics and exit information are retained;
- restart/reload is explicit.

Automatic restart policy is deferred. The process boundary is a reliability and
API boundary, not a security sandbox. Plugins run as the same OS user and must
be treated as trusted code.

### Single-user remote access

Kit is always single-user. It will not add accounts, organizations, tenant
routing, or a cloud control plane.

The private daemon listener remains loopback-only. Explicit remote serving uses
a separate listener and token authentication. CLI clients use bearer
credentials; a browser may exchange a token for a secure HTTP-only cookie.
Non-loopback deployments require TLS from a trusted tunnel or reverse proxy
unless the user makes an explicit insecure development choice. Host and Origin
validation remain active.

### Delivery and parity

Development proceeds through a thin end-to-end walking skeleton that proves the
risky boundaries: native client, daemon, droids run, SQLite, session protocol,
web client, remote attach, one external plugin, and one concurrent subagent.

The walking skeleton is not the merge gate. Because this is a replacement, the
branch is mergeable only after the parity ledger is complete against baseline
commit `5c6e112`, plus deliberate triage of relevant changes made on the target
branch during the rewrite.

Intentional non-parity is limited to recorded decisions, initially:

- remove `web-tui`;
- replace OpenTUI with vaxis/ui;
- replace the TypeScript/Pi agent core with the Kit-local Go/droids core;
- replace runtime storage with SQLite plus migration;
- run subagents concurrently under the runtime;
- preserve custom plugins only through the subprocess protocol.

## Initial repository direction

The exact package set should grow with real consumers, but the intended shape is:

```text
cmd/kit/                 executable composition root
internal/apphome/        paths and filesystem ownership
internal/daemon/         discovery, startup, lifecycle
internal/server/         authoritative server and session directory
internal/session/        parent runtime orchestration
internal/subagent/       supervised child execution
internal/plugin/         manifests, RPC, process supervision
internal/storage/        SQLite and migrations
internal/protocol/       canonical Go wire records and validation
internal/client/         shared Go server/session clients
internal/tui/            vaxis presentation
web/                     Solid/Mica browser source
docs/adrs/               current architecture decisions
docs/plugin-protocol/    public plugin wire contract
```

Packages are introduced to enforce ownership or dependency boundaries, not just
to hold shared types.

## Consequences

### Positive

- Users on macOS and Linux install one native executable.
- Agent and subagent work survives client detachment.
- Local use continuously exercises the same semantics required for remote use.
- Multiple sessions and subagents can make bounded progress concurrently.
- Plugin crashes are isolated and plugins remain language-neutral.
- SQLite provides transactional migrations and concurrency suitable for a
  persistent daemon.
- TUI and web clients cannot accidentally depend on server-local state.

### Trade-offs

- Rebuilding the OpenTUI client in vaxis/ui is substantial parity work.
- The daemon adds process discovery, authentication, upgrade, logging, and
  lifecycle responsibilities.
- Runtime, storage, protocol, and client projections intentionally duplicate
  some shapes.
- Per-session plugin processes can duplicate resource use.
- Browser development still needs Bun even though users do not.
- Existing session data needs an explicit semantic migration into droids- and
  Kit-owned records.
- Internalizing droids increases Kit's source and test surface and requires an
  explicit future decision about extraction or upstream synchronization.

## Deferred

- daemon-wide multiplexed external plugins;
- automatic plugin restart policies;
- accounts or multi-user hosting;
- a Kit-managed TLS certificate authority or public cloud control plane;
- exact resumption of interrupted provider streams or side-effecting tools;
- multiplexing several session bindings over one WebSocket;
- native session tabs beyond what parity requires;
- idle session and daemon eviction policy.

## Related

- [`../parity.md`](../parity.md)
- [0002: Internalize the droids agent core](./0002-internalize-agent-core.md)
- [0003: Provider credential storage](./0003-provider-credential-storage.md)
- Historical implementation and ADRs at Git commit `5c6e112`
- External plugin v1 specification at
  [`../../app/docs/plugin-protocol/v1.md`](../../app/docs/plugin-protocol/v1.md)
