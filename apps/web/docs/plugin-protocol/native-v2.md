# Native v2 implementation profile

This document describes the **implemented subset** of the language-neutral
[v1 protocol](v1.md) in Kit's native Go rewrite. It is not a declaration of full
v1 conformance. The v1 schemas describe the target protocol; a schema-valid
method is not necessarily implemented by this host.

## Installation and ownership

The default user discovery path is `~/.kit/plugins/*/plugin.json`;
`KIT_HOME` overrides the home directory. Project discovery
continues to use `<session-cwd>/.kit/plugins/*/plugin.json`. The manifest-v1
format and shell-free launch rules in v1 apply. Python fixtures require Python
in `PATH`; Kit does not install it.

Plugins are trusted same-user programs, launched automatically with inherited
environment and the manifest directory as process cwd. This is not a sandbox
or an approval boundary. Only open projects containing plugins you trust.

Each loaded session runtime owns independent processes and instance generations.
Initialization proceeds in the background; attaching a client or starting a turn
does not wait for every plugin. A client disconnect does not unload plugins.
Session rename and cwd changes update plugin context; user plugin instances
survive those changes, while a cwd change replaces project plugin instances.
Explicit session reload replaces all instances. Crashed plugins are not
restarted automatically.

Contribution ownership includes the host lifetime and instance generation.
Command selections additionally carry a monotonic registration identity, so
unregistering and re-registering the same ID cannot retarget an old selection.
Revocation removes command availability and cancels pending plugin calls and
dialogs. An old selection or response cannot authorize a replacement instance
or registration. Cancellation does not imply rollback of effects already
performed; Kit does not automatically replay failed commands.

## Supported surface

- Initialization, shutdown, full-duplex JSON-RPC, cancellation, and session/project
  context synchronization.
- `kit/commands/register`, `kit/commands/unregister`, and Kit-to-plugin
  `kit/commands/execute`.
- `kit/tools/register`, `kit/tools/unregister`, and Kit-to-plugin
  `kit/tools/execute`, including tool-owned prompt snippets/guidelines.
- `kit/tool-calls/register-interceptor`, `kit/tool-calls/unregister-interceptor`,
  and Kit-to-plugin `kit/tool-calls/before-execute`.
- `kit/ui/toast` notifications.
- `kit/events/git.changed` live Git projection notifications.
- `kit/events/agent.turn.started` and `kit/events/agent.turn.completed`
  lifecycle notifications.
- `kit/subagents/register` and `kit/subagents/unregister` live child-agent
  definitions.
- Static `kit/footer/set`, `kit/footer/clear`, `kit/footer/hide`, and
  `kit/footer/show` contributions in the TUI and macOS app.
- `kit/ui/confirm`, `kit/ui/input`, and `kit/ui/select` requests through the
  shared session interaction broker and native interaction dock.

Commands appear in the TUI and macOS app palettes under their plugin-qualified IDs. They
receive literal arguments without starting a model turn or replacing the
composer draft. The native client allows one command invocation per attachment,
with a two-minute wait deadline. Switching sessions cancels that client's wait;
it does not independently dismiss a session-owned dialog.

Autonomous message submission through `kit/session/submit-message` is outside
native scope, not
deferred work. A separate future channel may let plugins provide bounded,
attributed information to their owning session without starting or queuing a
model turn; it has no public method yet. Plugin footer click callbacks,
declarative URL actions, and `kit/system/open-url` are also outside native scope,
not deferred work. Kit-owned links, including the
built-in pull request label, are unaffected. Unknown/unsupported requests return a
JSON-RPC method-not-found error. Notifications do not receive responses.

General system-prompt slots (`kit/system-prompt/set` and
`kit/system-prompt/clear`) are outside the native implementation scope.
Tool-owned descriptions, snippets, and guidelines remain supported.

The target chrome customization surface is **bottom-right footer only**.
Header and bottom-left status operations are unsupported, not redirected.
Static footer items and aggregate hide claims are projected through live session
snapshots to both clients. Styling uses semantic tokens; width-aware presentation
uses labeled overflow without encroaching on status or workspace controls. The
macOS right-hand slot uses its full allocated width, and its overflow button opens
a scrollable popover containing the full styled content and visible location. Click
dispatch and declarative URL actions are explicitly rejected as unsupported
rather than silently ignored; they are not planned native capabilities. See
[ADR 0026](../../../../docs/adrs/0026-scope-plugin-processes-and-route-plugin-ui.md)
for the supported scope.

