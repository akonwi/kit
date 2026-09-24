import { describe, expect, test } from "bun:test";
import { reconcileReviewFiles } from "./live-files";
import type { ReviewFile } from "./model";

function reviewFile(id: string, rawPatch: string): ReviewFile {
	return { id, status: "modified", rawPatch } as unknown as ReviewFile;
}

describe("live review file reconciliation", () => {
	test("reuses unchanged files when another diff changes", () => {
		const a = reviewFile("a", "old a");
		const b = reviewFile("b", "same b");
		const nextA = reviewFile("a", "new a");
		const nextB = reviewFile("b", "same b");

		const reconciled = reconcileReviewFiles([a, b], [nextA, nextB]);

		expect(reconciled[0]).toBe(nextA);
		expect(reconciled[1]).toBe(b);
	});

	test("preserves a missing file when it still owns draft notes", () => {
		const drafted = reviewFile("drafted", "old diff");
		const current = [drafted, reviewFile("clean", "old diff")];
		const reconciled = reconcileReviewFiles(
			current,
			[],
			(file) => file.id === "drafted",
		);

		expect(reconciled).toEqual([drafted]);
	});

	test("reuses the array when no rendered diff changed", () => {
		const current = [reviewFile("a", "same")];
		const reconciled = reconcileReviewFiles(current, [reviewFile("a", "same")]);

		expect(reconciled).toBe(current);
	});
});
