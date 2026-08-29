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

## Remaining server and browser work

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
local AppShell/CliRenderer -> semantic WebSocket RPC -> remote RpcSessionHost
```

It must reuse the existing web-mode protocol rather than introduce another
server protocol or stream terminal bytes. This requires separating the TUI's
presentation state from direct ownership of an in-process `AgentRuntime`.

`kit --web-tui` and a possible future OpenTUI SSH entry point are complementary
server-rendered terminal modes:

```text
browser/SSH client -> terminal bytes -> server-side AppShell/CliRenderer
```

They do not provide the local renderer, protocol reduction, shared-session
synchronization, or reconnect behavior required by `kit attach`.

A focused `kit attach` design should settle the CLI and authentication contract,
the local/remote shell-facing interface, synchronization and reconnect behavior,
capability-based feature support, and detach-versus-host-shutdown semantics.
