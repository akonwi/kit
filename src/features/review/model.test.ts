import { describe, expect, test } from "bun:test";
import { buildSkippedSectionsForFile, type ReviewHunk } from "./model";

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
});