Persistent plugin-authored notices remain in host diagnostics. A completed
plugin failure also emits one Kit-owned persistent error notification to attached
clients and writes its bounded summary and retained stderr tail to the private
`logs/server.log`. The notification stream remains live-only, so failures that
occur without an attached client are recovered from the log and session warning
projection rather than replayed as notifications. The log is private to the user
but may contain sensitive text written by a plugin to stderr; its default path is
`~/.kit/logs/server.log`. Session reload is the recovery mechanism; dedicated
diagnostics and per-plugin restart UI are outside native scope.

## Transport and resource limits

KiB and MiB are binary units. String limits below count UTF-8 bytes, not display
cells. Encoded JSON limits also count escaping and structural overhead.

| Resource | Native host bound |
| --- | --- |
| Manifest file | 1 MiB |
| Installations planned per session | 128 |
| Tracked processes, including retiring instances | 128 per session |
| NDJSON frame before newline | 16 MiB (including CR for CRLF) |
| Queued plus currently writing outbound data | 32 MiB per process |
| Concurrent inbound requests | 128 per process |
| Pending outbound requests | 128 per process |
| Queued incoming notifications | 128 per process |
| Members in one JSON-RPC batch | 1,024 |
| Retained batch response data | 16 MiB frame and shared 32 MiB buffered-data budgets |
| Incoming request/notification and retained batch data | Shared 32 MiB budget per endpoint |
| Initialization deadline | 10 seconds |
| Graceful shutdown / process cleanup | 2 seconds each |
| Captured stderr tail | 64 KiB per process |
| Failure diagnostic message | 4,096 bytes |
| Retained host diagnostics | 32 entries per category |
| Registered commands | 256 per session |
| Literal command arguments | 64 KiB before JSON encoding |
| Encoded registration/toast/dialog params | 64 KiB |
| Local command ID | 128 bytes |
| Command description | 1,024 bytes |
| Command argument hint/category | 128 bytes each |

Oversized frames, outbound queue exhaustion, malformed protocol output, and
notification flooding can terminate the instance. Excess inbound requests
receive `-32006`; unavailable outgoing-call capacity fails locally rather than
creating an unbounded wait queue. Installation/process limits retain a host
diagnostic rather than launching unlimited children. Batch and output limits
apply independently: splitting work into a batch does not bypass admission or
memory bounds.

Contribution objects are parsed strictly: duplicate/unknown fields and invalid
shapes are rejected. Display strings reject invalid UTF-8, terminal controls,
and Unicode format controls; multiline fields permit newline and tab.
Opaque select values are JSON data, not display strings.

## Static footer contributions and bounds

`kit/footer/set` accepts local IDs and normalized styled segments, defaults to
`side: "right"`, and returns the plugin-qualified ID. Updating preserves the
item's first-registration position; clearing then recreating appends it. Text is
single-line, valid UTF-8 without terminal or format controls. Styles retain
validated public v1 theme tokens and boolean attributes rather than colors.
Explicit `side: "left"`, `clickable: true`, and `action` are unsupported.

`kit/footer/clear` removes only the caller's item and is idempotent. Hide/show
accept canonical footer IDs, including `kit.footer.location`; header IDs and
other protected built-ins are rejected. Claims may name a plugin footer item
before it exists. Hide is idempotent for one owner/target, and show removes only
that owner's claim. Items remain hidden while any active owner claims them.
Revocation removes an owner's items and claims; user-plugin claims survive cwd
changes while project-plugin claims do not. The host projects only visible
items and the aggregate location-hidden state.

Bounds apply across one session's plugin host:

- 64 footer items, 256 hide claims, and 64 KiB encoded request params.
- 32 segments and 4,096 total UTF-8 text bytes per item.
- 128 bytes for a local item ID; canonical hide targets must be valid namespaced
  IDs or the configurable built-in location ID.
- Updates and already-owned hide claims remain allowed at capacity.

The repository’s `footer-demo` fixture exercises set/replace/clear and reload.
Automated coverage includes the real subprocess-to-daemon snapshot path and both
client presentations. The user manually verified footer rendering, overflow,
and lifecycle behavior; plugin click and URL support are excluded from native
scope.

## Tool registration and execution

Tool schemas follow the v1 keyword allowlist and are validated at registration.
Schemas cannot resolve references or fetch external resources. Model inputs are
validated as JSON objects without defaults, coercion, or property removal. JSON
numbers retain their exact values through validation and subprocess dispatch.
Schema nodes must be objects; `additionalProperties` accepts a boolean.
The current regex engine is Go's RE2-compatible `regexp`; unsupported patterns
are rejected at registration.

