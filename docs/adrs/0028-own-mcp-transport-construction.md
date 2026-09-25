# 0028: Own MCP transport construction outside the agent core

## Status

Accepted

## Context

Kit reaches Model Context Protocol servers through the progressively disclosed
namespace tools in `internal/droids/mcp`. A namespace connects lazily, and
`mcp.Server.Transport` is a `TransportFactory` that the embedding application
supplies:

```go
// TransportFactory creates a fresh MCP transport. MCP transports are
// single-use, so the factory may be called again after a failed connection.
type TransportFactory func(context.Context) (sdkmcp.Transport, error)
```

Connecting a configured server is not protocol work. A stdio server is a
supervised child process; an HTTP server is an authenticated client with a
credential and redirect policy. Both are governed by Kit's trust model, not by
the MCP specification, and `github.com/modelcontextprotocol/go-sdk` already
provides the protocol-generic `CommandTransport` and
`StreamableClientTransport`.

`internal/droids` is a reviewed source seed with a deliberately narrow
dependency surface, and ADR 0004 draws the boundary as: a droid owns when and
why a capability is used, while adapters own its physical implementation.

## Decision

`internal/droids/mcp` owns protocol-shaped concerns only: the namespace tool
surface, lazy connection lifetime, tool metadata caching, result conversion, and
bounds. `TransportFactory` is its sole transport contract, and it remains
testable entirely over in-memory transports.

Kit owns transport construction in a server-side package that projects a
validated `mcpconfig.Server` into a configured `droids/mcp.Server`, including
its factory. Because transports are single-use, each factory invocation mints a
fresh transport and, for stdio, a fresh process.

### Kit-owned transport policy

Stdio transports are supervised child processes. Kit gives each server its own
process group and tears down the group, so a launcher that forks a long-lived
child does not leak servers past session shutdown. Kit retains a bounded stderr
tail for diagnostics and resolves the working directory from explicit session
cwd or configuration rather than process-global cwd.

A stdio server inherits Kit's process environment, overlaid with its configured
`env`. This matches how Kit launches plugin processes, keeps servers that depend
on ambient tooling configuration working without per-server declarations, and
avoids an allowlist that silently breaks servers as their dependencies change.

HTTP transports use a Kit-owned `http.Client`. Configured headers and bearer
credentials are injected by a Kit round tripper, environment-sourced tokens
resolve at connect time rather than being retained in configuration snapshots,
and credential-bearing headers are not forwarded across a cross-origin redirect.

Credential acquisition and persistence stay with `internal/auth` and reach the
transport through the SDK's `OAuthHandler` seam.

## Consequences

Positive:

- the agent core keeps a narrow dependency surface, so reconciling the seed with
  its upstream stays a reviewable migration;
- process supervision, credential handling, and configuration policy live with
  the Kit packages that already own them;
- transport policy is testable against real processes and `httptest` servers
  without involving the agent loop;
- adding a future transport changes one Kit package.

Trade-offs:

- Kit, not the agent core, must correctly implement process-group teardown and
  redirect policy;
- an embedder of droids that is not Kit has to supply its own transports;
- an inherited environment reaches every configured stdio server, including one
  declared by a project-local `.mcp.json` or `.agents/mcp.json` that arrived
  with a cloned repository. Kit's provider credentials are readable from that
  environment, so bounding this exposure is a question of which servers Kit
  agrees to run, not of how a transport is built.

## Scope boundaries

`internal/mcpconfig` resolves the Kit user `mcp.json`, `.mcp.json`, and
`.agents/mcp.json` with deterministic precedence. Kit-owned transport
construction includes OAuth acquisition and private credential persistence;
protocol-facing namespace behavior remains in `internal/droids/mcp`. Canonical
status projection is a separate server/client protocol concern.

## Related

- [0002: Internalize the droids agent core during the rewrite](./0002-internalize-agent-core.md)
- [0004: Model a droid as an autonomous agent runtime](./0004-droids-agent-runtime-boundary.md)
- [0026: Scope plugin processes and route plugin UI](./0026-scope-plugin-processes-and-route-plugin-ui.md)
