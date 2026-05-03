import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import {
	getDefaultEnvironment,
	StdioClientTransport,
} from "@modelcontextprotocol/sdk/client/stdio.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import type { McpMetadataCache } from "./metadata-cache";
import { getServerFingerprint } from "./metadata-cache";
import type {
	McpServerDefinition,
	McpServerRuntimeState,
	McpToolMetadata,
} from "./types";

type ManagedConnection = {
	definition: McpServerDefinition;
	client: Client;
	transport: { close: () => Promise<void> | void };
	status: McpServerRuntimeState["status"];
	lastError?: string;
	tools: McpToolMetadata[];
};

function toCanonicalToolName(serverName: string, toolName: string): string {
	return `${serverName}.${toolName}`;
}

export class McpManager {
	private readonly definitions = new Map<string, McpServerDefinition>();
	private readonly connections = new Map<string, ManagedConnection>();
	private readonly cachedTools = new Map<string, McpToolMetadata[]>();

	constructor(
		definitions: McpServerDefinition[],
		cache: McpMetadataCache,
		private readonly onStateChange?: () => void,
	) {
		for (const definition of definitions) {
			this.definitions.set(definition.name, definition);
			const cached = cache.servers[definition.name];
			if (!cached) continue;
			if (cached.fingerprint !== getServerFingerprint(definition)) continue;
			this.cachedTools.set(definition.name, cached.tools);
		}
	}

	getDefinitions(): McpServerDefinition[] {
		return [...this.definitions.values()];
	}

	getDefinition(name: string): McpServerDefinition | undefined {
		return this.definitions.get(name);
	}

	getRuntimeStates(): McpServerRuntimeState[] {
		return this.getDefinitions().map((definition) => {
			const existing = this.connections.get(definition.name);
			const cachedTools = this.cachedTools.get(definition.name) ?? [];
			return {
				name: definition.name,
				status: definition.disabled
					? "disabled"
					: (existing?.status ?? "configured"),
				type: definition.type,
				description: definition.description,
				source: definition.source,
				filePath: definition.filePath,
				toolCount: existing?.tools.length ?? cachedTools.length,
				lastError: existing?.lastError,
				disabled: definition.disabled,
				cached: !existing && cachedTools.length > 0,
			};
		});
	}

	async dispose(): Promise<void> {
		for (const connection of this.connections.values()) {
			try {
				await connection.transport.close();
			} catch {
				// best effort
			}
		}
		this.connections.clear();
		this.onStateChange?.();
	}

	private createTransport(definition: McpServerDefinition) {
		if (definition.type === "stdio") {
			return new StdioClientTransport({
				command: definition.command,
				args: definition.args,
				env: {
					...getDefaultEnvironment(),
					...definition.env,
				},
				...(definition.cwd ? { cwd: definition.cwd } : {}),
				stderr: "pipe",
			});
		}
		return new StreamableHTTPClientTransport(new URL(definition.url), {
			requestInit: {
				headers: definition.headers,
			},
		});
	}

	async connectServer(name: string): Promise<McpServerRuntimeState> {
		const definition = this.definitions.get(name);
		if (!definition) throw new Error(`Unknown MCP server: ${name}`);
		if (definition.disabled) {
			return {
				name: definition.name,
				status: "disabled",
				type: definition.type,
				description: definition.description,
				source: definition.source,
				filePath: definition.filePath,
				toolCount: 0,
				disabled: true,
				cached: false,
			};
		}

		const existing = this.connections.get(name);
		if (existing?.status === "connected") {
			return this.getRuntimeStates().find(
				(state) => state.name === name,
			) as McpServerRuntimeState;
		}
		if (existing) {
			try {
				await existing.transport.close();
			} catch {
				// best effort
			}
		}

		const client = new Client({ name: "kit-mcp", version: "0.1.0" });
		const transport = this.createTransport(definition);
		const pending: ManagedConnection = {
			definition,
			client,
			transport,
			status: "connecting",
			tools: existing?.tools ?? [],
			lastError: undefined,
		};
		this.connections.set(name, pending);
		this.onStateChange?.();

		try {
			await client.connect(transport);
			pending.status = "connected";
			pending.lastError = undefined;
			this.onStateChange?.();
			return this.getRuntimeStates().find(
				(state) => state.name === name,
			) as McpServerRuntimeState;
		} catch (error) {
			pending.status = "error";
			pending.lastError =
				error instanceof Error ? error.message : String(error);
			try {
				await transport.close();
			} catch {
				// best effort
			}
			this.onStateChange?.();
			throw error;
		}
	}

	getPersistentCache(): McpMetadataCache {
		const servers: McpMetadataCache["servers"] = {};
		for (const definition of this.getDefinitions()) {
			const tools = this.cachedTools.get(definition.name) ?? [];
			if (tools.length === 0) continue;
			servers[definition.name] = {
				fingerprint: getServerFingerprint(definition),
				tools,
				updatedAt: new Date().toISOString(),
			};
		}
		return { version: 1, servers };
	}

	async ensureTools(name: string): Promise<McpToolMetadata[]> {
		const definition = this.definitions.get(name);
		if (!definition) throw new Error(`Unknown MCP server: ${name}`);
		if (definition.disabled) return [];
		await this.connectServer(name);
		const connection = this.connections.get(name);
		if (!connection || connection.status !== "connected") {
			throw new Error(`MCP server ${name} is not connected.`);
		}
		const result = await connection.client.listTools();
		connection.tools = (result.tools ?? []).map((tool) => ({
			serverName: name,
			name: tool.name,
			canonicalName: toCanonicalToolName(name, tool.name),
			description: tool.description ?? "",
			inputSchema:
				tool.inputSchema && typeof tool.inputSchema === "object"
					? (tool.inputSchema as Record<string, unknown>)
					: undefined,
		}));
		this.cachedTools.set(name, connection.tools);
		this.onStateChange?.();
		return connection.tools;
	}

	async ensureAllTools(): Promise<McpToolMetadata[]> {
		const all: McpToolMetadata[] = [];
		for (const definition of this.getDefinitions()) {
			if (definition.disabled) continue;
			try {
				all.push(...(await this.ensureTools(definition.name)));
			} catch {
				// leave error state on the server, but keep searching others
			}
		}
		return all;
	}

	getKnownTools(serverName?: string): McpToolMetadata[] {
		if (serverName) {
			const live = this.connections.get(serverName)?.tools;
			if (live && live.length > 0) return live;
			return this.cachedTools.get(serverName) ?? [];
		}
		const allNames = new Set<string>([
			...this.cachedTools.keys(),
			...this.connections.keys(),
		]);
		const all: McpToolMetadata[] = [];
		for (const name of allNames) {
			all.push(...this.getKnownTools(name));
		}
		return all;
	}

	async callTool(
		serverName: string,
		toolName: string,
		args?: Record<string, unknown>,
	): Promise<unknown> {
		await this.connectServer(serverName);
		const connection = this.connections.get(serverName);
		if (!connection || connection.status !== "connected") {
			throw new Error(`MCP server ${serverName} is not connected.`);
		}
		return connection.client.callTool({
			name: toolName,
			arguments: args ?? {},
		});
	}
}
