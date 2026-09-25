import { randomBytes } from "node:crypto";
import { chmod, mkdir, open, readFile, rm, stat } from "node:fs/promises";
import path from "node:path";
import { getKitPaths } from "../paths";
import { replaceFileAtomically } from "../storage/atomic-file";

export const LOCAL_SERVER_PROTOCOL_VERSION = 1;
export const LOCAL_SERVER_USERNAME = "kit";

export type LocalServerRegistration = {
	pid: number;
	url: string;
	instanceId: string;
	kitVersion: string;
	protocolVersion: number;
	startedAt: string;
};

export type LocalServerPaths = {
	runDir: string;
	registryPath: string;
	passwordPath: string;
	logPath: string;
	operationLockPath: string;
	ownerLockPath: string;
};

export function getLocalServerPaths(
	kitRoot = getKitPaths().kitRoot,
): LocalServerPaths {
	const runDir = path.join(kitRoot, "run");
	return {
		runDir,
		registryPath: path.join(runDir, "server.json"),
		passwordPath: path.join(runDir, "server.password"),
		logPath: path.join(runDir, "server.log"),
		operationLockPath: path.join(runDir, "server.operation"),
		ownerLockPath: path.join(runDir, "server.owner"),
	};
}

export async function ensureLocalServerRunDir(
	paths: LocalServerPaths,
): Promise<void> {
	await mkdir(paths.runDir, { recursive: true, mode: 0o700 });
	await chmod(paths.runDir, 0o700);
}

export async function ensureLocalServerPassword(
	paths: LocalServerPaths,
): Promise<string> {
	await ensureLocalServerRunDir(paths);
	try {
		const handle = await open(paths.passwordPath, "wx", 0o600);
		const password = randomBytes(32).toString("base64url");
		try {
			await handle.writeFile(`${password}\n`, "utf8");
			await handle.sync();
		} finally {
			await handle.close();
		}
		return password;
	} catch (error) {
		if (
			!(error instanceof Error && "code" in error && error.code === "EEXIST")
		) {
			throw error;
		}
	}
	await chmod(paths.passwordPath, 0o600);
	const password = (await readFile(paths.passwordPath, "utf8")).trim();
	if (!/^[A-Za-z0-9_-]{43}$/.test(password)) {
		throw new Error("Local server password file is invalid");
	}
	return password;
}

export async function readLocalServerPassword(
	paths: LocalServerPaths,
): Promise<string | null> {
	try {
		const metadata = await stat(paths.passwordPath);
		if (!metadata.isFile()) return null;
		// Windows relies on the user's protected profile ACL; POSIX clients reject
		// credentials readable by group or other users.
		if (process.platform !== "win32" && (metadata.mode & 0o077) !== 0) {
			return null;
		}
		const password = (await readFile(paths.passwordPath, "utf8")).trim();
		return /^[A-Za-z0-9_-]{43}$/.test(password) ? password : null;
	} catch {
		return null;
	}
}

function parseRegistration(value: unknown): LocalServerRegistration | null {
	if (typeof value !== "object" || value === null || Array.isArray(value)) {
		return null;
	}
	const record = value as Record<string, unknown>;
	if (
		typeof record.pid !== "number" ||
		!Number.isSafeInteger(record.pid) ||
		record.pid < 1 ||
		typeof record.url !== "string" ||
		typeof record.instanceId !== "string" ||
		!record.instanceId ||
		typeof record.kitVersion !== "string" ||
		!record.kitVersion ||
		typeof record.protocolVersion !== "number" ||
		!Number.isSafeInteger(record.protocolVersion) ||
		record.protocolVersion < 1 ||
		typeof record.startedAt !== "string" ||
		!Number.isFinite(Date.parse(record.startedAt))
	) {
		return null;
	}
	try {
		const url = new URL(record.url);
		if (
			url.protocol !== "http:" ||
			url.hostname !== "127.0.0.1" ||
			!url.port ||
			url.pathname !== "/" ||
			url.search ||
			url.hash ||
			url.username ||
			url.password
		) {
			return null;
		}
	} catch {
		return null;
	}
	return record as LocalServerRegistration;
}

export async function readLocalServerRegistration(
	paths: LocalServerPaths,
): Promise<LocalServerRegistration | null> {
	try {
		const content = await readFile(paths.registryPath, "utf8");
		if (Buffer.byteLength(content, "utf8") > 8 * 1024) return null;
		return parseRegistration(JSON.parse(content));
	} catch {
		return null;
	}
}

export async function writeLocalServerRegistration(
	paths: LocalServerPaths,
	registration: LocalServerRegistration,
): Promise<void> {
	await ensureLocalServerRunDir(paths);
	await replaceFileAtomically(
		paths.registryPath,
		`${JSON.stringify(registration, null, 2)}\n`,
		{ mode: 0o600 },
	);
}

export async function removeLocalServerRegistration(
	paths: LocalServerPaths,
	instanceId?: string,
): Promise<void> {
	if (instanceId !== undefined) {
		const current = await readLocalServerRegistration(paths);
		if (current?.instanceId !== instanceId) return;
	}
	await rm(paths.registryPath, { force: true });
}
