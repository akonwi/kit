import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, realpath, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { chdirIfNeeded, safeProcessCwd } from "./process-cwd";

const originalCwd = process.cwd();
const originalPwd = process.env.PWD;

afterEach(() => {
	process.chdir(originalCwd);
	if (originalPwd === undefined) delete process.env.PWD;
	else process.env.PWD = originalPwd;
});

describe("safeProcessCwd", () => {
	test("updates PWD when changing directories", async () => {
		const tempRoot = await mkdtemp(path.join(tmpdir(), "kit-safe-cwd-"));
		try {
			process.env.PWD = originalCwd;
			chdirIfNeeded(tempRoot);

			expect(process.cwd()).toBe(await realpath(tempRoot));
			expect(process.env.PWD).toBe(process.cwd());
		} finally {
			process.chdir(originalCwd);
			await rm(tempRoot, { recursive: true, force: true });
		}
	});

	test("falls back when the current directory has been deleted", async () => {
		const tempRoot = await mkdtemp(path.join(tmpdir(), "kit-safe-cwd-"));
		const deletedCwd = path.join(tempRoot, "deleted");
		await mkdir(deletedCwd);
		try {
			process.chdir(deletedCwd);
			await rm(deletedCwd, { recursive: true, force: true });
			process.env.PWD = tempRoot;

			expect(safeProcessCwd()).toBe(tempRoot);
		} finally {
			process.chdir(originalCwd);
			await rm(tempRoot, { recursive: true, force: true });
		}
	});
});
