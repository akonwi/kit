/**
 * Kit credential storage — ~/.kit/auth.json
 *
 * OAuth tokens are refreshed proactively before each API call
 * via getApiKey(), which checks expiry and calls the provider's
 * refreshToken() if needed.
 */

import { readFileSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { getEnvApiKey } from "@mariozechner/pi-ai";
import {
	getOAuthApiKey,
	type OAuthCredentials,
} from "@mariozechner/pi-ai/oauth";

export const AUTH_PATH = join(homedir(), ".kit", "auth.json");

export interface AuthEntry {
	type: "api_key" | "oauth";
	key?: string;
	access?: string;
	refresh?: string;
	expires?: number;
}

export type AuthFile = Record<string, AuthEntry>;

export function readAuthFileSync(): AuthFile {
	try {
		return JSON.parse(readFileSync(AUTH_PATH, "utf8")) as AuthFile;
	} catch {
		return {};
	}
}

export async function readAuthFile(): Promise<AuthFile> {
	try {
		return JSON.parse(await readFile(AUTH_PATH, "utf8")) as AuthFile;
	} catch {
		return {};
	}
}

export async function writeAuthFile(data: AuthFile): Promise<void> {
	await mkdir(dirname(AUTH_PATH), { recursive: true });
	await writeFile(AUTH_PATH, JSON.stringify(data, null, 2), "utf8");
}

/** Provider IDs that have credentials in auth.json. */
export function getAuthenticatedProviderIds(): string[] {
	return Object.keys(readAuthFileSync());
}

/**
 * Get API key for a provider.
 *
 * For OAuth providers, this checks token expiry and refreshes
 * proactively before the token expires. Updated credentials are
 * persisted to auth.json automatically.
 *
 * Priority: auth.json (api_key or oauth) > environment variable.
 */
export async function getApiKey(provider: string): Promise<string | undefined> {
	const auth = await readAuthFile();
	const entry = auth[provider];

	if (entry?.type === "api_key" && entry.key) return entry.key;

	if (entry?.type === "oauth" && entry.access && entry.refresh) {
		try {
			const result = await getOAuthApiKey(provider, {
				[provider]: entry as unknown as OAuthCredentials,
			});
			if (result) {
				// Persist refreshed credentials if they changed
				if (result.newCredentials.access !== entry.access) {
					auth[provider] = {
						...entry,
						...result.newCredentials,
						type: "oauth",
					};
					await writeAuthFile(auth);
				}
				return result.apiKey;
			}
		} catch (err) {
			// Refresh failed — return stale token as fallback.
			// The API call will likely fail too, but the error will
			// surface through normal agent error handling.
			console.warn(`[auth] OAuth refresh failed for ${provider}:`, err);
			return entry.access;
		}
	}

	return getEnvApiKey(provider) ?? undefined;
}