Accepted catalog changes update the next model request without a reload or turn
completion barrier. An in-flight request retains its advertised definitions and
callbacks. Each tool has both an instance generation and a unique registration
identity; unregister/re-register cannot retarget an old model response. Recovery
also fences durable tool-call registration identities and fails closed when a
dynamic definition cannot safely be recovered. The admitted batch execution mode
is persisted too; recovery never adopts a replacement registration’s scheduling
policy. Records without a captured mode recover sequentially. A failure does not automatically
replay the call against a replacement.

Sequential/parallel execution declarations use the agent's existing tool
scheduler. Results support text, base64 PNG/JPEG/GIF/WebP images, opaque JSON
`details`, and `terminate`. Termination completes normally after the containing
batch is durable. Invalid result shapes fail the instance; cancelled/failed
operations may already have produced partial effects. V1 has no streaming
plugin-tool progress method.

| Tool resource | Bound |
| --- | --- |
| Registered tools | 128 per session |
| Encoded registration | 64 KiB |
| Model-facing name | 64 ASCII characters, `plugin-id__local_snake_case` |
| Label | 512 bytes |
| Description / prompt snippet | 8 KiB each |
| Prompt guidelines | 32 strings, 1,024 bytes each |
| Schema complexity | 1,024 schema nodes, 16 levels |
| Encoded tool input | 64 KiB |
| Tool-call ID | 512 bytes |
| Result content | 64 parts; 4 MiB aggregate decoded text/image bytes |
| Result details | 64 KiB canonical JSON |
| Parsed input/schema/result JSON | 64 nesting levels, 32,768 value nodes |
| JSON numbers in tool payloads | 1,024 characters; exponent magnitude at most 1,024 |

The transport frame limit also applies. Objects reject duplicate keys, including
inside schemas, inputs, and tool results. Tool-provided prompt guidance is removed
alongside its catalog contribution. Base session prompt/thinking reconfiguration
preserves independently owned tool contributions.

The read-only `.kit/plugins/tool-demo` fixture is covered by a real subprocess →
daemon → model tool-call → durable transcript test. A live-session echo smoke
check returned `plugin tools work`, and the user verified the broader native
lifecycle behavior. Interception is covered separately below.

## Plugin-provided subagents

`kit/subagents/register` contributes a definition to Kit's existing supervised
subagent system; it does not execute child work inside the plugin. The canonical
model/client-facing name is `<plugin-id>.<local-id>`, and registration returns
that name. `kit/subagents/unregister` accepts the local id and is idempotent.

Definitions apply live to the next model request and to subsequent `subagent`
tool dispatch. A stale model callback cannot start a definition after its owner
is revoked. Unregister, plugin failure, reload, and project-plugin replacement
remove future availability but do not abort or mutate an already-created durable
child conversation. Existing conversations remain inspectable, steerable,
cancellable, and dismissible under their snapshotted definition.

Filesystem definitions take precedence over a conflicting canonical plugin
name. Registration rejects a conflict with `-32003`; if a replacement base
catalog introduces a conflict, Kit removes the plugin contribution and retains
a warning. The effective catalog remains limited to 128 definitions. Canonical
names are at most 128 bytes, descriptions 1,024 bytes, optional model selectors
256 bytes, and instructions 128 KiB, subject to the stricter 64 KiB encoded
registration bound. Instructions allow newline and tab but reject other control
and Unicode format characters. Source metadata identifies the plugin and its
manifest without exposing process arguments.

No plugin-specific client surface is added. Definitions and conversations use
the existing native roster, activity, retained conversation tabs, mailbox, and
completion notifications.

## Dialog semantics and bounds

A dialog belongs to the session and plugin generation, not a fabricated model
run/tool call and not one client connection. The oldest pending request occupies
the native interaction dock. Other clients can see and answer the same request;
the first valid response wins. Disconnecting and reconnecting preserves pending
requests while their owning runtime/generation remains available.

Only option IDs, labels, and descriptions are projected to clients. Arbitrary
JSON option values stay inside the plugin host, preserving large integers and
other exact JSON data. Selection returns `{"value": <original JSON>}`; selecting
a JSON-null option returns `{"value":null}`, distinct from cancellation.

Explicit user cancellation returns `false` for confirm and JSON `null` for input
or select. An empty input answer is a valid empty string, not cancellation.
Revocation, runtime shutdown, and other infrastructure failures are errors and
must not approve the waiting operation. Labels, default confirmation focus,
initial input and input placeholders are supported. Both the TUI and macOS app
deliberately render plain option lists. The v1 `filterable` field remains accepted
on the wire but does not enable filtering in either client.

