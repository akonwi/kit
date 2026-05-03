import { beforeEach, describe, expect, mock, test } from "bun:test";
import { getServerFingerprint } from "./metadata-cache";
import type { McpServerDefinition, McpToolMetadata } from "./types";

type MockBehavior = {
	requireAuth?: boolean;
	authorizationUrl?: string;
	connectAttempts: number;
	tools?: Array<{
		name: string;
		description?: string;
		inputSchema?: Record<string, unknown>;
	}>;
	callResult?: unknown;
	calls: Array<{ name: string; arguments: Record<string, unknown> }>;
};

const behaviors = new Map<string, MockBehavior>();

function keyForDefinition(definition: McpServerDefinition): string {
	return definition.type === "http"
		? definition.url
		: `stdio:${definition.command}:${definition.args.join(" ")}`;
}

class MockUnauthorizedError extends Error {}

class MockStdioClientTransport {
	readonly key: string;
	constructor(
		readonly options: { command: string; args: string[]; close?: () => void },
	) {
		this.key = `stdio:${options.command}:${options.args.join(" ")}`;
	}
	async close(): Promise<void> {}
}

class MockStreamableHTTPClientTransport {
	readonly key: string;
	readonly authProvider?: {
		tokens: () =>
			| Promise<{ access_token?: string } | undefined>
			| { access_token?: string }
			| undefined;
		redirectToAuthorization: (url: URL) => Promise<void> | void;
		saveTokens: (tokens: {
			access_token: string;
			token_type: string;
		}) => Promise<void> | void;
	};
	constructor(
		readonly url: URL,
		readonly options?: {
			authProvider?: MockStreamableHTTPClientTransport["authProvider"];
		},
	) {
		this.key = url.toString();
		this.authProvider = options?.authProvider;
	}
	async finishAuth(code: string): Promise<void> {
		await this.authProvider?.saveTokens({
			access_token: `token-${code}`,
			token_type: "Bearer",
		});
	}
	async close(): Promise<void> {}
}

class MockClient {
	private behavior: MockBehavior | null = null;

	async connect(
		transport: MockStdioClientTransport | MockStreamableHTTPClientTransport,
	): Promise<void> {
		const behavior = behaviors.get(transport.key);
		if (!behavior) throw new Error(`No mock behavior for ${transport.key}`);
		behavior.connectAttempts += 1;
		if (
			behavior.requireAuth &&
			transport instanceof MockStreamableHTTPClientTransport
		) {
			const tokens = await transport.authProvider?.tokens?.();
			if (!tokens?.access_token) {
				await transport.authProvider?.redirectToAuthorization(
					new URL(
						behavior.authorizationUrl ?? "https://auth.example.com/authorize",
					),
				);
				throw new MockUnauthorizedError("Unauthorized");
			}
		}
		this.behavior = behavior;
	}

	async listTools(): Promise<{ tools: MockBehavior["tools"] }> {
		return { tools: this.behavior?.tools ?? [] };
	}

	async callTool(input: {
		name: string;
		arguments: Record<string, unknown>;
	}): Promise<unknown> {
		if (!this.behavior) throw new Error("Client not connected");
		this.behavior.calls.push(input);
		return this.behavior.callResult ?? { content: [] };
	}
}

mock.module("@modelcontextprotocol/sdk/client/auth.js", () => ({
	UnauthorizedError: MockUnauthorizedError,
}));

mock.module("@modelcontextprotocol/sdk/client/index.js", () => ({
	Client: MockClient,
}));

mock.module("@modelcontextprotocol/sdk/client/stdio.js", () => ({
	StdioClientTransport: MockStdioClientTransport,
	getDefaultEnvironment: () => ({ PATH: "mock-path" }),
}));

mock.module("@modelcontextprotocol/sdk/client/streamableHttp.js", () => ({
	StreamableHTTPClientTransport: MockStreamableHTTPClientTransport,
}));

const { McpManager } = await import("./manager");

function createEmptyCache() {
	return { version: 1 as const, servers: {} };
}

function createEmptyOAuthStore() {
	return { version: 1 as const, servers: {} };
}

