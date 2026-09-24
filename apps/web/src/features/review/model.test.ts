import { describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import {
	buildSkippedSectionsForFile,
	loadReviewFiles,
	normalizeReviewLineText,
	type ReviewHunk,
} from "./model";

test("normalizes parser line endings before layout", () => {
	expect(normalizeReviewLineText("source\n")).toBe("source");
	expect(normalizeReviewLineText("source\r\n")).toBe("source");
	expect(normalizeReviewLineText("source")).toBe("source");
	expect(normalizeReviewLineText("\n")).toBe("");
});

function makeHunk(overrides: Partial<ReviewHunk>): ReviewHunk {
	return {
		id: overrides.id ?? "hunk-1",
		noteKey: overrides.noteKey ?? "note",
		header: overrides.header ?? "@@ -1 +1 @@",
		context: overrides.context ?? "",
		lines: overrides.lines ?? [],
		changeCount: overrides.changeCount ?? 0,
		rawPatch: overrides.rawPatch ?? "",
		patchStartLine: overrides.patchStartLine ?? 0,
		patchLineCount: overrides.patchLineCount ?? 0,
		additionStart: overrides.additionStart ?? 1,
		additionCount: overrides.additionCount ?? 1,
		deletionStart: overrides.deletionStart ?? 1,
		deletionCount: overrides.deletionCount ?? 1,
		collapsedBefore: overrides.collapsedBefore ?? 0,
	};
}

describe("review model", () => {
	test("builds skipped sections before, between, and after hunks", () => {
		const skippedSections = buildSkippedSectionsForFile(
			"file-1",
			[
				"diff --git a/src/test.ts b/src/test.ts",
				"--- a/src/test.ts",
				"+++ b/src/test.ts",
				"@@ -3,2 +3,2 @@",
				"-before",
				"+after",
			].join("\n"),
			[
				makeHunk({
					id: "hunk-1",
					additionStart: 3,
					additionCount: 2,
					deletionStart: 3,
					deletionCount: 2,
					collapsedBefore: 2,
				}),
				makeHunk({
					id: "hunk-2",
					additionStart: 8,
					additionCount: 2,
					deletionStart: 8,
					deletionCount: 2,
					collapsedBefore: 3,
				}),
			],
			[
				"one",
				"two",
				"three",
				"four",
				"five",
				"six",
				"seven",
				"eight",
				"nine",
				"ten",
			],
		);

		expect(skippedSections).toHaveLength(3);
		expect(skippedSections.map((section) => section.beforeHunkIndex)).toEqual([
			0, 1, 2,
		]);
		expect(skippedSections.map((section) => section.lineCount)).toEqual([
			2, 3, 1,
		]);
		expect(skippedSections[0]?.rawPatch).toContain("@@ -1,2 +1,2 @@");
		expect(skippedSections[0]?.rawPatch).toContain(" one\n two");
		expect(skippedSections[1]?.rawPatch).toContain("@@ -5,3 +5,3 @@");
		expect(skippedSections[1]?.rawPatch).toContain(" five\n six\n seven");
		expect(skippedSections[2]?.rawPatch).toContain("@@ -10 +10 @@");
		expect(skippedSections[2]?.rawPatch).toContain(" ten");
	});

	test("keeps working-tree file identities stable when ordering changes", async () => {
		const repo = mkdtempSync(path.join(tmpdir(), "kit-live-review-"));
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
			writeFileSync(path.join(repo, "a.txt"), "a\n");
			writeFileSync(path.join(repo, "b.txt"), "b\n");
			git(["add", "."]);
			git(["commit", "-qm", "initial"]);

			writeFileSync(path.join(repo, "b.txt"), "changed b\n");
			const before = await loadReviewFiles(repo);
			const beforeId = before.find((file) => file.path === "b.txt")?.id;

			writeFileSync(path.join(repo, "a.txt"), "changed a\n");
			const after = await loadReviewFiles(repo);
			expect(after.map((file) => file.path)).toEqual(["a.txt", "b.txt"]);
			expect(after.find((file) => file.path === "b.txt")?.id).toBe(beforeId);
		} finally {
			rmSync(repo, { recursive: true, force: true });
		}
	});

	test("throws instead of returning empty when a pinned repo fails transiently", async () => {
		// Regression: a transient repo-root resolution failure used to publish
		// a successful empty file list, blanking the review pane's file tree.
		// A .git marker that git cannot resolve stands in for the transient
		// failure: the directory still claims to be a repository.
		const repo = mkdtempSync(path.join(tmpdir(), "kit-review-broken-repo-"));
		try {
			writeFileSync(
				path.join(repo, ".git"),
				"gitdir: /nonexistent-kit-target\n",
			);
			await expect(loadReviewFiles(repo)).rejects.toThrow(
				"Failed to resolve repository root.",
			);
		} finally {
			rmSync(repo, { recursive: true, force: true });
		}
	});

	test("returns empty when a pinned directory stopped being a repository", async () => {
		const gone = path.join(tmpdir(), "kit-review-missing-repo");
		rmSync(gone, { recursive: true, force: true });
		expect(await loadReviewFiles(gone)).toEqual([]);
	});

	test("still returns empty for an unpinned non-repo cwd", async () => {
		const plain = mkdtempSync(path.join(tmpdir(), "kit-review-non-repo-"));
		const previousCwd = process.cwd();
		try {
			process.chdir(plain);
			expect(await loadReviewFiles(undefined)).toEqual([]);
		} finally {
			process.chdir(previousCwd);
			rmSync(plain, { recursive: true, force: true });
		}
	});
});
