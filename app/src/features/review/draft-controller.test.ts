import { describe, expect, test } from "bun:test";
import type { ReviewDraftState } from "./draft";
import {
	createReviewDraftController,
	reviewTargetKey,
} from "./draft-controller";
import type { ReviewTarget } from "./model";

function draft(comment: string): ReviewDraftState {
	return {
		fileNotes: new Map([["working:->src/test.ts", comment]]),
		rangeNotes: new Map(),
	};
}

describe("review draft controller", () => {
	test("isolates repositories and targets", () => {
		const controller = createReviewDraftController("session-a");
		const token = controller.currentToken();
		const commit: ReviewTarget = { kind: "commit", sha: "abc123" };
		controller.saveDraft(token, "/repo-a", { kind: "working" }, draft("a"));
		controller.saveDraft(token, "/repo-a", commit, draft("commit"));
		controller.saveDraft(token, "/repo-b", { kind: "working" }, draft("b"));

		expect(
			Array.from(
				controller
					.getDraft(token, "/repo-a", { kind: "working" })
					.fileNotes.values(),
			),
		).toContain("a");
		expect(
			Array.from(
				controller.getDraft(token, "/repo-a", commit).fileNotes.values(),
			),
		).toContain("commit");
		expect(
			Array.from(
				controller
					.getDraft(token, "/repo-b", { kind: "working" })
					.fileNotes.values(),
			),
		).toContain("b");
	});

	test("clones drafts on save and read", () => {
		const controller = createReviewDraftController("session-a");
		const token = controller.currentToken();
		const state = draft("saved");
		controller.saveDraft(token, "/repo", { kind: "working" }, state);
		state.fileNotes.set("later", "mutation");

		const restored = controller.getDraft(token, "/repo", { kind: "working" });
		expect(restored.fileNotes.has("later")).toBe(false);
		restored.fileNotes.set("read", "mutation");
		expect(
			controller
				.getDraft(token, "/repo", { kind: "working" })
				.fileNotes.has("read"),
		).toBe(false);
	});

	test("removes empty drafts and clears only the selected target", () => {
		const controller = createReviewDraftController("session-a");
		const token = controller.currentToken();
		const working: ReviewTarget = { kind: "working" };
		const commit: ReviewTarget = { kind: "commit", sha: "abc123" };
		controller.saveDraft(token, "/repo", working, draft("working"));
		controller.saveDraft(token, "/repo", commit, draft("commit"));
		controller.clearDraft(token, "/repo", working);

		expect(controller.getDraft(token, "/repo", working).fileNotes.size).toBe(0);
		expect(controller.getDraft(token, "/repo", commit).fileNotes.size).toBe(1);
		expect(controller.countDraftsExcept(token, "/repo", commit)).toBe(0);

		controller.saveDraft(token, "/repo", commit, {
			fileNotes: new Map(),
			rangeNotes: new Map(),
		});
		expect(controller.getDraft(token, "/repo", commit).fileNotes.size).toBe(0);
	});

	test("remembers the last target per repository", () => {
		const controller = createReviewDraftController("session-a");
		const token = controller.currentToken();
		const target: ReviewTarget = { kind: "commit", sha: "abc123" };
		controller.setLastTarget(token, "/repo", target);

		expect(controller.getLastTarget(token, "/repo")).toEqual(target);
		expect(controller.getLastTarget(token, "/other")).toEqual({
			kind: "working",
		});
	});

	test("session reset clears drafts and rejects stale writes", () => {
		const controller = createReviewDraftController("session-a");
		const staleToken = controller.currentToken();
		controller.saveDraft(
			staleToken,
			"/repo",
			{ kind: "working" },
			draft("old"),
		);

		controller.resetForSession("session-b");
		const currentToken = controller.currentToken();
		controller.saveDraft(
			staleToken,
			"/repo",
			{ kind: "working" },
			draft("stale"),
		);

		expect(
			controller.getDraft(currentToken, "/repo", { kind: "working" }).fileNotes
				.size,
		).toBe(0);
		expect(currentToken.generation).toBe(staleToken.generation + 1);
		expect(currentToken.sessionId).toBe("session-b");
	});

	test("builds stable target keys", () => {
		expect(reviewTargetKey({ kind: "working" })).toBe("working");
		expect(reviewTargetKey({ kind: "commit", sha: "abc" })).toBe("commit:abc");
		expect(
			reviewTargetKey({
				kind: "branch",
				base: "main",
				head: "def",
				mergeBase: "abc",
			}),
		).toBe("branch:main:abc:def");
	});
});
