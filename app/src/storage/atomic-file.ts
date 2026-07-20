import { randomUUID } from "node:crypto";
import { open, rename, rm } from "node:fs/promises";
import path from "node:path";
import { lock } from "proper-lockfile";

export async function withFileLock<T>(
	filePath: string,
	operation: () => Promise<T>,
): Promise<T> {
	const release = await lock(filePath, {
		realpath: false,
		stale: 30_000,
		update: 10_000,
		retries: {
			retries: 100,
			factor: 1.2,
			minTimeout: 20,
			maxTimeout: 500,
		},
	});
	try {
		return await operation();
	} finally {
		await release();
	}
}

export async function replaceFileAtomically(
	filePath: string,
	content: string,
): Promise<void> {
	const temporaryPath = `${filePath}.${randomUUID()}.tmp`;
	try {
		const handle = await open(temporaryPath, "w");
		try {
			await handle.writeFile(content, "utf8");
			await handle.sync();
		} finally {
			await handle.close();
		}
		await rename(temporaryPath, filePath);
		const directory = await open(path.dirname(filePath), "r").catch(() => null);
		if (directory) {
			await directory.sync().catch(() => {});
			await directory.close().catch(() => {});
		}
	} finally {
		await rm(temporaryPath, { force: true }).catch(() => {});
	}
}
