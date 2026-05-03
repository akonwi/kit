import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { homedir as originalHomedir, tmpdir } from "node:os";
import path from "node:path";
import type { McpMetadataCache } from "./metadata-cache";
import type { McpOAuthStore } from "./oauth-store";

const originalHome = process.env.HOME;
const originalWorkspace = process.env.WORKSPACE_NAME;
const originalToken = process.env.MCP_TEST_TOKEN;
const originalCwd = process.cwd();

let mockedHomeDir = originalHomedir();

mock.module("node:os", () => ({
	homedir: () => mockedHomeDir,
	tmpdir,
}));

const { loadMcpConfig } = await import("./config");
const { loadMcpMetadataCache, saveMcpMetadataCache } = await import(
	"./metadata-cache"
);
const { loadMcpOAuthStore, saveMcpOAuthStore } = await import("./oauth-store");

let tempRoot = "";
let homeDir = "";
let projectDir = "";

async function writeJson(filePath: string, value: unknown): Promise<void> {
	await mkdir(path.dirname(filePath), { recursive: true });
	await writeFile(filePath, JSON.stringify(value, null, 2));
}

describe("MCP config and storage", () => {
	beforeEach(async () => {
		tempRoot = await mkdtemp(path.join(tmpdir(), "kit-mcp-test-"));
		homeDir = path.join(tempRoot, "home");
		projectDir = path.join(tempRoot, "project");
		await mkdir(homeDir, { recursive: true });
		await mkdir(projectDir, { recursive: true });
		process.env.HOME = homeDir;
		mockedHomeDir = homeDir;
		process.env.WORKSPACE_NAME = "workspace";
		process.env.MCP_TEST_TOKEN = "secret-token";
		process.chdir(projectDir);
	});

	afterEach(async () => {
		process.chdir(originalCwd);
		if (originalHome === undefined) delete process.env.HOME;
		else process.env.HOME = originalHome;
		if (originalWorkspace === undefined) delete process.env.WORKSPACE_NAME;
		else process.env.WORKSPACE_NAME = originalWorkspace;
		if (originalToken === undefined) delete process.env.MCP_TEST_TOKEN;
		else process.env.MCP_TEST_TOKEN = originalToken;
		if (tempRoot) await rm(tempRoot, { recursive: true, force: true });
	});

	test("merges MCP config layers with later overrides and expansion", async () => {
		await writeJson(path.join(homeDir, ".config", "mcp", "mcp.json"), {
			mcpServers: {
				shared: {
					description: "shared user",
					command: "base-cmd",
					args: ["--shared"],
					env: { A: "1", KEEP: "shared" },
				},
				httpAuth: {
					url: "https://api.example.com/mcp",
					headers: { "X-Base": "1" },
					auth: "bearer",
					bearerTokenEnv: "MCP_TEST_TOKEN",
				},
			},
		});
		await writeJson(path.join(homeDir, ".kit", "mcp.json"), {
			mcpServers: {
				shared: {
					args: ["--kit"],
					// biome-ignore lint/suspicious/noTemplateCurlyInString: test fixture verifies MCP env expansion syntax
					cwd: "~/work/${WORKSPACE_NAME:-default}",
					env: { B: "2", KEEP: "kit" },
				},
			},
		});
		await writeJson(path.join(projectDir, ".mcp.json"), {
			mcpServers: {
				shared: {
					description: "shared project",
				},
			},
		});
		await writeJson(path.join(projectDir, ".agents", "mcp.json"), {
			mcpServers: {
				shared: {
					command: "final-cmd",
					env: { C: "3" },
				},
				invalid: "nope",
				missingTransport: {
					description: "bad server",
				},
			},
		});

		const result = await loadMcpConfig(projectDir);
		expect(result.files.map((file) => file.loaded)).toEqual([
			true,
			true,
			true,
			true,
		]);
		expect(result.servers.map((server) => server.name)).toEqual([
			"httpAuth",
			"shared",
		]);

		const shared = result.servers.find((server) => server.name === "shared");
		expect(shared).toEqual({
			name: "shared",
			type: "stdio",
			command: "final-cmd",
			args: ["--kit"],
			env: { A: "1", KEEP: "kit", B: "2", C: "3" },
			cwd: path.join(homeDir, "work", "workspace"),
			description: "shared project",
			disabled: false,
			source: "kit-project",
			filePath: path.join(projectDir, ".agents", "mcp.json"),
		});

		const httpAuth = result.servers.find(
			(server) => server.name === "httpAuth",
		);
		expect(httpAuth).toEqual({
			name: "httpAuth",
			type: "http",
			url: "https://api.example.com/mcp",
			headers: {
				"X-Base": "1",
				Authorization: "Bearer secret-token",
			},
			disabled: false,
			auth: { type: "bearer", bearerTokenEnv: "MCP_TEST_TOKEN" },
			source: "shared-user",
			filePath: path.join(homeDir, ".config", "mcp", "mcp.json"),
		});

		expect(result.warnings).toContain(
			`${path.join(projectDir, ".agents", "mcp.json")}: server "invalid" is not an object`,
		);
		expect(result.warnings).toContain(
			`${path.join(projectDir, ".agents", "mcp.json")}: server "missingTransport" is missing a supported transport definition`,
		);
	});

	test("persists and restores metadata cache", async () => {
		const cache: McpMetadataCache = {
			version: 1,
			servers: {
				alpha: {
					fingerprint: "fp-alpha",
					updatedAt: "2025-01-01T00:00:00.000Z",
					tools: [
						{
							serverName: "alpha",
							name: "search",
							canonicalName: "alpha.search",
							description: "Search",
							inputSchema: { type: "object" },
						},
					],
				},
			},
		};

		await saveMcpMetadataCache(cache);
		expect(await loadMcpMetadataCache()).toEqual(cache);

		await writeJson(path.join(homeDir, ".kit", "mcp-cache.json"), {
			version: 1,
			servers: {
				alpha: {
					fingerprint: "fp-alpha",
					updatedAt: "2025-01-01T00:00:00.000Z",
					tools: [{ nope: true }],
				},
			},
		});
		expect(await loadMcpMetadataCache()).toEqual({
			version: 1,
			servers: {
				alpha: {
					fingerprint: "fp-alpha",
					updatedAt: "2025-01-01T00:00:00.000Z",
					tools: [],
				},
			},
		});
	});

	test("persists and restores OAuth store while ignoring invalid discovery state", async () => {
		const store: McpOAuthStore = {
			version: 1,
			servers: {
				secure: {
					clientInformation: { client_id: "kit-client" },
					tokens: {
						access_token: "access",
						token_type: "Bearer",
						refresh_token: "refresh",
					},
					codeVerifier: "verifier",
					discoveryState: {
						authorizationServerUrl: "https://auth.example.com",
					},
				},
			},
		};

		await saveMcpOAuthStore(store);
		expect(await loadMcpOAuthStore()).toEqual(store);

		await writeJson(path.join(homeDir, ".kit", "mcp-auth.json"), {
			version: 1,
			servers: {
				secure: {
					clientInformation: { client_id: "kit-client" },
					discoveryState: { issuer: "missing-required-field" },
				},
			},
		});
		expect(await loadMcpOAuthStore()).toEqual({
			version: 1,
			servers: {
				secure: {
					clientInformation: { client_id: "kit-client" },
				},
			},
		});
	});
});
