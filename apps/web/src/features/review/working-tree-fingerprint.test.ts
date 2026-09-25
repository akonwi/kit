import { describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { getWorkingTreeFingerprint } from "./working-tree-fingerprint";

describe("working tree fingerprint", () => {
	test("tracks same-size edits and broken symlink targets", async () => {
		const repo = mkdtempSync(path.join(tmpdir(), "kit-review-fingerprint-"));
		const git = (args: string[]) =>
			execFileSync("git", args, {
				cwd: repo,
				stdio: "ignore",
				env: {
					...process.env,
					GIT_CONFIG_GLOBAL: "/dev/null",
					GIT_CONFIG_SYSTEM: "/dev/null",
				},
			});
		try {
			git(["init", "-q"]);
			git(["config", "user.email", "test@example.com"]);
			git(["config", "user.name", "Test"]);
			const filePath = path.join(repo, "file.txt");
			const linkPath = path.join(repo, "link");
			writeFileSync(filePath, "base\n");
			symlinkSync("missing-base", linkPath);
			git(["add", "."]);
			git(["commit", "-qm", "initial"]);

			writeFileSync(filePath, "first\n");
			const first = await getWorkingTreeFingerprint(repo);
			await Bun.sleep(10);
			writeFileSync(filePath, "other\n");
			const second = await getWorkingTreeFingerprint(repo);

			expect(second).not.toBe(first);

			rmSync(linkPath);
			symlinkSync("missing-one", linkPath);
			const firstLink = await getWorkingTreeFingerprint(repo);
			await Bun.sleep(10);
			rmSync(linkPath);
			symlinkSync("missing-next", linkPath);
			const secondLink = await getWorkingTreeFingerprint(repo);
			expect(secondLink).not.toBe(firstLink);
		} finally {
			rmSync(repo, { recursive: true, force: true });
		}
	});
});