| Dialog resource | Bound |
| --- | --- |
| Pending plugin callbacks | 8 per host/session |
| Shared pending broker requests, including model interactions | 8 per session |
| Encoded request params | 64 KiB |
| Options | 64 |
| Title, placeholder, option label | 512 bytes each |
| Message, option description | 8 KiB each |
| Confirm/cancel labels | 128 bytes each |
| Initial input and submitted input | 16 KiB each |
| Each opaque option value | 16 KiB encoded JSON |

Dialog capacity or encoded-params exhaustion returns `-32005`; invalid parameter
shapes or display-field bounds return `-32602`.
An unavailable owner returns `-32002` or cancellation according to the transport
lifetime. Hosts without an interaction adapter return `-32000` with
`data: {"reason":"interactivity_unavailable"}`.

The native daemon installs the shared session interaction adapter for every
client, including print. There is no separate noninteractive plugin mode and
none is planned: [ADR 0027](../../../../docs/adrs/0027-treat-print-as-an-ordinary-session-client.md)
treats print as an ordinary session client. A dialog may remain pending until a
capable client answers or normal request/instance/runtime cancellation ends it.
Print mode and temporary absence of clients do not cause automatic rejection or
substitute a negative answer. Print does not guarantee unattended completion.

## Toast delivery

Toast titles are bounded to 1,024 bytes; subtitles to 4,096 bytes. Supported
variants are `info`, `warning`, and `error`. The encoded params bound above also
applies. Persistent plugin-authored toasts additionally retain a host diagnostic.
Kit also emits a persistent error notification when a plugin generation reaches its
final failed state; its bounded summary and retained stderr tail are written to
`logs/server.log`.

The TUI and macOS app receive session notifications through an authenticated live-only NDJSON
subscription, separate from durable session event replay:

- At most 32 subscribers per session.
- At most 16 queued toasts per subscriber; overflow drops without blocking the
  plugin, session, or other clients.
- Client decoding is bounded to 32 KiB frames and validates raw UTF-8 and shape.
- Heartbeats occur every 15 seconds; server writes have a 10-second deadline.
- There is no transient offline storage, cursor, or replay on reconnect.

The macOS app uses its existing session alerts, preserving severity and plugin
provenance rather than introducing a separate toast UI. It retains at most 32
persistent plugin alerts per session, supports their normal dismissal, and clears
transient plugin alerts on detach. Its notification subscription reconnects fresh
and rejects callbacks from old attachments. Plugin provenance is also visible in
TUI notifications. Successful command
completion does not add a separate automatic completion toast.

## Verification and examples

The repository's `.kit/plugins/plugin-demo` exercises commands, literal
arguments, session context, and toasts. `.kit/plugins/ui-api-demo` exercises
select → select → input → confirm → toast. `.kit/plugins/subagent-demo`
registers a live reviewer definition and unregisters it through a command. These
fixtures do not require a parent model turn.

Automated coverage includes real subprocess/daemon subagent registration,
native child start, definition removal with retained child inspection, real
dialog execution, concurrent answers, cancellation from another client
connection, session deletion, reload
revocation, broker shutdown lock ordering, and native keyboard presentation.
The user confirmed the ui-api-demo dialog flow works in the TUI on macOS and
verified plugin notification delivery through the macOS app’s session alerts.
Linux-specific manual subprocess verification is not a native-scope completion
gate; portable behavior remains covered by the Go test suite on the platforms
where that suite runs.

Future extensions beyond this implemented native profile are tracked in
[the core backlog](../../../../backlog/core.md), not implied by the examples.

## Tool-call interception

`kit/tool-calls/register-interceptor` and
`kit/tool-calls/unregister-interceptor` accept no params and are idempotent.
Each instance owns at most one interceptor (128 per session). Registration
order determines sequential `kit/tool-calls/before-execute` calls; separate tool
calls may be intercepted concurrently. Both core and plugin tools are eligible.
Session-owned subagent tools resolve their owning session's policy too.

The first `reject-and-continue` response blocks that tool and supplies its message
to the model. Only an explicit `allow` continues. Transport errors, invalid
responses, missing/replaced policy, or process failure block the affected tool;
they do not automatically abort the entire turn. Ordinary run cancellation still
cancels the turn. No failed or interrupted tool is automatically replayed.

The native profile bounds intercepted input to 1 MiB, call ID and model-facing
name to 512 bytes each, encoded decision objects to 64 KiB, and rejection messages
to 8 KiB. JSON input is forwarded without floating-point conversion. These input
limits apply when an interceptor is registered, not to unintercepted tools.

