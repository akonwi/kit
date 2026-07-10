import { describe, expect, test } from "bun:test";
import {
	reviewAttachmentMetaText,
	reviewAttachmentSourceEquals,
} from "./ReviewAttachmentViewer";

const review = {
	submittedAt: new Date(0).toISOString(),
	files: [
		{
			path: "src/a.ts",
			fileComment: "File note",
			ranges: [
				{
					side: "additions" as const,
					startLine: 2,
					endLine: 3,
					comment: "Range note",
				},
			],
		},
	],
};

describe("review attachment viewer", () => {
	test("summarizes files and comments", () => {
		expect(reviewAttachmentMetaText(review)).toBe("2 comments · 1 file");
	});

	test("compares draft and historical sources by stable identity", () => {
		expect(
			reviewAttachmentSourceEquals(
				{ kind: "draft", attachmentId: "code-review" },
				{ kind: "draft", attachmentId: "code-review" },
			),
		).toBe(true);
		expect(
			reviewAttachmentSourceEquals(
				{ kind: "historical", id: "message:0", review },
				{ kind: "historical", id: "message:0", review },
			),
		).toBe(true);
		expect(
			reviewAttachmentSourceEquals(
				{ kind: "draft", attachmentId: "code-review" },
				{ kind: "historical", id: "message:0", review },
			),
		).toBe(false);
	});
});
