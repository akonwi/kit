import { UnauthorizedError } from "@modelcontextprotocol/sdk/client/auth.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import {
	getDefaultEnvironment,
	StdioClientTransport,
} from "@modelcontextprotocol/sdk/client/stdio.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import type { McpMetadataCache } from "./metadata-cache";
import { getServerFingerprint } from "./metadata-cache";
import { KitMcpOAuthProvider } from "./oauth-provider";
import type { McpOAuthStore, StoredMcpOAuthSession } from "./oauth-store";
import type {
	McpServerDefinition,
	McpServerRuntimeState,
	McpToolMetadata,
} from "./types";

type ManagedTransport = StdioClientTransport | StreamableHTTPClientTransport;

type ManagedConnection = {
	definition: McpServerDefinition;
	client: Client;
	transport: ManagedTransport;
	status: McpServerRuntimeState["status"];
	lastError?: string;
	tools: McpToolMetadata[];
};

type ConnectOptions = {
	onAuthorizationUrl?: (url: URL) => void | Promise<void>;
	getAuthorizationCode?: () => Promise<string>;
};

function toCanonicalToolName(serverName: string, toolName: string): string {
	return `${serverName}.${toolName}`;
}

function isOAuthHttpServer(
	definition: McpServerDefinition,
): definition is Extract<McpServerDefinition, { type: "http" }> {
	return definition.type === "http" && definition.auth?.type === "oauth";
}

export class McpManager {
	private readonly definitions = new Map<string, McpServerDefinition>();
	private readonly connections = new Map<string, ManagedConnection>();
	private readonly cachedTools = new Map<string, McpToolMetadata[]>();
	private readonly oauthSessions = new Map<string, StoredMcpOAuthSession>();
	private readonly pendingAuthorizationUrls = new Map<string, string>();

	constructor(
		definitions: McpServerDefinition[],
		cache: McpMetadataCache,
		oauthStore: McpOAuthStore,
		private readonly onStateChange?: () => void,
	) {
		for (const definition of definitions) {
			this.definitions.set(definition.name, definition);
			const cached = cache.servers[definition.name];
			if (cached && cached.fingerprint === getServerFingerprint(definition)) {
				this.cachedTools.set(definition.name, cached.tools);
			}
			const oauthSession = oauthStore.servers[definition.name];
			if (oauthSession) {
				this.oauthSessions.set(definition.name, oauthSession);
			}
		}
	}

	getDefinitions(): McpServerDefinition[] {
		return [...this.definitions.values()];
	}

	getDefinition(name: string): McpServerDefinition | undefined {
		return this.definitions.get(name);
	}

	getPendingAuthorizationUrl(name: string): string | undefined {
		return this.pendingAuthorizationUrls.get(name);
	}

	hasOAuthSession(name: string): boolean {
		return this.oauthSessions.has(name);
	}

