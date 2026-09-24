# 0026: Scope plugin processes and route plugin UI through sessions

## Status

Accepted. Print-client interaction policy is governed by
[ADR 0027](0027-treat-print-as-an-ordinary-session-client.md).

## Context

The daemon can host concurrent sessions with multiple attached clients or no
attached clients. External plugins require unambiguous ownership of their
process state, contributions, interactions, and client-local side effects.
A client attachment is not the lifetime of a session or its plugin instances.

## Decision

### Session-owned processes

The initial external plugin host uses the v1 subprocess protocol with one
instance per `(session ID, plugin ID)`. User-installed and project-installed
plugins both have session-local instance state and contributions. Application-wide
instances and daemon-wide multiplexing are deferred.

Plugin processes continue running while the owning session runtime is loaded,
including when it is idle and no clients are attached. Switching sessions in a
client does not retarget a plugin process. Detachment does not suspend plugin
work or imply that external side effects, such as audio, are muted. Plugin
memory is not guaranteed to survive runtime disposal or process/daemon restart.

On a session cwd change, user-plugin instances remain alive and receive a
project-change event. Kit removes old project contributions and stops their
processes before discovering and initializing project plugins for the new cwd.
Other sessions are unaffected. This ownership model is also specified in
[ADR 0001](0001-native-go-architecture.md).

### Live contribution updates

Plugin contribution changes take effect as soon as possible through the
session's live configuration path. Kit does not defer them until the current
turn finishes or require a reload to activate an accepted registration, update,
or removal.

Commands and chrome updates are published to clients immediately. Runtime
contributions become effective at the next applicable consumption boundary:
subsequent model requests use the updated tool catalog and prompt contributions,
and subsequent tool or subagent dispatch uses the current applicable
contributions. A model request already sent cannot be retroactively changed.
Changes within a session remain serialized and ownership-checked. Removing a
plugin-provided subagent definition prevents new child creation but does not
abort or retarget an already-created durable child conversation; that child
retains its admitted definition and normal supervised lifecycle.

### Automatic loading and nonblocking startup

User and project plugins load automatically from their configured discovery
locations. There is no additional project approval workflow. Plugins execute as
the same OS user and inherit the launch environment; opening a project with
plugin manifests authorizes execution of trusted local code, not sandboxed code.
V2 user discovery uses the resolved v2 home, defaulting to `~/.kit-v2`, and does
not inspect `~/.kit` without explicit migration.

Plugin initialization runs in the background without a readiness barrier before
turns. Model and tool work can begin before plugin initialization and registration
finish, including after project-plugin replacement. Early work may therefore
lack plugin tools, interceptors, or prompt contributions. This is an intentional
availability tradeoff, not a guarantee that plugin policies protect every turn.
Initialization and process shutdown remain bounded.

A failed plugin does not block session work or prevent healthy plugins from
loading. Kit removes failed-instance contributions, fails affected calls, and
reports a persistent failure. A missing or failed policy plugin does not continue
enforcing its policy. Recovery remains explicit through session reload; separate
per-plugin restart controls are outside the native scope.

### Reload, crashes, and in-flight work

Reload is session-scoped and cancels in-flight plugin work rather than draining
it indefinitely. Kit revokes the old instance's contributions, cancels its
outstanding requests in both directions and owned interactions (including nested
approval dialogs), and rejects new work for that instance. Shutdown allows a
bounded graceful exit before terminating and, if necessary, force-killing the
process group. Kit then initializes replacement instances without resetting
plugins belonging to other sessions.

Each replacement has a new instance generation. Late responses from a revoked
instance cannot mutate session state or settle replacement-instance requests.
An RPC already dispatched to an old instance is never reassigned to its
replacement. Crash cleanup uses the same ownership revocation and cancellation
mechanism; recovery uses explicit session reload rather than automatic or
per-plugin restart.

Failures are scoped to affected operations:

- A command reports failure without automatic retry.
- A plugin tool returns a failed tool result so the agent can decide how to
  proceed; plugin failure alone does not automatically abort the entire turn.
- An interceptor failure blocks the tool call awaiting its decision. Failure,
  cancellation, or disappearance of an approval request never counts as approval.
- A background plugin failure removes its contributions, emits a persistent
  Kit-owned failure notification to attached clients, and records its bounded
  summary and stderr evidence in the private server log.
- Unrelated operations and healthy plugins continue.

