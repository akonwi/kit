import { randomUUID } from "node:crypto";
import {
	closeSync,
	existsSync,
	fsyncSync,
	mkdirSync,
	openSync,
	readFileSync,
	renameSync,
	rmSync,
	statSync,
	writeFileSync,
} from "node:fs";
import path from "node:path";
import { SESSIONS_DIR } from "../../session";
import { scratchpadPath } from "../../storage/session-sidecars";

export { scratchpadPath };

const LOCK_STALE_MS = 30_000;

export type ScratchpadMutationResult = {
	updated: boolean;
	content: string;
};

type LockOwner = {
	pid: number;
	token: string;
};

function readFile(filePath: string): string {
	if (!existsSync(filePath)) return "";
	return readFileSync(filePath, "utf8");
}

function replaceFile(filePath: string, content: string): void {
	const temporaryPath = `${filePath}.${randomUUID()}.tmp`;
	let descriptor: number | undefined;
	try {
		descriptor = openSync(temporaryPath, "w");
		writeFileSync(descriptor, content, "utf8");
		fsyncSync(descriptor);
		closeSync(descriptor);
		descriptor = undefined;
		renameSync(temporaryPath, filePath);
	} finally {
		if (descriptor !== undefined) closeSync(descriptor);
		rmSync(temporaryPath, { force: true });
	}
}

function processIsRunning(pid: number): boolean {
	try {
		process.kill(pid, 0);
		return true;
	} catch (error) {
		return (error as NodeJS.ErrnoException).code === "EPERM";
	}
}

function readLockOwner(lockPath: string): LockOwner | null {
	try {
		const parsed = JSON.parse(
			readFileSync(path.join(lockPath, "owner.json"), "utf8"),
		) as Partial<LockOwner>;
		return typeof parsed.pid === "number" && typeof parsed.token === "string"
			? { pid: parsed.pid, token: parsed.token }
			: null;
	} catch {
		return null;
	}
}

function removeAbandonedLock(lockPath: string): boolean {
	const owner = readLockOwner(lockPath);
	if (owner && processIsRunning(owner.pid)) return false;
	if (!owner) {
		try {
			if (Date.now() - statSync(lockPath).mtimeMs <= LOCK_STALE_MS)
				return false;
		} catch {
			return true;
		}
	}
	const abandonedPath = `${lockPath}.abandoned.${randomUUID()}`;
	try {
		renameSync(lockPath, abandonedPath);
		rmSync(abandonedPath, { recursive: true, force: true });
		return true;
	} catch {
		return false;
	}
}

function withScratchpadLock<T>(filePath: string, operation: () => T): T {
	const lockPath = `${filePath}.lock`;
	const owner: LockOwner = { pid: process.pid, token: randomUUID() };
	for (let attempt = 0; attempt < 2; attempt++) {
		try {
			mkdirSync(lockPath);
			try {
				writeFileSync(
					path.join(lockPath, "owner.json"),
					JSON.stringify(owner),
					"utf8",
				);
			} catch (error) {
				rmSync(lockPath, { recursive: true, force: true });
				throw error;
			}
		} catch (error) {
			if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
			if (!removeAbandonedLock(lockPath)) {
				throw new Error("Scratchpad is being updated by another Kit process.");
			}
			continue;
		}

		try {
			return operation();
		} finally {
			if (readLockOwner(lockPath)?.token === owner.token) {
				rmSync(lockPath, { recursive: true, force: true });
			}
		}
	}
	throw new Error("Could not acquire the scratchpad lock.");
}

export function readScratchpadFile(sessionId: string): string {
	return readFileSync(scratchpadPath(sessionId), "utf8");
}

export function readScratchpad(sessionId: string): string {
	try {
		return readScratchpadFile(sessionId);
	} catch {
		return "";
	}
}

export function mutateScratchpadFile(
	filePath: string,
	update: (current: string) => string | null,
): ScratchpadMutationResult {
	return withScratchpadLock(filePath, () => {
		const fileExists = existsSync(filePath);
		const current = readFile(filePath);
		const next = update(current);
		if (next === null || (next === current && fileExists)) {
			return { updated: false, content: current };
		}
		replaceFile(filePath, next);
		return { updated: next !== current, content: next };
	});
}

export function mutateScratchpad(
	sessionId: string,
	update: (current: string) => string | null,
): ScratchpadMutationResult {
	mkdirSync(SESSIONS_DIR, { recursive: true });
	return mutateScratchpadFile(scratchpadPath(sessionId), update);
}

export function ensureScratchpadFile(filePath: string): void {
	if (existsSync(filePath)) return;
	try {
		mutateScratchpadFile(filePath, (current) => current);
	} catch (error) {
		if (existsSync(filePath)) return;
		if (
			error instanceof Error &&
			error.message === "Scratchpad is being updated by another Kit process."
		) {
			return;
		}
		throw error;
	}
}

export function ensureScratchpad(sessionId: string): void {
	mkdirSync(SESSIONS_DIR, { recursive: true });
	ensureScratchpadFile(scratchpadPath(sessionId));
}

export function writeScratchpad(sessionId: string, content: string): void {
	mutateScratchpad(sessionId, () => content);
}
