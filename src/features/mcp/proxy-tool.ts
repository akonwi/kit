import type { AgentToolResult } from "@mariozechner/pi-agent-core";
import { type Static, Type } from "@sinclair/typebox";
import type { McpManager } from "./manager";
import type { McpToolMetadata } from "./types";

const parameters = Type.Object({
	server: Type.Optional(
		Type.String({
			description: "Server name to inspect or constrain operations to",
		}),
	),
	search: Type.Optional(
		Type.String({ description: "Search MCP tools by name or description" }),
	),
	describe: Type.Optional(
		Type.String({
			description: "Show the schema and details for one MCP tool",
		}),
	),
	connect: Type.Optional(
		Type.String({ description: "Connect to a specific MCP server" }),
	),
	tool: Type.Optional(
		Type.String({
			description: "Call one MCP tool by canonical name like server.tool",
		}),
	),
	args: Type.Optional(
		Type.String({
			description: "JSON object string containing tool arguments",
		}),
	),
});

type ProxyInput = Static<typeof parameters>;

type TextBlock = { type: "text"; text: string };

type ImageBlock = { type: "image"; data: string; mimeType: string };

function textResult(
	text: string,
	details: Record<string, unknown> = {},
): AgentToolResult<Record<string, unknown>> {
	return {
		content: [{ type: "text", text }],
		details,
	};
}

function formatSchema(schema: Record<string, unknown> | undefined): string {
	if (!schema) return "No input schema.";
	return JSON.stringify(schema, null, 2);
}

function searchTools(
	query: string,
	tools: McpToolMetadata[],
): McpToolMetadata[] {
	const pattern = query.trim().toLowerCase();
	if (!pattern) return [];
	return tools.filter((tool) => {
		return (
			tool.canonicalName.toLowerCase().includes(pattern) ||
			tool.description.toLowerCase().includes(pattern)
		);
	});
}

function renderStatus(manager: McpManager): string {
	const states = manager.getRuntimeStates();
	if (states.length === 0) return "No MCP servers configured.";
	const lines = [
		`MCP servers: ${states.length}`,
		"",
		...states.map((state) => {
			const status =
				state.status === "connected"
					? "✓"
					: state.status === "connecting"
						? "◌"
						: state.status === "error"
							? "✗"
							: state.status === "disabled"
								? "⊘"
								: "○";
			const tail = state.lastError ? ` · ${state.lastError}` : "";
			const count = state.toolCount > 0 ? ` · ${state.toolCount} tools` : "";
			const cache = state.cached ? " · cached" : "";
			return `${status} ${state.name} (${state.type})${count}${cache}${tail}`;
		}),
	];
	return lines.join("\n");
}

function resolveCanonicalName(tool: McpToolMetadata): string {
	return `${tool.serverName}.${tool.name}`;
}

function parseJsonArgs(raw: string | undefined): Record<string, unknown> {
	if (!raw?.trim()) return {};
	const parsed = JSON.parse(raw) as unknown;
	if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
		throw new Error("MCP tool args must be a JSON object string.");
	}
	return parsed as Record<string, unknown>;
}

function transformContent(content: unknown): Array<TextBlock | ImageBlock> {
	if (!Array.isArray(content))
		return [{ type: "text", text: String(content ?? "") }];
	const blocks: Array<TextBlock | ImageBlock> = [];
	for (const block of content) {
		if (!block || typeof block !== "object") continue;
		const value = block as Record<string, unknown>;
		if (value.type === "text" && typeof value.text === "string") {
			blocks.push({ type: "text", text: value.text });
			continue;
		}
		if (
			value.type === "image" &&
			typeof value.data === "string" &&
			typeof value.mimeType === "string"
		) {
			blocks.push({
				type: "image",
				data: value.data,
				mimeType: value.mimeType,
			});
			continue;
		}
		if (value.type === "resource") {
			const resource = value.resource as Record<string, unknown> | undefined;
			const uri = typeof resource?.uri === "string" ? resource.uri : "(no uri)";
			const text =
				typeof resource?.text === "string"
					? resource.text
					: JSON.stringify(resource ?? {}, null, 2);
			blocks.push({ type: "text", text: `[Resource: ${uri}]\n${text}` });
			continue;
		}
		blocks.push({ type: "text", text: JSON.stringify(value, null, 2) });
	}
	return blocks.length > 0
		? blocks
		: [{ type: "text", text: "(empty result)" }];
}

function findTool(
	name: string,
	tools: McpToolMetadata[],
	serverName?: string,
): { tool: McpToolMetadata | null; error?: string } {
	const canonicalMatch = tools.find((tool) => tool.canonicalName === name);
	if (canonicalMatch) return { tool: canonicalMatch };
	const scoped = serverName
		? tools.filter((tool) => tool.serverName === serverName)
		: tools;
	const exact = scoped.filter((tool) => tool.name === name);
	if (exact.length === 1) return { tool: exact[0] };
	if (exact.length > 1) {
		return {
			tool: null,
			error: `Tool name "${name}" exists on multiple servers. Use a canonical name like server.tool.`,
		};
	}
	return { tool: null, error: `Tool "${name}" was not found.` };
}

