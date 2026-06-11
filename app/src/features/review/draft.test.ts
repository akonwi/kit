import { describe, expect, test } from "bun:test";
import {
	buildRangeNoteKey,
	buildReviewSubmission,
	countDraftNotes,
} from "./draft";
import type { ReviewFile } from "./model";

function makeFile(): ReviewFile {
	return {
		id: "file-1",
		noteKey: "src/test.ts",
		path: "src/test.ts",
		status: "modify" as ReviewFile["status"],
		source: "working",
		filetype: "typescript",
		rawPatch: [
			"diff --git a/src/test.ts b/src/test.ts",
			"--- a/src/test.ts",
			"+++ b/src/test.ts",
			"@@ -1,2 +1,2 @@",
			"-before",
			"+after",
		].join("\n"),
		hunks: [
			{
				id: "hunk-1",
				noteKey: "hunk-1",
				header: "@@ -1,2 +1,2 @@",
				context: "",
				lines: [
					{ kind: "delete", text: "before", deletionLineNumber: 1 },
					{ kind: "add", text: "after", additionLineNumber: 1 },
				],
				changeCount: 2,
				rawPatch: [
					"diff --git a/src/test.ts b/src/test.ts",
					"--- a/src/test.ts",
					"+++ b/src/test.ts",
					"@@ -1,2 +1,2 @@",
					"-before",
					"+after",
				].join("\n"),
				patchStartLine: 0,
				patchLineCount: 2,
				additionStart: 1,
				additionCount: 1,
				deletionStart: 1,
				deletionCount: 1,
				collapsedBefore: 0,
			},
		],
		skippedSections: [],
		changeCount: 2,
		unifiedLineCount: 2,
		splitLineCount: 1,
	};
}

describe("review draft", () => {
	test("counts non-empty file and range notes", () => {
		expect(
			countDraftNotes({
				fileNotes: new Map([
					["file", "file note"],
					["empty", "   "],
				]),
				rangeNotes: new Map([["range", "line note"]]),
			}),
		).toBe(2);
	});

	test("builds review submission from file and range notes", () => {
		const file = makeFile();
		const review = buildReviewSubmission([file], {
			fileNotes: new Map([[file.noteKey, "Look at this file"]]),
			rangeNotes: new Map([
				[
					buildRangeNoteKey({
						path: file.path,
						side: "deletions",
						startLine: 1,
						endLine: 1,
					}),
					"Watch this removed line",
				],
			]),
		});

		expect(review).not.toBeNull();
		expect(review?.files).toHaveLength(1);
		expect(review?.files[0]).toEqual({
			path: "src/test.ts",
			fileComment: "Look at this file",
			ranges: [
				{
					side: "deletions",
					startLine: 1,
					endLine: 1,
					comment: "Watch this removed line",
				},
			],
		});
	});

	test("trims submitted notes and ignores malformed range keys", () => {
		const file = makeFile();
		const review = buildReviewSubmission([file], {
			fileNotes: new Map([[file.noteKey, "  Look at this file  "]]),
			rangeNotes: new Map([
				["src/test.ts::additions::not-a-range", "ignore me"],
				[
					buildRangeNoteKey({
						path: "src/other.ts",
						side: "additions",
						startLine: 1,
						endLine: 1,
					}),
					"ignore other files",
				],
				[
					buildRangeNoteKey({
						path: file.path,
						side: "additions",
						startLine: 1,
						endLine: 1,
					}),
					"  Keep this added line  ",
				],
			]),
		});

		expect(review?.files).toEqual([
			{
				path: "src/test.ts",
				fileComment: "Look at this file",
				ranges: [
					{
						side: "additions",
						startLine: 1,
						endLine: 1,
						comment: "Keep this added line",
					},
				],
			},
			{
				path: "src/other.ts",
				fileComment: "",
				ranges: [
					{
						side: "additions",
						startLine: 1,
						endLine: 1,
						comment: "ignore other files",
					},
				],
			},
		]);
	});

	test("returns null when no notes are present", () => {
		const file = makeFile();
		expect(
			buildReviewSubmission([file], {
				fileNotes: new Map(),
				rangeNotes: new Map(),
			}),
		).toBeNull();
	});
});
