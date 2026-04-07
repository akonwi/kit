/**
 * Kit credential storage — ~/.kit/auth.json
 */

import { readFileSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { getEnvApiKey } from "@mariozechner/pi-ai";

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

/** Get API key for a provider — auth.json takes priority over env vars. */
export function getApiKey(provider: string): string | undefined {
	const auth = readAuthFileSync();
	const entry = auth[provider];

	if (entry?.type === "api_key" && entry.key) return entry.key;
	if (entry?.type === "oauth" && entry.access) return entry.access;

	return getEnvApiKey(provider) ?? undefined;
}