export function MCP_PROXY_POLICY(): string {
	return [
		"Use the mcp tool to discover and call external MCP tools when built-in tools are insufficient.",
		"Prefer mcp search or describe before calling an unfamiliar MCP tool.",
		"When calling an MCP tool, pass args as a JSON object string.",
	].join("\n");
}

export function createMcpProxyTool(manager: McpManager) {
	return {
		name: "mcp",
		label: "MCP",
		description:
			"Discover configured MCP servers, inspect their tools, connect lazily, and call MCP tools through one proxy interface.",
		promptGuidelines: [
			"Use search or describe before calling an unfamiliar MCP tool.",
			"Use canonical tool names like server.tool when there may be name collisions.",
			"Pass args as a JSON object string.",
		],
		parameters,
		async execute(
			_toolCallId: string,
			input: ProxyInput,
		): Promise<AgentToolResult<Record<string, unknown>>> {
			try {
				if (input.tool) {
					let allTools = manager.getKnownTools();
					let resolved = findTool(input.tool, allTools, input.server);
					if (!resolved.tool) {
						const canonicalServer = input.tool.includes(".")
							? input.tool.slice(0, input.tool.indexOf("."))
							: undefined;
						const targetServer = input.server ?? canonicalServer;
						if (targetServer && manager.getDefinition(targetServer)) {
							await manager.ensureTools(targetServer).catch(() => undefined);
							allTools = manager.getKnownTools();
							resolved = findTool(input.tool, allTools, input.server);
						} else {
							allTools = await manager.ensureAllTools();
							resolved = findTool(input.tool, allTools, input.server);
						}
					}
					if (!resolved.tool) {
						return textResult(resolved.error ?? "Tool not found.", {
							mode: "call",
							error: "tool_not_found",
						});
					}
					const args = parseJsonArgs(input.args);
					const result = (await manager.callTool(
						resolved.tool.serverName,
						resolved.tool.name,
						args,
					)) as { content?: unknown; isError?: boolean };
					const content = transformContent(result.content ?? []);
					return {
						content,
						details: {
							mode: "call",
							tool: resolveCanonicalName(resolved.tool),
							isError: result.isError === true,
						},
					};
				}

				if (input.connect) {
					const state = await manager.connectServer(input.connect);
					const tools = await manager
						.ensureTools(input.connect)
						.catch(() => []);
					return textResult(
						`${state.name} is ${state.status}.${tools.length > 0 ? ` Loaded ${tools.length} tools.` : ""}`,
						{ mode: "connect", server: state.name, toolCount: tools.length },
					);
				}

				if (input.describe) {
					let allTools = manager.getKnownTools();
					let resolved = findTool(input.describe, allTools, input.server);
					if (!resolved.tool && input.server) {
						await manager.ensureTools(input.server).catch(() => undefined);
						allTools = manager.getKnownTools();
						resolved = findTool(input.describe, allTools, input.server);
					}
					if (!resolved.tool) {
						return textResult(resolved.error ?? "Tool not found.", {
							mode: "describe",
							error: "tool_not_found",
						});
					}
					return textResult(
						[
							resolved.tool.canonicalName,
							resolved.tool.description || "(no description)",
							"",
							"Input schema:",
							formatSchema(resolved.tool.inputSchema),
						].join("\n"),
						{ mode: "describe", tool: resolved.tool.canonicalName },
					);
				}

				if (input.search) {
					const allTools = manager.getKnownTools();
					const matches = searchTools(
						input.search,
						input.server
							? allTools.filter((tool) => tool.serverName === input.server)
							: allTools,
					);
					if (matches.length === 0) {
						return textResult(`No MCP tools matched "${input.search}".`, {
							mode: "search",
							count: 0,
						});
					}
					return textResult(
						[
							`Found ${matches.length} MCP tool${matches.length === 1 ? "" : "s"}:`,
							"",
							...matches.map((tool) =>
								tool.description
									? `- ${tool.canonicalName} — ${tool.description}`
									: `- ${tool.canonicalName}`,
							),
						].join("\n"),
						{ mode: "search", count: matches.length },
					);
				}

				if (input.server) {
					let tools = manager.getKnownTools(input.server);
					if (tools.length === 0) {
						tools = await manager.ensureTools(input.server).catch(() => []);
					}
					const state = manager
						.getRuntimeStates()
						.find((next) => next.name === input.server);
					if (!state) {
						return textResult(`Unknown MCP server: ${input.server}`, {
							mode: "list",
							error: "server_not_found",
						});
					}
					if (tools.length === 0) {
						return textResult(`${state.name} has no tools.`, {
							mode: "list",
							server: state.name,
							count: 0,
						});
					}
					return textResult(
						[
							`${state.name} (${tools.length} tools${state.cached ? ", cached" : ""})`,
							"",
							...tools.map((tool) =>
								tool.description
									? `- ${tool.name} — ${tool.description}`
									: `- ${tool.name}`,
							),
						].join("\n"),
						{ mode: "list", server: state.name, count: tools.length },
					);
				}

				return textResult(renderStatus(manager), { mode: "status" });
			} catch (error) {
				return textResult(
					error instanceof Error ? error.message : String(error),
					{ error: "execution_failed" },
				);
			}
		},
	};
}
