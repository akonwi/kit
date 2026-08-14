import { describe, expect, test } from "bun:test";
import { mergeQueuedFollowUpsIntoDraft } from "./composer-draft";

describe("queued follow-up draft restoration", () => {
	test("restores messages in queue order", () => {
		expect(mergeQueuedFollowUpsIntoDraft(["first", "second"], "")).toBe(
			"first\n\nsecond",
		);
	});

	test("preserves the existing draft after restored messages", () => {
		expect(
			mergeQueuedFollowUpsIntoDraft(["first", "second"], "current draft"),
		).toBe("first\n\nsecond\n\ncurrent draft");
	});
});
