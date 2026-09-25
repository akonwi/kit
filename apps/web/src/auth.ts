/**
 * Kit credential storage — ~/.kit/auth.json.
 *
 * Pi owns provider-specific login, refresh, and request-auth resolution. Kit
 * supplies the persistent credential store used by that runtime.
 */

import { readFileSync } from "node:fs";
import { mkdir, readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import type {
	Credential,
	CredentialInfo,
	CredentialStore,
} from "@earendil-works/pi-ai";
import { replaceFileAtomically, withFileLock } from "./storage/atomic-file";

export const AUTH_PATH = join(homedir(), ".kit", "auth.json");

export type AuthEntry = Credential;
export type AuthFile = Record<string, AuthEntry>;

export function readAuthFileSync(): AuthFile {
	try {
		return JSON.parse(readFileSync(AUTH_PATH, "utf8")) as AuthFile;
	} catch {
		return {};
	}
}

async function readAuthFileAt(authPath: string): Promise<AuthFile> {
	try {
		return JSON.parse(await readFile(authPath, "utf8")) as AuthFile;
	} catch (error) {
		if ((error as NodeJS.ErrnoException).code === "ENOENT") return {};
		throw error;
	}
}

async function writeAuthFileAt(
	authPath: string,
	data: AuthFile,
): Promise<void> {
	await mkdir(dirname(authPath), { recursive: true });
	await replaceFileAtomically(authPath, JSON.stringify(data, null, 2), {
		mode: 0o600,
	});
}

export function readAuthFile(): Promise<AuthFile> {
	return readAuthFileAt(AUTH_PATH);
}

export async function writeAuthFile(data: AuthFile): Promise<void> {
	await mkdir(dirname(AUTH_PATH), { recursive: true });
	await withFileLock(AUTH_PATH, () => writeAuthFileAt(AUTH_PATH, data));
}

/** Provider IDs that have credentials in auth.json. */
export function getAuthenticatedProviderIds(): string[] {
	return Object.keys(readAuthFileSync());
}

/**
 * Persistent Pi credential store backed by Kit's existing auth file. Since
 * every provider shares one JSON document, all writes are serialized to avoid
 * cross-provider lost updates within the running Kit process.
 */
export class KitCredentialStore implements CredentialStore {
	private writeChain: Promise<unknown> = Promise.resolve();

	constructor(private readonly authPath = AUTH_PATH) {}

	private enqueue<T>(task: () => Promise<T>): Promise<T> {
		const next = this.writeChain.catch(() => {}).then(task);
		this.writeChain = next.catch(() => {});
		return next;
	}

	async read(providerId: string): Promise<Credential | undefined> {
		return (await readAuthFileAt(this.authPath))[providerId];
	}

	async list(): Promise<readonly CredentialInfo[]> {
		const auth = await readAuthFileAt(this.authPath);
		return Object.entries(auth).map(([providerId, credential]) => ({
			providerId,
			type: credential.type,
		}));
	}

	modify(
		providerId: string,
		fn: (current: Credential | undefined) => Promise<Credential | undefined>,
	): Promise<Credential | undefined> {
		return this.enqueue(async () => {
			await mkdir(dirname(this.authPath), { recursive: true });
			return withFileLock(this.authPath, async () => {
				const auth = await readAuthFileAt(this.authPath);
				const current = auth[providerId];
				const next = await fn(current);
				if (next === undefined) return current;
				auth[providerId] = next;
				await writeAuthFileAt(this.authPath, auth);
				return next;
			});
		});
	}

	delete(providerId: string): Promise<void> {
		return this.enqueue(async () => {
			await mkdir(dirname(this.authPath), { recursive: true });
			await withFileLock(this.authPath, async () => {
				const auth = await readAuthFileAt(this.authPath);
				if (!(providerId in auth)) return;
				delete auth[providerId];
				await writeAuthFileAt(this.authPath, auth);
			});
		});
	}
}

export const kitCredentialStore = new KitCredentialStore();
