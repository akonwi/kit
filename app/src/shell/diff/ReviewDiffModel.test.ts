import { describe, expect, test } from "bun:test";
import type { DiffLineAnnotation } from "@pierre/diffs";
import {
	buildReviewDiffSplitRows,
	buildReviewDiffUnifiedRows,
	estimateWrappedRows,
	getReviewDiffActiveLineId,
	getReviewDiffLineHeight,
	getReviewDiffLineTop,
	getReviewDiffRangeBounds,
	type ReviewDiffAnnotationMetadata,
	shouldResetPatchScroll,
} from "./ReviewDiffModel";
import type { ReviewHunk } from "./types";

function makeHunk(): ReviewHunk {
	return {
		id: "hunk-1",
		noteKey: "hunk-1",
		header: "@@ -9,4 +9,3 @@",
		context: "function example()",
		lines: [
			{
				kind: "context",
				text: "function example() {",
				deletionLineNumber: 9,
				additionLineNumber: 9,
			},
			{ kind: "delete", text: "  oldOne();", deletionLineNumber: 10 },
			{ kind: "delete", text: "  oldTwo();", deletionLineNumber: 11 },
			{ kind: "add", text: "  next();", additionLineNumber: 10 },
			{
				kind: "context",
				text: "}",
				deletionLineNumber: 12,
				additionLineNumber: 11,
			},
		],
		changeCount: 3,
		rawPatch: "",
		patchStartLine: 0,
		patchLineCount: 5,
		additionStart: 9,
		additionCount: 3,
		deletionStart: 9,
		deletionCount: 4,
		collapsedBefore: 0,
	};
}

function annotation(
	side: "additions" | "deletions",
	lineNumber: number,
	comment = "note",
): DiffLineAnnotation<ReviewDiffAnnotationMetadata> {
	return {
		side,
		lineNumber,
		metadata: {
			key: `${side}:${lineNumber}`,
			comment,
			side,
			startLine: lineNumber,
			endLine: lineNumber,
		},
	};
}

describe("ReviewDiffModel row model", () => {
	test("builds unified rows from hunk lines", () => {
		const rows = buildReviewDiffUnifiedRows(makeHunk());

		expect(rows.map((row) => [row.kind, row.sign, row.text])).toEqual([
			["context", " ", "function example() {"],
			["delete", "-", "  oldOne();"],
			["delete", "-", "  oldTwo();"],
			["add", "+", "  next();"],
			["context", " ", "}"],
		]);
		expect(rows[1]).toMatchObject({
			lineIndex: 1,
			deletionLineNumber: 10,
			additionLineNumber: undefined,
		});
	});

	test("builds split rows by pairing same change groups", () => {
		const rows = buildReviewDiffSplitRows(makeHunk());

		expect(rows).toHaveLength(4);
		expect(rows[0]).toMatchObject({
			deletion: { kind: "context", lineIndex: 0, lineNumber: 9 },
			addition: { kind: "context", lineIndex: 0, lineNumber: 9 },
		});
		expect(rows[1]).toMatchObject({
			deletion: { kind: "delete", lineIndex: 1, lineNumber: 10 },
			addition: { kind: "add", lineIndex: 3, lineNumber: 10 },
		});
		expect(rows[2]).toMatchObject({
			deletion: { kind: "delete", lineIndex: 2, lineNumber: 11 },
			addition: { kind: "empty" },
		});
		expect(rows[2].addition.lineIndex).toBeUndefined();
	});

	test("preserves source line indexes for windowed hunks", () => {
		const hunk = {
			...makeHunk(),
			lines: makeHunk().lines.slice(1, 4),
			lineIndexOffset: 1,
		};

		expect(
			buildReviewDiffUnifiedRows(hunk).map((row) => row.lineIndex),
		).toEqual([1, 2, 3]);
		expect(buildReviewDiffSplitRows(hunk)[0]).toMatchObject({
			deletion: { kind: "delete", lineIndex: 1, lineNumber: 10 },
			addition: { kind: "add", lineIndex: 3, lineNumber: 10 },
		});
	});
});

