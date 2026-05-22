import { describe, expect, test } from "bun:test";
import type { DiffLineAnnotation } from "@pierre/diffs";
import type { ReviewHunk } from "./model";
import {
	buildReviewDiffSplitRows,
	buildReviewDiffUnifiedRows,
	getReviewDiffActiveLineId,
	getReviewDiffLineTop,
	getReviewDiffRangeBounds,
	type ReviewDiffAnnotationMetadata,
} from "./ReviewDiffModel";

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
