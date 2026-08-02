import { afterEach, describe, expect, test } from "bun:test";
import {
	mkdirSync,
	mkdtempSync,
	readdirSync,
	readFileSync,
	rmSync,
	writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { mutateScratchpadFile } from "./storage";

const temporaryDirectories: string[] = [];

function temporaryFile(): string {
	const directory = mkdtempSync(path.join(tmpdir(), "kit-scratchpad-storage-"));
	temporaryDirectories.push(directory);
	return path.join(directory, "scratchpad.md");
}

afterEach(() => {
	for (const directory of temporaryDirectories.splice(0)) {
		rmSync(directory, { recursive: true, force: true });
	}
});

describe("scratchpad storage", () => {
	test("atomically mutates persisted content and cleans temporary state", () => {
		const filePath = temporaryFile();
		writeFileSync(filePath, "Existing", "utf8");

		const result = mutateScratchpadFile(filePath, (current) => {
			return `${current}\n\nAdded`;
		});

		expect(result).toEqual({
			updated: true,
			content: "Existing\n\nAdded",
		});
		expect(readFileSync(filePath, "utf8")).toBe("Existing\n\nAdded");
		expect(readdirSync(path.dirname(filePath))).toEqual(["scratchpad.md"]);
	});

	test("fails immediately instead of blocking when a live owner holds the lock", () => {
		const filePath = temporaryFile();
		const lockPath = `${filePath}.lock`;
		mkdirSync(lockPath);
		writeFileSync(
			path.join(lockPath, "owner.json"),
			JSON.stringify({ pid: process.pid, token: "other-owner" }),
			"utf8",
		);
		const startedAt = performance.now();

		expect(() => mutateScratchpadFile(filePath, () => "updated")).toThrow(
			"Scratchpad is being updated by another Kit process.",
		);
		expect(performance.now() - startedAt).toBeLessThan(100);
	});

	test("recovers an abandoned lock and cleans it after mutation", () => {
		const filePath = temporaryFile();
		const lockPath = `${filePath}.lock`;
		mkdirSync(lockPath);
		writeFileSync(
			path.join(lockPath, "owner.json"),
			JSON.stringify({ pid: 2_147_483_647, token: "dead-owner" }),
			"utf8",
		);

		expect(mutateScratchpadFile(filePath, () => "recovered")).toEqual({
			updated: true,
			content: "recovered",
		});
		expect(readdirSync(path.dirname(filePath))).toEqual(["scratchpad.md"]);
	});
});
