# 0030: Local daemon and loopback RPC

## Status

Accepted

## Context

Kit's interactive CLI currently owns an `AgentRuntime` directly. `kit --rpc`
and `kit --web` each start another authoritative runtime, so independent Kit
processes can accidentally compete for the same persisted session. A second
terminal tab cannot attach to the runtime already serving the first tab.

ADR 0029 introduced `KitServer` and initially kept same-process clients on a
direct server connection. That avoids a pass-through transport, but it does not
provide a reusable process boundary or let another Kit process discover and
attach to the existing server.

Kit needs one local authority per user that interactive, print, stdio, web, and
future attached clients can share. The local boundary should also exercise the
same session protocol used by remote clients rather than preserve a second
direct-call execution path.

## Decision

Kit will evolve toward a persistent per-user local server. Normal CLI startup
will discover or start this server, then connect to it as a client over
authenticated loopback HTTP and WebSocket RPC.

```text
kit · terminal A ─┐
kit · terminal B ─┼─> 127.0.0.1:<ephemeral> ─> KitServer ─> running sessions
kit -p           ─┤
kit --web        ─┘
```

The first client that cannot reach a compatible server will acquire a startup
lock, launch the detached server process, wait for readiness, and connect. Other
clients racing startup will wait for and reuse the registered server.

### Local discovery and authentication

The server binds only to `127.0.0.1` on an ephemeral port. It writes process
metadata under the user's Kit directory:

```text
~/.kit/run/server.json
~/.kit/run/server.password
~/.kit/run/server.log
```

The registry contains the loopback URL, PID, instance ID, Kit version, protocol
version, and start time. The password is generated from cryptographically secure
random bytes and stored separately with user-only permissions. Every health,
session, attachment, shutdown, and WebSocket request requires authentication.
Host and Origin validation remain active.

Clients validate both version compatibility and instance identity. A stale
registry, dead process, reused port, or incompatible server is not silently
accepted. Startup and registry writes are serialized and atomic.

### Why loopback instead of a Unix-domain socket

Loopback HTTP and WebSocket reuse Kit's existing network protocol, attachment
endpoints, synchronization behavior, browser support, and debugging tools. They
also avoid a separate Windows named-pipe implementation. A Unix-domain socket
would provide a stronger filesystem-only local boundary, but would require a
second transport and framing path without materially improving agent latency.

Loopback is not treated as authentication. Strict binding, a random credential,
file permissions, Host validation, and Origin validation are required. POSIX
systems enforce user-only mode bits; Windows relies on the current user's
protected profile ACL until Kit adds an explicit ACL implementation.

### Session routing

The local server protocol will separate server-scoped operations from immutable
session connections:

```text
server scope
  health · capabilities · list sessions · create session

session scope
  attach <session-id> · synchronize · commands · events · interactions
```

A bound connection cannot replace its session. Session switching is a client
operation that opens another bound connection. Multiple clients may attach to
one running session. Multiple different running sessions remain blocked until
runtime cwd handling and prompt-template caches are session-scoped or each
runtime is isolated in a worker process.

### Lifecycle

Closing a client detaches it; it does not stop the server or authoritative
session. The local server remains alive until explicitly stopped, restarted,
or terminated by a future idle-eviction policy. Kit will expose service
operations for status, start, stop, and restart. Normal startup never steals
ownership from a live PID. If PID reuse leaves an unreachable owner record, Kit
fails closed and requires deliberate manual diagnosis rather than risking a
second authoritative daemon.

Signals and commands must distinguish aborting a shared agent turn, detaching
one client, disposing an ephemeral session, and stopping the server. These
semantics must not be inferred from a socket closing.

### Mode convergence

The target composition is:

```text
kit          -> local loopback server -> session client -> OpenTUI
kit -p       -> local loopback server -> print client
kit --rpc    -> stdio bridge to the local server
kit --web    -> network gateway backed by the local server
kit attach   -> remote WebSocket server
```

The private local listener remains loopback-only. Remote web exposure uses a
separate configured listener and authentication boundary rather than rebinding
the daemon listener.

Migration is incremental. Existing modes may continue to own runtimes until
their client surfaces are migrated, but no new direct-runtime client path will
be introduced.

## Consequences

### Positive

- One authoritative local server owns each running session.
- Additional terminals can attach without starting competing runtimes.
- Local clients continuously exercise the remote protocol and reconnection path.
- Warm daemon state can reduce startup work after the first invocation.
- HTTP, WebSocket, browser, attachment, and cross-platform infrastructure are
  shared.

### Trade-offs

- Normal local Kit operation now depends on daemon discovery, authentication,
  version negotiation, logging, and recovery.
- Client exit no longer implies runtime shutdown.
- Upgrades must handle an already-running older server.
- Loopback listeners require careful access controls despite being local.
- The TUI and other modes must stop owning `AgentRuntime` directly.
- Concurrent sessions require removing process-global runtime state or adding
  worker isolation.

## Deferred

- idle eviction and resource limits;
- automatic compatible-server upgrade policy;
- per-session worker isolation;
- multiplexing multiple session bindings over one WebSocket;
- exposing daemon diagnostics in the TUI.

## Related

- [`0026-headless-rpc-mode.md`](0026-headless-rpc-mode.md)
- [`0027-remote-session-server.md`](0027-remote-session-server.md)
- [`0029-session-client-and-repository-boundaries.md`](0029-session-client-and-repository-boundaries.md)
- [`../../../docs/features/rpc-mode.md`](../../../docs/features/rpc-mode.md)