Tool admission stores a bounded policy identity (host lifetime plus ordered
registration revision). Changing registration, including unregister/re-register,
invalidates pending policy work. Revoked owners are removed and cancel waiting
interception. Identity checks run before the hook and immediately before tool
code, so a parallel call waiting behind another approval cannot execute under
an obsolete decision. Once tool execution is admitted, revocation cannot roll
back its side effects. Recovery under another host or from a legacy record without
a captured policy identity blocks old calls instead of silently approving them.

Interceptors may request shared session dialogs. `kit/cancel` cancels the
interceptor RPC; plugins **must propagate cancellation to their nested requests**,
because v1 has no parent-request IDs. The dependency-free approval test fixture
under `internal/plugin/testdata` demonstrates this propagation. It is not
installed automatically and is not a sandbox or machine security boundary.

Automated coverage includes real daemon approval/rejection of a core tool,
closing nested dialogs on abort, ordering, malformed decisions, unregister/reload,
parallel prepared calls, and actual SQLite reopen/recovery fencing. Live-session
approval, rejection, and lifecycle behavior were manually verified.

## Turn lifecycle notifications

Ready session plugin generations receive `kit/events/agent.turn.started` after
canonical durable turn admission and before model/tool activity. Completion is
emitted after terminal settlement for successful, failed, and aborted turns.
Delivery is ordered and live-only: only the same still-active generations that
received start are eligible for completion. Late-ready or replacement instances
do not inherit an earlier turn. Attach/reconnect and recovered historical or
interrupted turns do not replay lifecycle notifications.

Completion includes ordered user/assistant text only, retaining textual parts of
multipart input while excluding thinking, tool calls/results, images, annotations,
synthetic context, provider metadata, usage, and persistence fields. Owning-session
autonomous reactions use the owning session identity. Child subagent turns do not
fabricate parent-session lifecycle events.

The native completed-event frame limit is **256 KiB**. Oversized events are
omitted intact with a host diagnostic, not silently truncated. Canonical history
projection has a two-second timeout; the terminal delivery wait is bounded to
three seconds. Projection/read/encoding failures or an exceeded settlement
boundary likewise omit the completion with diagnostics. A delivered start
therefore does not guarantee a completion notification. No plugin acknowledgment
is awaited and notifications are never replayed to make up for omissions.

Runtime disposal cancels and joins the projection worker before closing its
store. Caller cancellation stops only that caller's wait; manager-owned cleanup
continues exactly once. Tests cover real subprocess delivery, queued-turn
ordering, failed/aborted settlement, generation fencing, omission limits,
canceled disposal with in-flight projection, and actual SQLite reopen/recovery
without replay. Live-session start/completion ordering, matching identities,
text-only projection, and failure/reload lifecycle behavior were manually
verified; the tool-call/output marker was excluded.

## Live Git notifications

Each loaded session owns a server-side observer shared by native client streams
and subprocess plugins. It probes Git immediately and every ten seconds, with a
two-second probe budget, independently of client attachments and plugin startup.
Shutdown cancels and joins observation; cwd changes revoke old probes and clear
old workspace metadata before observing the new directory.

`kit/events/git.changed` carries
`{"git":{"root":...,"branch":...,"dirty":...,"pullRequest":...}}` or `{"git":null}`.
Detached HEAD has a null branch. Null Git means no usable metadata, including a
non-worktree or a repository that cannot be inspected safely. `pullRequest` is
optional in the public schema; native v2 always emits it as either
`{"number":123,"url":"https://..."}` or null while unavailable/loading. PR numbers
are positive JavaScript-safe integers; URLs are absolute credential-free HTTP(S),
bounded to 4096 UTF-8 bytes and free of whitespace, backslashes and control/format
characters. GitHub lookup is optional, uses the user's `gh` authentication, and
never delays local Git delivery. Failures remain silent.

Positive and negative PR lookups are cached for 60 seconds, with bounded cache,
output, concurrency, and subprocess lifetime. Completion wakes the shared
observer without waiting for another Git poll. Changes to PR metadata alone,
including removal, generate `git.changed`; details outside the public Git/PR
projection do not. Plugins need no subscription flag to consume the new member.
Strict schema validators must use the updated public schema.

Initialization supplies each generation's current baseline without an extra Git
event. Subsequent public projection changes are deduplicated per generation and
queued without acknowledgment or replay; intermediate changes may coalesce.
Retained user plugins receive cwd-related `project.changed` before a changed Git
projection. Samples cannot cross cwd/reload epochs or target replacement processes.
Delivery uses the existing bounded instance writer; a failed/full writer revokes
the affected generation rather than blocking a turn. Native clients independently
subscribe to latest-state VCS streams and reconnect with a fresh snapshot.
