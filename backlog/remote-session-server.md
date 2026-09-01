# Remote session follow-ups

## Status

The remote-session foundation is implemented. `kit --rpc` and `kit --web` share
the transport-independent `RpcSessionHost`; web mode provides HTTP and WebSocket
access with optional Basic authentication, semantic protocol events,
snapshot/replay recovery, multiple controlling clients, remote interactions,
attachments, and the Solid
browser client.

Current behavior is documented in [`docs/features/rpc-mode.md`](../docs/features/rpc-mode.md).
The architecture is recorded in:

- [`app/docs/adrs/0026-headless-rpc-mode.md`](../app/docs/adrs/0026-headless-rpc-mode.md)
- [`app/docs/adrs/0027-remote-session-server.md`](../app/docs/adrs/0027-remote-session-server.md)
- [`app/docs/adrs/0028-minimal-web-client.md`](../app/docs/adrs/0028-minimal-web-client.md)
- [`app/docs/adrs/0029-session-client-and-repository-boundaries.md`](../app/docs/adrs/0029-session-client-and-repository-boundaries.md)
- [`app/docs/adrs/0030-local-daemon-and-loopback-rpc.md`](../app/docs/adrs/0030-local-daemon-and-loopback-rpc.md)

The authenticated loopback daemon and session protocol are implemented: Kit can
start, discover, inspect, restart, and stop one per-user `KitServer` process,
then attach a `SessionClient` to an immutable session WebSocket with bounded
snapshot/replay recovery. The composer now consumes a typed session contract
with runtime and `SessionClient` implementations for text, follow-ups, queue
recovery, guarded bash, and lifecycle actions. Transcript presentation,
attachments, slash-command composition, and normal CLI wiring remain outstanding.

## Remaining server and browser work

- Make runtime cwd handling and prompt-template caches session-scoped. Until
  those process-global mutations are removed, `KitServer` deliberately permits
  only one running in-process session at a time.
- Define explicit shared-session UX when another client changes the active
  session or model. Session changes currently force a fresh snapshot.
- Fill deliberate gaps in transport-neutral built-in commands and remote
  management surfaces.
- Validate supported deployment recipes through private tunnels and hosted
  sandboxes.
- Add a web-mode configuration file for bind address, public URL, allowlists,
  and an authentication credential source that avoids plaintext configuration.
- Preserve browser code-review drafts across reloads and add commit/branch,
  file-level, and unchanged-file review workflows.

## Remote TUI

`kit attach` remains outstanding and is tracked in
[`app/backlog/backlog.md`](../app/backlog/backlog.md). It should run a local
OpenTUI renderer as a semantic WebSocket client of an authoritative `kit --web`
host:

```text
local AppShell/CliRenderer -> SessionClient -> WebSocket transport -> KitServer
```

It must evolve the existing web-mode protocol with server-level session discovery
and immutable connection-to-session binding rather than introduce an unrelated
server protocol or stream terminal bytes. This also requires separating the
TUI's presentation state from direct ownership of an in-process `AgentRuntime`.

`kit --web-tui` and a possible future OpenTUI SSH entry point are complementary
server-rendered terminal modes:

```text
browser/SSH client -> terminal bytes -> server-side AppShell/CliRenderer
```

They do not provide the local renderer, protocol reduction, shared-session
synchronization, or reconnect behavior required by `kit attach`.

A focused `kit attach` design should still settle the CLI and authentication
contract, server/session routing syntax, and detach-versus-server-shutdown semantics.
ADR 0029 defines the local/remote client boundary, immutable session binding,
and capability ownership.
