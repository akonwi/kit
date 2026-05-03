import { readFile } from "node:fs/promises";
import { homedir } from "node:os";
import path from "node:path";
import { getKitPaths } from "../../paths";
import type {
	LoadMcpConfigResult,
	McpConfigSource,
	McpServerAuth,
	McpServerDefinition,
} from "./types";

type RawMcpConfig = {
	mcpServers?: Record<string, RawMcpServerConfig>;
};

type RawMcpServerConfig = {
	description?: unknown;
	disabled?: unknown;
	command?: unknown;
	args?: unknown;
	env?: unknown;
	cwd?: unknown;
	url?: unknown;
	baseUrl?: unknown;
	headers?: unknown;
	auth?: unknown;
	bearerToken?: unknown;
	bearerTokenEnv?: unknown;
};

type SourceFile = { source: McpConfigSource; filePath: string };

function isRecord(value: unknown): value is Record<string, unknown> {
	return value !== null && typeof value === "object" && !Array.isArray(value);
}

function expandString(value: string): string {
	const expandedEnv = value
		.replace(/\$env:([A-Z0-9_]+)/gi, (_match, key: string) => {
			return process.env[key] ?? "";
		})
		.replace(
			/\$\{([A-Z0-9_]+)(:-([^}]*))?\}/gi,
			(
				_match,
				key: string,
				_fallbackExpr: string | undefined,
				fallback: string | undefined,
			) => {
				return process.env[key] ?? fallback ?? "";
			},
		);
	if (expandedEnv === "~") return homedir();
	if (expandedEnv.startsWith("~/")) {
		return path.join(homedir(), expandedEnv.slice(2));
	}
	return expandedEnv;
}

function normalizeStringRecord(
	value: unknown,
): Record<string, string> | undefined {
	if (!isRecord(value)) return undefined;
	const out: Record<string, string> = {};
	for (const [key, raw] of Object.entries(value)) {
		if (typeof raw !== "string") continue;
		out[key] = expandString(raw);
	}
	return out;
}

function normalizeAuth(raw: RawMcpServerConfig): McpServerAuth | undefined {
	const authType = raw.auth;
	const bearerToken =
		typeof raw.bearerToken === "string"
			? expandString(raw.bearerToken)
			: undefined;
	const bearerTokenEnv =
		typeof raw.bearerTokenEnv === "string" ? raw.bearerTokenEnv : undefined;
	if (authType === "oauth") return { type: "oauth" };
	if (authType === "bearer" || bearerToken || bearerTokenEnv) {
		return {
			type: "bearer",
			...(bearerToken ? { bearerToken } : {}),
			...(bearerTokenEnv ? { bearerTokenEnv } : {}),
		};
	}
	return undefined;
}

function mergeServerConfig(
	base: RawMcpServerConfig | undefined,
	override: RawMcpServerConfig,
): RawMcpServerConfig {
	return {
		...(base ?? {}),
		...override,
		env: {
			...(isRecord(base?.env) ? base.env : {}),
			...(isRecord(override.env) ? override.env : {}),
		},
		headers: {
			...(isRecord(base?.headers) ? base.headers : {}),
			...(isRecord(override.headers) ? override.headers : {}),
		},
	};
}

async function readConfigFile(filePath: string): Promise<RawMcpConfig | null> {
	try {
		const raw = await readFile(filePath, "utf8");
		const parsed = JSON.parse(raw) as unknown;
		if (!isRecord(parsed)) return null;
		return parsed as RawMcpConfig;
	} catch {
		return null;
	}
}

function normalizeServer(
	name: string,
	raw: RawMcpServerConfig,
	source: McpConfigSource,
	filePath: string,
	warnings: string[],
): McpServerDefinition | null {
	const description =
		typeof raw.description === "string" ? raw.description : undefined;
	const disabled = raw.disabled === true;
	const auth = normalizeAuth(raw);

	if (typeof raw.command === "string") {
		const env = normalizeStringRecord(raw.env) ?? {};
		const command = expandString(raw.command);
		const args = Array.isArray(raw.args)
			? raw.args
					.filter((value): value is string => typeof value === "string")
					.map(expandString)
			: [];
		const cwd = typeof raw.cwd === "string" ? expandString(raw.cwd) : undefined;
		return {
			name,
			type: "stdio",
			command,
			args,
			env,
			...(cwd ? { cwd } : {}),
			...(description ? { description } : {}),
			disabled,
			...(auth ? { auth } : {}),
			source,
			filePath,
		};
	}

	const rawUrl =
		typeof raw.url === "string"
			? raw.url
			: typeof raw.baseUrl === "string"
				? raw.baseUrl
				: undefined;
	if (rawUrl) {
		const headers = normalizeStringRecord(raw.headers) ?? {};
		const url = expandString(rawUrl);
		if (auth?.type === "bearer") {
			const token =
				auth.bearerToken ??
				(auth.bearerTokenEnv ? process.env[auth.bearerTokenEnv] : undefined);
			if (token && !headers.Authorization) {
				headers.Authorization = `Bearer ${token}`;
			}
		}
		return {
			name,
			type: "http",
			url,
			headers,
			...(description ? { description } : {}),
			disabled,
			...(auth ? { auth } : {}),
			source,
			filePath,
		};
	}

	warnings.push(
		`${filePath}: server "${name}" is missing a supported transport definition`,
	);
	return null;
}

export async function loadMcpConfig(cwd: string): Promise<LoadMcpConfigResult> {
	const kitPaths = getKitPaths();
	const warnings: string[] = [];
	const files: SourceFile[] = [
		{
			source: "shared-user",
			filePath: path.join(homedir(), ".config", "mcp", "mcp.json"),
		},
		{ source: "kit-user", filePath: path.join(kitPaths.kitRoot, "mcp.json") },
		{ source: "shared-project", filePath: path.join(cwd, ".mcp.json") },
		{ source: "kit-project", filePath: path.join(cwd, ".agents", "mcp.json") },
	];

	const merged = new Map<
		string,
		{ raw: RawMcpServerConfig; source: McpConfigSource; filePath: string }
	>();
	const loadedFiles: Array<{
		source: McpConfigSource;
		filePath: string;
		loaded: boolean;
	}> = [];

	for (const file of files) {
		const config = await readConfigFile(file.filePath);
		loadedFiles.push({ ...file, loaded: config !== null });
		if (!config?.mcpServers || !isRecord(config.mcpServers)) continue;
		for (const [name, value] of Object.entries(config.mcpServers)) {
			if (!isRecord(value)) {
				warnings.push(`${file.filePath}: server "${name}" is not an object`);
				continue;
			}
			const prev = merged.get(name);
			merged.set(name, {
				raw: mergeServerConfig(prev?.raw, value as RawMcpServerConfig),
				source: file.source,
				filePath: file.filePath,
			});
		}
	}

	const servers: McpServerDefinition[] = [];
	for (const [name, entry] of merged.entries()) {
		const server = normalizeServer(
			name,
			entry.raw,
			entry.source,
			entry.filePath,
			warnings,
		);
		if (server) servers.push(server);
	}

	servers.sort((a, b) => a.name.localeCompare(b.name));
	return { servers, warnings, files: loadedFiles };
}
