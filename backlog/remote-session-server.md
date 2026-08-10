# Remote sessions through `kit --mode web`

Kit's headless RPC mode makes it possible to separate the agent runtime from its
UI. Explore a `kit --mode web` entry point that runs a normal persistent Kit
session without OpenTUI and serves remote clients over HTTP.

## Use cases

1. Run Kit on a local computer and control the session from a web browser while
   away from that computer. Execution, files, credentials, and tools remain
   local.
2. Run Kit inside a cloud sandbox and control it through either the web UI or a
   remotely connected Kit TUI.

Both cases should use the same server and client protocol. Only the placement of
the Kit process differs.

## Proposed interface

```sh
# Create a persistent session in the current directory.
kit --mode web

# Resume an existing session.
kit --mode web --session <id>

# Listen inside a cloud sandbox or container.
kit --mode web --host 0.0.0.0 --port 4782 \
  --allow-host kit.example.internal:4782 \
  --allow-origin https://kit.example.internal
```

The default host should be `127.0.0.1`. The initial implementation does not
provide native access control; non-loopback listeners are expected to run
behind a trusted private network boundary.

## Transport

Use HTTP for static content, health checks, and binary uploads, with WebSocket
for the bidirectional RPC stream:

```text
GET  /                 web client
GET  /api/health       health and basic server state
POST /api/attachments  multipart attachment upload
WS   /api/rpc          commands, responses, events, and UI requests
```

WebSocket starts through HTTP and works behind HTTPS proxies, tunnels, cloud
ingress, and sandbox providers. Reuse the existing RPC command, response, and
event envelopes, with one JSON object per WebSocket frame instead of JSONL
framing.

Do not expose the raw stdio protocol directly to the network.

## Runtime architecture

Avoid implementing web mode by spawning `kit --rpc` as a child. Extract
the current RPC dispatch into a transport-independent session host:

```text
HeadlessSessionHost
├── handleCommand(command)
├── subscribe(listener)
└── dispose()

Transports
├── stdio adapter      -> kit --rpc
└── WebSocket adapter  -> kit --mode web
```

Web mode should have normal Kit behavior—persistent sessions, built-in and
external plugins, approvals, models, and tools—but use a remote UI adapter in
place of OpenTUI.

## Initial browser client

Use Mica from the sibling `../mica` project as the component library and design
system. The initial stack should stay HTML-first:

- semantic HTML and native browser behavior
- Mica's CSS-only layout custom elements and native component patterns
- opt-in Mica JavaScript modules only where accessible behavior requires them
- plain TypeScript modules for WebSocket transport, protocol reduction, and
  services
- SolidJS for reactive view composition and DOM lifecycle; no React or virtual
  DOM

The first client is a functional protocol client, not a complete redesign. It
should emulate Kit's TUI hierarchy—session chrome, transcript, tool activity,
composer, status, and dialogs—while keeping the implementation simple. Mica
remains the style and native-component convention layer; Solid does not own
protocol state. Defer a full port and detailed interaction design until the RPC
parity gaps are closed.

The browser bundle is produced during Kit's build and embedded in the compiled
binary. See ADR 0028 for the client layering and asset boundary.

## Required parity work

The current RPC mode is a useful foundation but does not yet expose enough of
Kit for a complete remote client.

### Interactive UI

Replace the inert headless plugin UI with a remote UI implementation. The
server sends correlated `ui_request` events and waits for `ui_response`
commands. Support:

- confirmation
- text input
- selection
- guided questions
- plugin-owned tool approval through an interceptor's nested confirmation

Interactive requests are broadcast to all connected clients. The first valid
response wins, and the server broadcasts the resolution so other clients close
the interaction. Requests are not owned by a connection: keep them pending
across any number of disconnects, replay them to every client that connects,
and reject responses once the originating operation has already resolved,
aborted, or shut down.

Implemented: the transport-neutral broker, `ui_request` / `ui_response`
protocol, connection-independent pending requests, safe abort/shutdown behavior,
remote plugin UI primitives, remote guided-question/user-interaction tools, and
plugin-owned tool approvals composed from interceptors and nested confirmations.
The browser uses deny-by-default focus, bounded scrollable approval content, and
reconnect-safe first-response-wins resolution.

### Plugins

Server mode must initialize the external plugin manager without changing the
reduced plugin surface used by print and stdio RPC modes.

Implemented for web mode: user and project external plugins initialize and
retarget with the hosted runtime. Remote-safe URL opening, chrome contributions,
and user-visible plugin failure reporting remain client-protocol work.

### Commands and sessions

Add protocol operations for:

- capability and protocol-version discovery
- listing, creating, and opening sessions by opaque ID
- changing the active cwd/workspace
- executing slash commands and prompt commands
- retrieving complete state and messages

Implemented: capability discovery, state/messages, session listing and opening,
new sessions, cwd changes, and listing/execution for commands that explicitly
provide transport-neutral handlers. Browser UI for creating, listing, switching,
renaming, and deleting sessions is explicitly deferred; only existing
transport-neutral session commands remain available in web mode. Additional
renderer-owned built-in commands need deliberate remote adapters rather than
fabricated TUI context.

### Attachments

