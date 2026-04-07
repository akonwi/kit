/**
 * Read API credentials from ~/.pi/agent/auth.json.
 *
 * Temporary compatibility shim while we build our own auth system.
 * pi-ai accepts OAuth access tokens directly alongside regular API keys.
 */

import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { getEnvApiKey } from "@mariozechner/pi-ai";

const AUTH_PATH = join(homedir(), ".kit", "auth.json");

interface AuthEntry {
	type: "api_key" | "oauth";
	key?: string;
	access?: string;
	refresh?: string;
	expires?: number;
}

type AuthFile = Record<string, AuthEntry>;

function readAuthFile(): AuthFile {
	try {
		return JSON.parse(readFileSync(AUTH_PATH, "utf8")) as AuthFile;
	} catch {
		return {};
	}
}

/** Map pi provider names to pi-ai provider identifiers */
const PROVIDER_MAP: Record<string, string> = {
	anthropic: "anthropic",
	openai: "openai",
	google: "google",
};

/** Returns provider IDs that have credentials in auth.json */
export function getAuthenticatedProviderIds(): string[] {
	return Object.keys(readAuthFile());
}

export function getApiKey(provider: string): string | undefined {
	console.log("[auth] getApiKey called for provider:", provider);
	// auth.json takes precedence (OAuth tokens win over env vars)
	const auth = readAuthFile();
	const piProvider = PROVIDER_MAP[provider] ?? provider;
	const entry = auth[piProvider];

	if (entry?.type === "api_key" && entry.key) {
		console.log("[auth] returning api_key from auth.json");
		return entry.key;
	}
	if (entry?.type === "oauth" && entry.access) {
		console.log("[auth] returning oauth token from auth.json");
		return entry.access;
	}

	// Fall back to env var
	const envKey = getEnvApiKey(provider);
	if (envKey) {
		console.log("[auth] returning env var key, prefix:", envKey.slice(0, 10));
		return envKey;
	}

	console.log("[auth] no credential found for", provider);
	return undefined;
}
