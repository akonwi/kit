import { describe, expect, test } from "bun:test";
import type { ReviewFile } from "./model";
import {
	extractReviewRangeExcerpt,
	loadReviewAttachmentContext,
} from "./review-attachment-context";

function reviewFile(): ReviewFile {
	return {
		id: "working:src/test.ts",
		noteKey: "working:src/test.ts",
		path: "src/test.ts",
		status: "modify" as ReviewFile["status"],
		source: "working",
		rawPatch: "",
		hunks: [
			{
				id: "hunk",
				noteKey: "hunk",
				header: "@@ -10,5 +10,6 @@",
				context: "",
				lines: [
					{
						kind: "context",
						text: "before",
						additionLineNumber: 10,
						deletionLineNumber: 10,
					},
					{
						kind: "delete",
						text: "old",
						deletionLineNumber: 11,
					},
					{ kind: "add", text: "new", additionLineNumber: 11 },
					{ kind: "add", text: "extra", additionLineNumber: 12 },
					{
						kind: "context",
						text: "after",
						additionLineNumber: 13,
						deletionLineNumber: 12,
					},
				],
				changeCount: 3,
				rawPatch: "",
				patchStartLine: 0,
				patchLineCount: 5,
				additionStart: 10,
				additionCount: 4,
				deletionStart: 10,
				deletionCount: 3,
				collapsedBefore: 0,
			},
		],
		skippedSections: [],
		changeCount: 3,
		unifiedLineCount: 5,
		splitLineCount: 4,
	};
}

describe("review attachment context", () => {
	test("extracts bounded addition-side context", () => {
		const excerpt = extractReviewRangeExcerpt(
			reviewFile(),
			{
				side: "additions",
				startLine: 11,
				endLine: 12,
				comment: "note",
			},
			1,
		);

		expect(excerpt?.lines.map((line) => line.text)).toEqual([
			"old",
			"new",
			"extra",
			"after",
		]);
		expect(excerpt?.truncatedBefore).toBe(true);
		expect(excerpt?.truncatedAfter).toBe(false);
	});

	test("reports committed context unavailable outside its repository", async () => {
		const context = await loadReviewAttachmentContext({
			cwd: "/tmp",
			draft: false,
			review: {
				submittedAt: new Date(0).toISOString(),
				commit: { sha: "abc", parentSha: "def", subject: "test" },
				files: [{ path: "src/test.ts", fileComment: "note", ranges: [] }],
			},
		});
		expect(context.kind).toBe("unavailable");
	});

	test("uses deletion-side line numbers", () => {
		const excerpt = extractReviewRangeExcerpt(
			reviewFile(),
			{
				side: "deletions",
				startLine: 11,
				endLine: 11,
				comment: "note",
			},
			0,
		);

		expect(excerpt?.lines.map((line) => line.text)).toEqual(["old"]);
	});
});