	async clearOAuthSession(name: string): Promise<void> {
		if (!this.definitions.has(name)) {
			throw new Error(`Unknown MCP server: ${name}`);
		}
		await this.closeExistingConnection(name);
		this.connections.delete(name);
		this.oauthSessions.delete(name);
		this.pendingAuthorizationUrls.delete(name);
		this.onStateChange?.();
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

	private createTransport(
		definition: McpServerDefinition,
		options?: ConnectOptions,
	): ManagedTransport {
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

		const authProvider = isOAuthHttpServer(definition)
			? new KitMcpOAuthProvider(
					definition.name,
					() => this.oauthSessions.get(definition.name),
					(session) => this.saveOAuthSession(definition.name, session),
					async (url) => {
						this.pendingAuthorizationUrls.set(definition.name, url.toString());
						this.onStateChange?.();
						await options?.onAuthorizationUrl?.(url);
					},
				)
			: undefined;

		return new StreamableHTTPClientTransport(new URL(definition.url), {
			requestInit: {
				headers: definition.headers,
			},
			...(authProvider ? { authProvider } : {}),
		});
	}

	private async saveOAuthSession(
		serverName: string,
		session: StoredMcpOAuthSession | undefined,
	): Promise<void> {
		if (!session || Object.keys(session).length === 0) {
			this.oauthSessions.delete(serverName);
		} else {
			this.oauthSessions.set(serverName, session);
		}
		this.onStateChange?.();
	}

	private async closeExistingConnection(
		name: string,
	): Promise<ManagedConnection | null> {
		const existing = this.connections.get(name);
		if (!existing) return null;
		try {
			await existing.transport.close();
		} catch {
			// best effort
		}
		return existing;
	}

	private createPendingConnection(
		definition: McpServerDefinition,
		transport: ManagedTransport,
		existing: ManagedConnection | null,
	): ManagedConnection {
		const pending: ManagedConnection = {
			definition,
			client: new Client({ name: "kit-mcp", version: "0.1.0" }),
			transport,
			status: "connecting",
			tools: existing?.tools ?? [],
			lastError: undefined,
		};
		this.connections.set(definition.name, pending);
		this.onStateChange?.();
		return pending;
	}

	private getStateOrThrow(name: string): McpServerRuntimeState {
		return this.getRuntimeStates().find(
			(state) => state.name === name,
		) as McpServerRuntimeState;
	}

	private toConnectionError(serverName: string, error: unknown): string {
		if (error instanceof UnauthorizedError) {
			return `Authorization required. Run /mcp-login ${serverName}.`;
		}
		return error instanceof Error ? error.message : String(error);
	}

	private async failConnection(
		name: string,
		connection: ManagedConnection,
		error: unknown,
	): Promise<never> {
		connection.status = "error";
		connection.lastError = this.toConnectionError(name, error);
		try {
			await connection.transport.close();
		} catch {
			// best effort
		}
		this.onStateChange?.();
		throw error;
	}

	async connectServer(
		name: string,
		options?: ConnectOptions,
	): Promise<McpServerRuntimeState> {
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
		if (existing?.status === "connected" && !options?.getAuthorizationCode) {
			return this.getStateOrThrow(name);
		}
		const closed = await this.closeExistingConnection(name);
		const transport = this.createTransport(definition, options);
		const pending = this.createPendingConnection(definition, transport, closed);
		this.pendingAuthorizationUrls.delete(name);

		try {
			await pending.client.connect(transport);
			pending.status = "connected";
			pending.lastError = undefined;
			this.pendingAuthorizationUrls.delete(name);
			this.onStateChange?.();
			return this.getStateOrThrow(name);
		} catch (error) {
			if (
				error instanceof UnauthorizedError &&
				transport instanceof StreamableHTTPClientTransport &&
				options?.getAuthorizationCode
			) {
				try {
					const authorizationCode = await options.getAuthorizationCode();
					await transport.finishAuth(authorizationCode);
					await pending.client.connect(transport);
					pending.status = "connected";
					pending.lastError = undefined;
					this.pendingAuthorizationUrls.delete(name);
					this.onStateChange?.();
					return this.getStateOrThrow(name);
				} catch (authError) {
					return this.failConnection(name, pending, authError);
				}
			}
			return this.failConnection(name, pending, error);
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

	getPersistentOAuthStore(): McpOAuthStore {
		const servers: McpOAuthStore["servers"] = {};
		for (const definition of this.getDefinitions()) {
			const session = this.oauthSessions.get(definition.name);
			if (!session || Object.keys(session).length === 0) continue;
			servers[definition.name] = session;
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
		try {
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
		} catch (error) {
			connection.status = "error";
			connection.lastError = this.toConnectionError(name, error);
			this.onStateChange?.();
			throw error;
		}
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
		try {
			return connection.client.callTool({
				name: toolName,
				arguments: args ?? {},
			});
		} catch (error) {
			connection.status = "error";
			connection.lastError = this.toConnectionError(serverName, error);
			this.onStateChange?.();
			throw error;
		}
	}
}