Once a failed interceptor's registration is removed, later tool calls may
proceed without that interceptor. Blocking the affected call does not imply
continued enforcement of the failed plugin's policy.

Interrupted operations are not automatically replayed on reload.
A plugin may perform external side effects before returning a response; losing
that response does not establish that the operation did nothing. Failure results
communicate an uncertain or potentially partially completed outcome when
appropriate. Cancelling requests or terminating a process is not rollback.

### Uniform client execution

Print is an ordinary session client, as specified by
[ADR 0027](0027-treat-print-as-an-ordinary-session-client.md). Plugin loading and
interactive requests follow the same session-owned policy for every client.
Print mode and temporary client detachment do not disable the shared interaction
broker or turn pending requests into unavailable/cancellation results.
Diagnostics remain separate from result stdout and plugin protocol stdout.

### Shared user interactions

Plugin confirm, input, and select requests are server-owned session interactions.
All capable clients attached to the session may render and answer them, including
requests originating from a command invoked by one client. The first valid
response wins. Individual client detachment does not cancel the request.

An interactive session retains a pending plugin interaction when no capable
client is attached so a later attachment can answer it. Caller cancellation or
plugin shutdown cancels owned requests. The broker identifies plugin ownership
explicitly; command-originated requests do not require fabricated model-run or
tool-call identities. Existing model interaction semantics remain described by
[ADR 0012](0012-model-user-interactions.md).

### Bottom-right footer contributions

The bottom-right footer is the only plugin-configurable chrome region. The
header and bottom-left status area remain Kit-owned and cannot be contributed
to or hidden by plugins.

Kit's cwd/Git presentation is the default bottom-right content, identified by
`kit.footer.location`. Plugins may compose multiple footer items alongside it
or hide that default and supply replacement content. Composition uses footer
set/clear operations and scoped hide/show claims; it does not require a separate
customization editor. Hide claims may target only items within the configurable
bottom-right region. Show removes only the caller's claim, and an item remains
hidden while any plugin owns a claim. Revoking an instance removes its items
and claims, restoring default content when no remaining claim hides it.

Footer items retain stable ownership, deterministic ordering, theme-token text
styling, and static content. Updating an item preserves
its order. Kit owns bounded layout and overflow behavior so contributions cannot
encroach on the bottom-left status area or other shell regions.

Header contribution operations, left-side footer contributions, and hide/show
operations targeting protected chrome are explicitly unsupported. Kit reports
an error rather than silently redirecting them into the bottom-right region.
The supported external protocol surface must document this restriction.

### Plugin footer actions and URL opening

Plugin footer click callbacks, declarative URL actions, and
`kit/system/open-url` are outside the native implementation scope, not deferred
work. Static footer composition satisfies the plugin customization needs; an
originating-client side-effect routing contract is not required. Requests for
these capabilities are rejected as unsupported.

Kit-owned links, including the built-in pull request label, remain client-local
actions and do not expose a plugin URL-opening capability.

### Session-scoped notifications

Plugin toasts are session-scoped notifications presented by clients viewing that
session, including command-originated feedback. Persistent plugin failures remain
discoverable after reconnect. Stale transient toasts are not replayed on a later
attachment.

### Session information without autonomous submission

Autonomous plugin message submission is outside the native implementation scope.
A plugin cannot impersonate user speech or start or queue a model turn. A future
bounded, provenance-preserving channel may let a plugin inform only its owning
session at a deliberate consumption boundary without changing an already-sent
model request. Its retention, client visibility, consumption, and lifecycle
semantics must be specified before adding a public method.

## Consequences

- Existing single-session wire requests have an unambiguous session owner.
- Session-local instances can duplicate resource use and do not share in-memory
  toggles across sessions.
- Shared interactions remain answerable across detach and reconnect without
  tying server work to a widget or one client connection.
- Static plugin footer content does not require client-origin tracking or
  plugin-triggered browser side effects.
- Session notification state and client-side presentation are separate concerns.
- Plugins cannot autonomously trigger provider work by submitting session messages.
- Background initialization favors immediate session availability over plugin
  readiness; plugin policies are not an always-on enforcement boundary.
- Automatic project loading requires users to trust repositories containing
  executable plugin manifests.
- Reload is prompt and bounded but can interrupt plugin operations. Neither
  reload nor crash recovery provides rollback or exactly-once external effects.

Implementation requirements remain in the [core plugin backlog](../../backlog/core.md)
and [TUI plugin backlog](../../backlog/tui.md).