describe("ReviewDiffModel annotation placement", () => {
	test("offsets later unified lines by preceding annotation height", () => {
		const hunk = makeHunk();
		const annotations = [annotation("deletions", 10)];

		expect(getReviewDiffLineTop(hunk, 1, "unified", annotations)).toBe(1);
		expect(getReviewDiffLineTop(hunk, 2, "unified", annotations)).toBe(5);
	});

	test("offsets later split rows by max annotation row height", () => {
		const hunk = makeHunk();
		const annotations = [annotation("additions", 10)];

		expect(getReviewDiffLineTop(hunk, 3, "split", annotations)).toBe(1);
		expect(getReviewDiffLineTop(hunk, 2, "split", annotations)).toBe(5);
	});

	test("computes range bounds over visible commentable lines", () => {
		const hunk = makeHunk();
		const annotations = [annotation("additions", 10)];

		expect(
			getReviewDiffRangeBounds(
				hunk,
				{ side: "deletions", startLine: 10, endLine: 11 },
				"split",
				annotations,
			),
		).toEqual({ top: 1, height: 5 });
	});
});

describe("ReviewDiffModel active line ids", () => {
	test("uses stable active line ids for scrolling", () => {
		expect(getReviewDiffActiveLineId("hunk-1", 3)).toBe(
			"review-line-cursor-hunk-1-3",
		);
	});
});

describe("estimateWrappedRows", () => {
	test("returns 1 for empty or short text", () => {
		expect(estimateWrappedRows("", 10)).toBe(1);
		expect(estimateWrappedRows("hi", 10)).toBe(1);
		expect(estimateWrappedRows("exactly10!", 10)).toBe(1);
	});

	test("wraps prose at word boundaries", () => {
		// "hello" (5), then " world foo" (10), then " bar" (4)
		expect(estimateWrappedRows("hello world foo bar", 10)).toBe(3);
	});

	test("counts whitespace segments consistently", () => {
		expect(estimateWrappedRows("aa  bb  cc", 6)).toBe(2);
		expect(estimateWrappedRows("aa      bb", 6)).toBe(3);
	});

	test("breaks words longer than width", () => {
		// 25-char word into 10-col width → 3 rows
		expect(estimateWrappedRows("a".repeat(25), 10)).toBe(3);
	});

	test("handles unknown width gracefully", () => {
		expect(estimateWrappedRows("anything", 0)).toBe(1);
		expect(estimateWrappedRows("anything", -5)).toBe(1);
	});
});

describe("shouldResetPatchScroll", () => {
	test("resets on file changes or explicit file-open reset", () => {
		expect(shouldResetPatchScroll(undefined, "file-a", false)).toBe(true);
		expect(shouldResetPatchScroll("file-a", "file-a", false)).toBe(false);
		expect(shouldResetPatchScroll("file-a", "file-b", false)).toBe(true);
		expect(shouldResetPatchScroll("file-b", "file-b", true)).toBe(true);
	});
});

describe("ReviewDiffModel wrap-aware positioning", () => {
	function longHunk(): ReviewHunk {
		return {
			id: "long-hunk",
			noteKey: "long-hunk",
			header: "@@ -1,3 +1,3 @@",
			context: "",
			lines: [
				{
					kind: "context",
					text: "x".repeat(25),
					deletionLineNumber: 1,
					additionLineNumber: 1,
				},
				{
					kind: "context",
					text: "short",
					deletionLineNumber: 2,
					additionLineNumber: 2,
				},
				{
					kind: "context",
					text: "another short",
					deletionLineNumber: 3,
					additionLineNumber: 3,
				},
			],
			changeCount: 0,
			rawPatch: "",
			patchStartLine: 0,
			patchLineCount: 3,
			additionStart: 1,
			additionCount: 3,
			deletionStart: 1,
			deletionCount: 3,
			collapsedBefore: 0,
		};
	}

	test("unified line top accounts for wrapped preceding lines", () => {
		const hunk = longHunk();
		// Width 10 → 25-char first line wraps to 3 rows
		expect(getReviewDiffLineTop(hunk, 0, "unified", [], 10)).toBe(0);
		expect(getReviewDiffLineTop(hunk, 1, "unified", [], 10)).toBe(3);
		expect(getReviewDiffLineTop(hunk, 2, "unified", [], 10)).toBe(4);
	});

	test("line height reports wrapped row count", () => {
		const hunk = longHunk();
		expect(getReviewDiffLineHeight(hunk, 0, "unified", 10)).toBe(3);
		expect(getReviewDiffLineHeight(hunk, 1, "unified", 10)).toBe(1);
	});

	test("falls back to 1-row math when content columns omitted", () => {
		const hunk = longHunk();
		expect(getReviewDiffLineTop(hunk, 1, "unified")).toBe(1);
		expect(getReviewDiffLineHeight(hunk, 0, "unified")).toBe(1);
	});
});