describe("McpManager", () => {
	beforeEach(() => {
		behaviors.clear();
	});

	test("hydrates matching cached tool metadata into runtime state", () => {
		const definition: McpServerDefinition = {
			name: "cached",
			type: "http",
			url: "https://cached.example.com/mcp",
			headers: {},
			description: "Cached server",
			disabled: false,
			source: "kit-project",
			filePath: ".agents/mcp.json",
		};
		const cachedTools: McpToolMetadata[] = [
			{
				serverName: "cached",
				name: "search",
				canonicalName: "cached.search",
				description: "Search",
			},
		];
		const manager = new McpManager(
			[definition],
			{
				version: 1,
				servers: {
					cached: {
						fingerprint: getServerFingerprint(definition),
						tools: cachedTools,
						updatedAt: "2025-01-01T00:00:00.000Z",
					},
				},
			},
			createEmptyOAuthStore(),
		);

		expect(manager.getKnownTools("cached")).toEqual(cachedTools);
		expect(manager.getRuntimeStates()).toEqual([
			{
				name: "cached",
				status: "configured",
				type: "http",
				description: "Cached server",
				source: "kit-project",
				filePath: ".agents/mcp.json",
				toolCount: 1,
				lastError: undefined,
				disabled: false,
				cached: true,
			},
		]);
	});

	test("auto-connects a stdio server when calling a tool", async () => {
		const definition: McpServerDefinition = {
			name: "local",
			type: "stdio",
			command: "echo-server",
			args: ["--stdio"],
			env: {},
			description: "Local server",
			disabled: false,
			source: "kit-project",
			filePath: ".agents/mcp.json",
		};
		behaviors.set(keyForDefinition(definition), {
			connectAttempts: 0,
			calls: [],
			callResult: { content: [{ type: "text", text: "pong" }] },
		});
		const manager = new McpManager(
			[definition],
			createEmptyCache(),
			createEmptyOAuthStore(),
		);

		const result = await manager.callTool("local", "ping", { value: 1 });

		expect(result).toEqual({ content: [{ type: "text", text: "pong" }] });
		expect(behaviors.get(keyForDefinition(definition))).toMatchObject({
			connectAttempts: 1,
			calls: [{ name: "ping", arguments: { value: 1 } }],
		});
	});

	test("automatically authorizes an OAuth HTTP server when tools are needed", async () => {
		const definition: McpServerDefinition = {
			name: "secure",
			type: "http",
			url: "https://secure.example.com/mcp",
			headers: {},
			description: "Secure server",
			disabled: false,
			auth: { type: "oauth" },
			source: "kit-project",
			filePath: ".agents/mcp.json",
		};
		behaviors.set(keyForDefinition(definition), {
			requireAuth: true,
			authorizationUrl: "https://auth.example.com/authorize",
			connectAttempts: 0,
			calls: [],
			tools: [
				{
					name: "search",
					description: "Search",
					inputSchema: { type: "object" },
				},
			],
		});
		const authRequests: Array<{ serverName: string; url: string }> = [];
		const manager = new McpManager(
			[definition],
			createEmptyCache(),
			createEmptyOAuthStore(),
			{
				authorizeOAuthServer: async (serverName, authorizationUrl) => {
					authRequests.push({
						serverName,
						url: authorizationUrl.toString(),
					});
					return "code-123";
				},
			},
		);

		const tools = await manager.ensureTools("secure");

		expect(tools).toEqual([
			{
				serverName: "secure",
				name: "search",
				canonicalName: "secure.search",
				description: "Search",
				inputSchema: { type: "object" },
			},
		]);
		expect(authRequests).toEqual([
			{
				serverName: "secure",
				url: "https://auth.example.com/authorize",
			},
		]);
		expect(behaviors.get(keyForDefinition(definition))?.connectAttempts).toBe(
			2,
		);
		expect(manager.hasOAuthSession("secure")).toBe(true);
		expect(manager.getPendingAuthorizationUrl("secure")).toBeUndefined();
		expect(
			manager.getPersistentOAuthStore().servers.secure?.tokens,
		).toMatchObject({
			access_token: "token-code-123",
			token_type: "Bearer",
		});
	});
});
