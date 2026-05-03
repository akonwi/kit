# MCP

Kit includes an MCP feature plugin that can discover configured MCP servers and expose them to the agent through a proxy tool.

## Current design

The current MCP integration is **proxy-first**.

Instead of registering every MCP tool directly into the model tool list, Kit exposes a single proxy tool:

- `mcp`

That tool can be used to:

- inspect configured servers
- list tools
- search tools
- describe a tool
- call a tool

This keeps tool prompt size under control when MCP servers expose many tools.

## Config sources

Kit reads and merges MCP configuration from these locations:

1. `~/.config/mcp/mcp.json`
2. `~/.kit/mcp.json`
3. `.mcp.json`
4. `.agents/mcp.json`

Later files override earlier ones by server name.

## Scope

The current MCP feature is focused on:

- tools
- stdio and HTTP transports
- lazy connection
- persistent tool metadata cache
- minimal OAuth handling for auth-required HTTP servers
- MCP status/debug visibility

It does not yet aim to provide full MCP coverage for prompts, resources, or MCP UI integrations.

## Metadata cache

Kit persists discovered MCP tool metadata to a Kit-owned cache file so search, list, and describe can still work after restart without immediately reconnecting every server.

## OAuth

For HTTP MCP servers configured with `auth: "oauth"`, Kit persists OAuth client and token state in a Kit-owned auth file.

When a protected server is actually needed, Kit automatically starts the browser-based authorization flow and continues once the callback completes.

If you want to clear saved MCP OAuth state, use:

- `/mcp-logout <server>`

## Commands and debugging

The MCP plugin may register commands for status, reload, and logout flows.

Run `/debug` to inspect the plugin's current server/config state.
