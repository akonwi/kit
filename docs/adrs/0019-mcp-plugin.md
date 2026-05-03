# 0019: Add MCP support as a native `McpPlugin`

## Status
Accepted

## Context

Kit wants MCP support, but the current codebase has no MCP integration.

The repo already has the right extension points for a clean implementation:

- plugins can dynamically register tools
- plugins can dynamically register commands
- plugins can append system prompt policy
- the app/plugin layer owns toast UI
- `AgentRuntime` can stay focused on agent/session/tool orchestration

We also want to avoid a naive MCP integration that registers every MCP tool directly into the model prompt, because large MCP tool catalogs can create major prompt bloat.

## Decision

Kit will implement MCP support as a native plugin:

- `src/features/mcp/`
- `McpPlugin`

The initial design is **proxy-first**.

### Initial scope

The MVP supports:

- MCP **tools only**
- shared MCP config files
- Kit-specific MCP override files
- stdio transport
- HTTP transport
- lazy connection and persistent metadata caching
- minimal OAuth handling for auth-required HTTP servers
- one proxy tool named `mcp`
- status/reload/connect/login/logout commands
- `/debug` visibility

The MVP does **not** support:

- MCP prompts
- MCP resources as first-class features
- MCP UI integrations
- direct-tool registration by default
- rich onboarding or management UI

## Config model

The first pass reads and merges these config layers, in order:

1. `~/.config/mcp/mcp.json`
2. `~/.kit/mcp.json`
3. `.mcp.json`
4. `.agents/mcp.json`

Later layers override earlier ones by server name.

Shared MCP files provide the interoperable baseline.
Kit override files provide Kit-specific behavior and local adjustments.

## Tool model

The default MCP exposure is a single proxy tool:

- `mcp`

The proxy tool supports:

- status
- list server tools
- search tools
- describe tool schema
- connect to a server
- call a tool

This keeps prompt cost bounded even when the configured MCP ecosystem is large.

## Runtime boundary

`AgentRuntime` should not become MCP-aware.

The `McpPlugin` owns:

- MCP config discovery
- connection lifecycle
- tool metadata caching
- proxy tool behavior
- MCP slash commands
- status/debug reporting

## Transports

The first pass should support both transports when practical:

- stdio
- HTTP

OAuth should remain minimal in the first implementation and follow the same product philosophy as current provider login flows: enough to work, not a full auth platform.

## Consequences

### Positive

- MCP support fits the current plugin architecture cleanly
- runtime remains slim
- prompt bloat is controlled with a proxy-first design
- future direct-tool support can be layered on without redesigning the feature
- shared MCP config files improve interoperability with the wider MCP ecosystem

### Trade-offs

- the proxy tool is less convenient than direct tools for some workflows
- metadata caching and lifecycle logic add complexity to the plugin
- OAuth handling remains intentionally minimal rather than a full auth management UI

## Follow-up direction

Likely next phases after MVP:

1. selective direct tools
2. richer MCP status UI
3. resource/prompt support if still desirable

## Related

- `docs/adrs/0015-plugin-system.md`
- `docs/adrs/0018-runtime-notification-decoupling.md`