Upload files and images through multipart HTTP. Return an attachment ID that a
subsequent prompt can reference instead of embedding binary data in WebSocket
JSON.

Implemented for web mode: bounded multipart uploads, opaque one-shot prompt
references, explicit deletion, UTF-8 text and validated image conversion, and
WebSocket projection that omits inline image data.

### Reconnection

Assign monotonically increasing sequence numbers to events. A reconnecting
client supplies its last observed sequence. Replay buffered events when
possible; otherwise return a fresh state and message snapshot before resuming
live events.

Implemented: host-instance stream IDs, ordered projected-event journaling,
count/byte-bounded retention, cursor replay through the WebSocket URL, explicit
sync completion, bounded transcript-tail snapshots, paginated/chunked message
and interaction recovery, and snapshot fallback for changed streams or
unavailable history.

Disconnecting a client must not terminate the Kit process or an active agent
run.

### Multi-client control

Allow multiple WebSocket clients to fully control one authoritative runtime:

- correlate command responses within the originating connection
- serialize commands before they mutate shared session state
- broadcast runtime events and shared-state changes to every client
- allow any client to prompt, steer, abort, change session state, or answer an
  interaction
- resolve an interaction with the first valid response and broadcast the
  resolution to the remaining clients
- rely on existing runtime rules when simultaneous actions conflict with an
  active agent run

## Local remote execution

A local machine runs `kit --mode web` and exposes it through a trusted tunnel
or relay such as Tailscale or SSH. The machine must remain awake and online.

The existing TUI and web mode cannot concurrently own the same persisted
session because they create separate runtimes. Initially, continuing remotely
requires detaching from the TUI and resuming the session under `kit --mode web`.
Longer term, make both the local TUI and web UI clients of the same session
host.

## Cloud execution

Run the same `kit --mode web --host 0.0.0.0` command inside an isolated
sandbox. The hosting layer remains responsible for:

- repository cloning or workspace upload
- persistent worktree and session storage
- scoped Git and model credentials
- HTTPS routing and authentication
- sandbox isolation and egress policy
- idle suspension, restoration, resource limits, and cleanup

Prefer one sandbox per workspace and one Kit runtime per active session.

## Remote TUI

After the WebSocket protocol and client state reducer are stable, add a client
entry point such as:

```sh
kit attach https://example.com/session/<id>
```

The remote TUI should consume the same protocol as the browser. This requires
separating the existing TUI's presentation state from its in-process
`AgentRuntime`; it should not require a second server protocol.

## Network exposure

- Bind to `127.0.0.1` by default.
- Defer native authentication and authorization. Initial remote access is
  expected to run through a trusted boundary such as Tailscale or an SSH
  tunnel, which owns its access controls.
- Require allowed-origin WebSocket connections, validate the request host
  against the bound host or an explicit `--allow-host <host:port>` value, and
  support explicit `--allow-origin <origin>` values for trusted reverse
  proxies.
- Treat direct public exposure as unsupported until an authentication layer is
  added.
- Allow native authentication and authorization to be layered on later without
  changing the session protocol.

## Browser-client findings

The first browser client exposed follow-up protocol work that should be solved
for every remote client rather than hidden in renderer-specific code:

- [x] Replace the protocol's legacy Pi-shaped `turn_start` and
  `message_start/update/end` records with projections of Kit's semantic
  `AgentRuntime` events (`agent.turn.*`, `agent.message.*`,
  `user.message.created`, and `session.message.appended`). Pi event names,
  payloads, and types remain private to the core `Agent`; streaming updates use
  a Kit-owned content-update schema.
- [x] Preserve Kit's existing `Turn.id` consistently in snapshot and live
  protocol records, and promote persisted message-entry IDs to stable
  `messageId` values shared by live events, snapshots, references, pagination,
  and legacy-session migration.
- [x] Add a server-owned generation to pending-interaction snapshots, pages,
  and events so clients reject pages invalidated by live mutations.
- [x] Advertise attachment quotas, upload concurrency, page sizes, snapshot
  bounds, and recovery limits through capability discovery.
- [x] Split the browser controller into transport, remote-service, and local
  view-state modules behind the existing controller facade.
- [ ] Expose context-usage statistics so remote headers can render the TUI's
  threshold-colored context progress line.
- [ ] Expose queued follow-up previews, not only their count, so remote pending
  slots can mirror the TUI queue rows.
- [ ] Define explicit shared-session control UX for session/model changes made by
  another connected client; session changes currently force a fresh snapshot.

## Suggested delivery order

- [x] Extract transport-independent RPC dispatch from the stdio server.
- [x] Build a localhost-only web mode with multi-client WebSocket broadcasting.
- [x] Add a minimal Mica-based web transcript/composer client.
- [ ] Fill remaining plugin-client-surface and built-in command gaps.
- [x] Add event sequencing, reconnect, and interaction coordination.
- [ ] Validate local access through a trusted private tunnel.
- [ ] Add a cloud sandbox worker using the same command and protocol.
- [ ] Add `kit attach` for a remote TUI.

The core product abstraction is: `kit --mode web` turns a local or cloud
workspace into a remotely controllable Kit session to which compatible browser
and terminal clients can attach.
