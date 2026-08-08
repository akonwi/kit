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

Kit will introduce `kit --mode web` as the network-hosted counterpart to the
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

RPC dispatch and event publication will be transport-independent. The same
session host will back:

- the existing `kit --mode rpc` stdio adapter
- the `kit --mode web` HTTP and WebSocket adapter

Web mode will host the runtime directly rather than spawning a nested
`kit --mode rpc` process. Closing a network connection will not terminate the
hosted session or abort an active agent run.

### Runtime parity

Server mode is a full Kit host rather than a reduced print-mode environment. It
will initialize the built-in and external plugin systems appropriate to the
hosted workspace.

Renderer-dependent interactions will go through a remote UI boundary. The
server can issue correlated interaction requests for confirmation, input,
selection, guided questions, and tool approval, and the controlling client can
respond or cancel them. Interactive requests must have defined behavior when
no controller is connected.

The remote protocol will expose the session, command, workspace, model,
thinking, attachment, steering, abort, and state-retrieval operations required
for a client to provide the normal Kit workflow. Protocol capabilities and
versions will be discoverable rather than inferred by clients.

### Reconnection and ownership

Runtime events will have ordered sequence identifiers. A reconnecting client
can resume from its last observed event when retained history is available, or
recover from a current state and message snapshot before receiving new events.

The initial server will permit one controlling client. A second client must be
rejected or explicitly take control; collaborative writes are not implicit.
Read-only observation and multi-user collaboration may be added later without
changing the single authoritative runtime model.

### Deployment and network exposure

The same `kit --mode web` process can run on a local computer or inside a cloud
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
owned by `kit --mode web`. A future `kit attach` client may let the TUI consume
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
- Cloud providers can run a standard Kit command instead of implementing a
  Kit-specific worker protocol.

### Trade-offs

- Kit must maintain a network server and a versioned remote protocol.
- RPC dispatch, interactive UI, and client state must no longer depend on one
  renderer or transport.
- Reconnection introduces event ordering, retention, snapshot, and takeover
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
