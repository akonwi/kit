# MCP

Kit includes an MCP feature plugin that can discover configured MCP servers and expose them to the agent through a proxy tool.

## Current design

The current MCP integration is **proxy-first**.

Instead of registering every remote tool directly into the model tool list, Kit exposes one namespace proxy per configured server, such as `mcp_github`.

A namespace proxy can:

- list tools
- search tools
- describe a tool
- call a tool
- clear managed OAuth credentials

This keeps tool prompt size under control when MCP servers expose many tools.

## Config sources

Kit reads and merges MCP configuration from these locations:

1. `~/.kit-v2/mcp.json` (or `$KIT_HOME/mcp.json`)
2. `.mcp.json`
3. `.agents/mcp.json`

Later files override earlier ones by server name.

## Scope

The current MCP feature is focused on:

- tools
- stdio and HTTP transports
- lazy connection
- automatic OAuth handling for auth-required HTTP servers
- retained MCP status workspace pane and lightweight debug UI

It does not yet aim to provide full MCP coverage for prompts, resources, or broader MCP management UI.

## OAuth

For HTTP MCP servers configured with `auth: "oauth"`, Kit persists OAuth client and token state in a Kit-owned auth file.

When a protected server is actually needed, Kit automatically starts the browser-based authorization flow and continues once the callback completes.

Current behavior includes:

- automatic browser-based auth on first protected use
- a 1 minute authorization timeout
- error toasts when auth fails
- automatic clear-and-reauthorize retry once when saved auth has expired or is rejected

To clear saved MCP OAuth state, ask Kit to use the `logout` action on that server's MCP namespace. The active connection is closed and the next use authorizes again.

## Status and debugging

The MCP plugin currently provides:

Canonical MCP status and debugging surfaces are tracked separately for the terminal and browser clients.
