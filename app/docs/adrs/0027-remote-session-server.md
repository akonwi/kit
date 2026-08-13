# 0027: Remote session web mode

## Status

Proposed

## Context

ADR 0026 introduced a persistent headless RPC mode over JSONL stdio. That mode
separates Kit's agent runtime from OpenTUI and is sufficient for subprocess
hosts, but stdio is not a network transport and supports only the process that
owns the stream.

Kit should also support remote clients in two environments:

1. Kit runs on a user's computer while a browser controls the session from
   elsewhere. Execution, files, credentials, and tools remain local.
2. Kit runs inside a cloud sandbox and is controlled by either a browser or a
   remotely connected Kit TUI.

These are the same architectural problem with different runtime placement. A
separate protocol or server for each environment would duplicate session,
streaming, interaction, and reconnection behavior.

The current headless host also provides less functionality than interactive
Kit. It initializes built-in plugins with an inert UI implementation, does not
initialize external plugins, and cannot delegate confirmation, input,
selection, approval, or attachment interactions to a remote client. A remote
session server needs deliberate parity rather than inheriting those omissions.

## Decision

Kit will introduce `kit --web` as the network-hosted counterpart to the
normal interactive and headless RPC modes. It owns a persistent Kit session
and exposes that session to remote clients over HTTP.

Web mode will use:

- HTTP for serving the web client, health information, and binary transfer such
  as attachments.
- WebSocket for bidirectional RPC commands, correlated responses, runtime
  events, and interactive UI requests.

The WebSocket protocol will retain the command, response, and event model from
ADR 0026. A WebSocket message carries one protocol record, while the stdio
transport continues to use one JSON record per line. The external-plugin
JSON-RPC protocol from ADR 0025 remains a separate protocol with the opposite
process-ownership relationship.

### Shared session host

RPC dispatch and event publication will be transport-independent. The event
source is Kit's semantic `AgentRuntime` surface above the core `Agent`; the host
will not expose raw Pi lifecycle events or provider-specific payload types.
Transport-safe records preserve Kit event names, turn/message identities, and
lifecycle meaning while projecting or bounding unsafe data. The same session
host will back:

- the existing `kit --rpc` stdio adapter
- the `kit --web` HTTP and WebSocket adapter

Web mode will host the runtime directly rather than spawning a nested
`kit --rpc` process. Closing a network connection will not terminate the
hosted session or abort an active agent run.

### Browser client

The initial browser client will use an HTML-first stack built on Mica, the
project's progressive custom-element component library and design system. It
will prefer native browser behavior, CSS, and small JavaScript modules over a
client framework, and it will not use React. The client should emulate Kit's
TUI information hierarchy and visual language without coupling protocol work to
a full UI port. A different lightweight framework may be considered later if
client-state complexity warrants it.

### Runtime parity

Server mode is a full Kit host rather than a reduced print-mode environment. It
will initialize the built-in and external plugin systems appropriate to the
hosted workspace.

Renderer-dependent interactions will go through a remote UI boundary. The
server can issue correlated interaction requests for confirmation, input,
selection, and guided questions. Tool approval remains policy-owned by runtime
interceptors and composes through a nested confirmation request rather than a
second approval-specific protocol primitive. Requests are broadcast to all
connected clients; the first valid response resolves the interaction, and that
resolution is broadcast so other clients dismiss it. Interactive requests must
have defined behavior when no client is connected.

Plugin header and footer contributions remain host-owned state. Web snapshots
and live events project their stable ids, side, semantic token styling,
clickability, and built-in hide claims; clients activate clickable items through
a correlated host command rather than receiving plugin callbacks or literal
terminal colors. Declarative HTTP(S) chrome actions cross as validated data:
browsers render native isolated links, while terminal renderers retain their
platform opener.

The remote protocol will expose the session, command, workspace, model,
thinking, attachment, steering, abort, and state-retrieval operations required
for a client to provide the normal Kit workflow. Protocol capabilities and
versions will be discoverable rather than inferred by clients.

### Reconnection and multi-client control

Runtime events have a host-instance stream ID and ordered sequence identifiers.
The web transport journals a bounded, contiguous suffix after applying its
browser-safe projection. A reconnecting client supplies the prior stream ID and
its last applied sequence in the WebSocket URL. The server captures one
high-water mark, synchronously sends either the retained suffix or a bounded
state/message/interaction snapshot, marks synchronization complete, and only
then enables live delivery. Command responses remain connection-scoped and
unsequenced.

Replay starts from the client's supplied sequence rather than advertising the
terminal cursor as already applied. A completion record carries the high-water
mark after all replay records have been sent. Stream changes, invalid cursors,
evicted history, and oversized history fall back to a snapshot. Snapshot
transcripts are bounded and expose offsets so clients can page older messages
through the command protocol.

Multiple clients may connect to and fully control the same authoritative
runtime. Commands are correlated per connection and serialized before mutating
shared session state. Runtime events are broadcast to every connected client,
so prompts, aborts, model or session changes, and interaction resolutions from
one client become visible to the others. Existing runtime rules continue to
reject or redirect operations that are invalid while an agent run is active.

### Deployment and network exposure

The same `kit --web` process can run on a local computer or inside a cloud
sandbox. Tunnels, relays, sandbox platforms, and cloud control planes are
deployment concerns outside the session protocol.

The server will bind to loopback by default. Native authentication and
authorization are deferred from the initial mode. Remote access is expected to
run through a trusted boundary such as Tailscale or an SSH tunnel, which owns
its access controls. The server will reject permissive cross-origin access,
and direct public exposure is unsupported until an authentication layer is
added. Authentication can be layered on later without changing the session
protocol.

A local OpenTUI process and web mode must not independently mutate the same
persisted session. Initially, remote continuation requires the session to be
owned by `kit --web`. A future `kit attach` client may let the TUI consume
the same remote protocol, making browser and terminal interfaces clients of one
session host.

Cloud workspace provisioning, repository transfer, secret injection,
persistent volumes, resource accounting, suspension, and sandbox cleanup are
out of scope for web mode; a hosting layer composes those responsibilities
around it.

## Consequences

### Positive

- Local remote control and cloud execution share one server and client
  protocol.
- Web and future terminal clients observe the same authoritative session and
  event stream.
- Stdio RPC remains available for subprocess integrations without becoming the
  network boundary.
- Runtime and UI concerns become more explicitly separated.
- The initial web client shares the Mica-based design system already used by
  Kit's website without introducing a large client runtime.
- Cloud providers can run a standard Kit command instead of implementing a
  Kit-specific worker protocol.

### Trade-offs

- Kit must maintain a network server and a versioned remote protocol.
- RPC dispatch, interactive UI, and client state must no longer depend on one
  renderer or transport.
- The HTML-first client must manage streaming and shared-session state without
  assuming a component-framework runtime.
- Reconnection and multiple controlling clients introduce event ordering,
  retention, snapshot, command-serialization, and interaction-resolution
  semantics.
- Full server parity requires external-plugin initialization and remote
  implementations of currently inert headless UI capabilities.
- The existing TUI cannot attach to a served session until its presentation
  layer is separated from its in-process runtime ownership.
- Running Kit remotely expands the security impact of workspace isolation,
  credential handling, and network exposure; deployments must provide a trusted
  access boundary until Kit gains native access controls.

## Related

- `docs/adrs/0025-json-rpc-plugin-protocol.md`
- `docs/adrs/0026-headless-rpc-mode.md`
- `backlog/remote-session-server.md`
