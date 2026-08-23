import { describe, expect, test } from "bun:test";
import { createFileCommentDraftController } from "./comment-draft-controller";

describe("file comment draft controller", () => {
	test("isolates files and clones stored drafts", () => {
		const controller = createFileCommentDraftController("session-a");
		const token = controller.currentToken();
		const state = {
			fileNotes: new Map([["unchanged:a.ts", "note"]]),
			rangeNotes: new Map<string, string>(),
			revision: "rev-a",
		};
		controller.saveDraft(token, "/repo", "a.ts", state);
		state.fileNotes.clear();

		expect(controller.getDraft(token, "/repo", "a.ts").fileNotes.size).toBe(1);
		expect(controller.getDraft(token, "/repo", "b.ts").fileNotes.size).toBe(0);
	});

	test("consumes only the submitted snapshot", () => {
		const controller = createFileCommentDraftController("session-a");
		const token = controller.currentToken();
		controller.saveDraft(token, "/repo", "a.ts", {
			fileNotes: new Map([["unchanged:a.ts", "new"]]),
			rangeNotes: new Map([["a.ts::additions::2-2", "range"]]),
			revision: "rev-a",
		});

		controller.consumeDraft(token, "/repo", "a.ts", {
			fileNotes: new Map([["unchanged:a.ts", "old"]]),
			rangeNotes: new Map([["a.ts::additions::2-2", "range"]]),
			revision: "rev-a",
		});

		const remaining = controller.getDraft(token, "/repo", "a.ts");
		expect(remaining.fileNotes.get("unchanged:a.ts")).toBe("new");
		expect(remaining.rangeNotes.size).toBe(0);
		expect(remaining.revision).toBe("rev-a");
	});

	test("rejects old-session consumption after reset", () => {
		const controller = createFileCommentDraftController("session-a");
		const oldToken = controller.currentToken();
		const submitted = {
			fileNotes: new Map([["unchanged:a.ts", "same note"]]),
			rangeNotes: new Map<string, string>(),
			revision: "rev-a",
		};
		controller.saveDraft(oldToken, "/repo", "a.ts", submitted);
		controller.resetForSession("session-b");
		const newToken = controller.currentToken();
		controller.saveDraft(newToken, "/repo", "a.ts", submitted);

		controller.consumeDraft(oldToken, "/repo", "a.ts", submitted);

		expect(
			controller
				.getDraft(newToken, "/repo", "a.ts")
				.fileNotes.get("unchanged:a.ts"),
		).toBe("same note");
		expect(controller.getDraft(oldToken, "/repo", "a.ts").fileNotes.size).toBe(
			0,
		);
	});

	test("clears every file draft on session reset", () => {
		const controller = createFileCommentDraftController("session-a");
		const token = controller.currentToken();
		controller.saveDraft(token, "/repo", "a.ts", {
			fileNotes: new Map([["unchanged:a.ts", "note"]]),
			rangeNotes: new Map(),
			revision: "rev-a",
		});
		const events: string[] = [];
		controller.subscribe((event) => events.push(event.type));

		controller.resetForSession("session-b");

		expect(
			controller.getDraft(controller.currentToken(), "/repo", "a.ts").fileNotes
				.size,
		).toBe(0);
		expect(events).toEqual(["reset"]);
	});
});
