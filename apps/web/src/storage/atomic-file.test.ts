import { afterEach, describe, expect, test } from "bun:test";
import { mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { replaceFileAtomically, withFileLock } from "./atomic-file";

let tempDir = "";

afterEach(async () => {
	if (tempDir) await rm(tempDir, { recursive: true, force: true });
});

describe("atomic file storage", () => {
	test("replaces files and cleans temporary state", async () => {
		tempDir = await mkdtemp(path.join(tmpdir(), "kit-atomic-file-"));
		const filePath = path.join(tempDir, "session.jsonl");
		await writeFile(filePath, "old\n", "utf8");

		await replaceFileAtomically(filePath, "new\n");

		expect(await readFile(filePath, "utf8")).toBe("new\n");
		expect(await readdir(tempDir)).toEqual(["session.jsonl"]);
	});

	test("serializes access and removes the lock file", async () => {
		tempDir = await mkdtemp(path.join(tmpdir(), "kit-file-lock-"));
		const filePath = path.join(tempDir, "session.jsonl");
		const events: string[] = [];
		let releaseFirst: () => void = () => {};
		let firstAcquired: () => void = () => {};
		const acquired = new Promise<void>((resolve) => {
			firstAcquired = resolve;
		});
		const release = new Promise<void>((resolve) => {
			releaseFirst = resolve;
		});
		const first = withFileLock(filePath, async () => {
			events.push("first:start");
			firstAcquired();
			await release;
			events.push("first:end");
		});
		await acquired;
		const second = withFileLock(filePath, async () => {
			events.push("second");
		});
		releaseFirst();

		await Promise.all([first, second]);

		expect(events).toEqual(["first:start", "first:end", "second"]);
		expect(await readdir(tempDir)).toEqual([]);
	});
});
