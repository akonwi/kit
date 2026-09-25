import { describe, expect, test } from "bun:test";
import type { ReviewDraftState } from "../review/draft";
import {
	draftForFile,
	fileContentRevision,
	fileReviewAttachmentId,
} from "./FileCommentView";

describe("file comments", () => {
	test("isolates one file's notes from the shared working-tree draft", () => {
		const state: ReviewDraftState = {
			fileNotes: new Map([
				["unchanged:src/a.ts", "A file note"],
				["unchanged:src/b.ts", "B file note"],
			]),
			rangeNotes: new Map([
				["src/a.ts::additions::2-4", "A range note"],
				["src/b.ts::additions::1-1", "B range note"],
			]),
		};

		expect(draftForFile("src/a.ts", state)).toEqual({
			fileNotes: new Map([["unchanged:src/a.ts", "A file note"]]),
			rangeNotes: new Map([["src/a.ts::additions::2-4", "A range note"]]),
		});
	});

	test("changes the pinned revision when file content changes", () => {
		expect(fileContentRevision(["one", "two"])).not.toBe(
			fileContentRevision(["zero", "one", "two"]),
		);
		expect(fileContentRevision(["one", "two"])).toBe(
			fileContentRevision(["one", "two"]),
		);
	});

	test("gives each repository file a stable attachment identity", () => {
		expect(fileReviewAttachmentId("/repo", "src/a.ts")).toBe(
			"file-review:/repo:src/a.ts",
		);
		expect(fileReviewAttachmentId("/repo", "src/b.ts")).not.toBe(
			fileReviewAttachmentId("/repo", "src/a.ts"),
		);
	});
});
