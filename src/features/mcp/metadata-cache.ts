import { existsSync, mkdirSync } from "node:fs";
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { getKitPaths } from "../../paths";
import type { McpServerDefinition, McpToolMetadata } from "./types";

export type McpMetadataCache = {
	version: 1;
	servers: Record<
		string,
		{
			fingerprint: string;
			tools: McpToolMetadata[];
			updatedAt: string;
		}
	>;
};

const EMPTY_CACHE: McpMetadataCache = {
	version: 1,
	servers: {},
};

function sortRecord(record: Record<string, string>): Array<[string, string]> {
	return Object.entries(record).sort(([a], [b]) => a.localeCompare(b));
}

export function getServerFingerprint(definition: McpServerDefinition): string {
	if (definition.type === "stdio") {
		return JSON.stringify({
			type: definition.type,
			command: definition.command,
			args: definition.args,
			env: sortRecord(definition.env),
			cwd: definition.cwd ?? null,
			auth: definition.auth ?? null,
			disabled: definition.disabled,
		});
	}
	return JSON.stringify({
		type: definition.type,
		url: definition.url,
		headers: sortRecord(definition.headers),
		auth: definition.auth ?? null,
		disabled: definition.disabled,
	});
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return value !== null && typeof value === "object" && !Array.isArray(value);
}

function normalizeToolMetadata(raw: unknown): McpToolMetadata | null {
	if (!isRecord(raw)) return null;
	if (
		typeof raw.serverName !== "string" ||
		typeof raw.name !== "string" ||
		typeof raw.canonicalName !== "string" ||
		typeof raw.description !== "string"
	) {
		return null;
	}
	const inputSchema =
		isRecord(raw.inputSchema) || Array.isArray(raw.inputSchema)
			? (raw.inputSchema as Record<string, unknown>)
			: undefined;
	return {
		serverName: raw.serverName,
		name: raw.name,
		canonicalName: raw.canonicalName,
		description: raw.description,
		...(inputSchema ? { inputSchema } : {}),
	};
}

export async function loadMcpMetadataCache(): Promise<McpMetadataCache> {
	const { mcpCachePath } = getKitPaths();
	try {
		const raw = await readFile(mcpCachePath, "utf8");
		const parsed = JSON.parse(raw) as unknown;
		if (
			!isRecord(parsed) ||
			parsed.version !== 1 ||
			!isRecord(parsed.servers)
		) {
			return EMPTY_CACHE;
		}
		const servers: McpMetadataCache["servers"] = {};
		for (const [name, value] of Object.entries(parsed.servers)) {
			if (!isRecord(value) || typeof value.fingerprint !== "string") continue;
			const tools = Array.isArray(value.tools)
				? value.tools
						.map(normalizeToolMetadata)
						.filter((tool): tool is McpToolMetadata => tool !== null)
				: [];
			servers[name] = {
				fingerprint: value.fingerprint,
				tools,
				updatedAt:
					typeof value.updatedAt === "string"
						? value.updatedAt
						: new Date(0).toISOString(),
			};
		}
		return { version: 1, servers };
	} catch {
		return EMPTY_CACHE;
	}
}

export async function saveMcpMetadataCache(
	cache: McpMetadataCache,
): Promise<void> {
	const { mcpCachePath } = getKitPaths();
	const dir = path.dirname(mcpCachePath);
	if (!existsSync(dir)) mkdirSync(dir, { recursive: true });
	await writeFile(mcpCachePath, JSON.stringify(cache, null, 2));
}
