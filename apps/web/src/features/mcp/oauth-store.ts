import { existsSync, mkdirSync } from "node:fs";
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import type { OAuthDiscoveryState } from "@modelcontextprotocol/sdk/client/auth.js";
import type {
	OAuthClientInformationMixed,
	OAuthTokens,
} from "@modelcontextprotocol/sdk/shared/auth.js";
import { getKitPaths } from "../../paths";

export type StoredMcpOAuthSession = {
	clientInformation?: OAuthClientInformationMixed;
	tokens?: OAuthTokens;
	codeVerifier?: string;
	discoveryState?: OAuthDiscoveryState;
};

export type McpOAuthStore = {
	version: 1;
	servers: Record<string, StoredMcpOAuthSession>;
};

const EMPTY_STORE: McpOAuthStore = {
	version: 1,
	servers: {},
};

function isRecord(value: unknown): value is Record<string, unknown> {
	return value !== null && typeof value === "object" && !Array.isArray(value);
}

function normalizeSession(raw: unknown): StoredMcpOAuthSession | null {
	if (!isRecord(raw)) return null;
	const out: StoredMcpOAuthSession = {};
	if (isRecord(raw.clientInformation)) {
		out.clientInformation =
			raw.clientInformation as OAuthClientInformationMixed;
	}
	if (isRecord(raw.tokens)) {
		out.tokens = raw.tokens as OAuthTokens;
	}
	if (typeof raw.codeVerifier === "string") {
		out.codeVerifier = raw.codeVerifier;
	}
	if (
		isRecord(raw.discoveryState) &&
		typeof raw.discoveryState.authorizationServerUrl === "string"
	) {
		out.discoveryState = raw.discoveryState as unknown as OAuthDiscoveryState;
	}
	return out;
}

function hasSessionData(session: StoredMcpOAuthSession): boolean {
	return Boolean(
		session.clientInformation ||
			session.tokens ||
			session.codeVerifier ||
			session.discoveryState,
	);
}

export async function loadMcpOAuthStore(): Promise<McpOAuthStore> {
	const { mcpAuthPath } = getKitPaths();
	try {
		const raw = await readFile(mcpAuthPath, "utf8");
		const parsed = JSON.parse(raw) as unknown;
		if (
			!isRecord(parsed) ||
			parsed.version !== 1 ||
			!isRecord(parsed.servers)
		) {
			return EMPTY_STORE;
		}
		const servers: McpOAuthStore["servers"] = {};
		for (const [name, value] of Object.entries(parsed.servers)) {
			const session = normalizeSession(value);
			if (!session || !hasSessionData(session)) continue;
			servers[name] = session;
		}
		return { version: 1, servers };
	} catch {
		return EMPTY_STORE;
	}
}

export async function saveMcpOAuthStore(store: McpOAuthStore): Promise<void> {
	const { mcpAuthPath } = getKitPaths();
	const dir = path.dirname(mcpAuthPath);
	if (!existsSync(dir)) mkdirSync(dir, { recursive: true });
	await writeFile(mcpAuthPath, JSON.stringify(store, null, 2));
}
