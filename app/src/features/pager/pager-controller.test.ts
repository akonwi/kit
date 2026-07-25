import { describe, expect, test } from "bun:test";
import type { AgentMessage } from "../../runtime/agent";
import {
	createPagerController,
	formatPagerFeedbackMessage,
} from "./pager-controller";

function assistantMessage(text: string, turnId = "turn-1"): AgentMessage {
	const message: AgentMessage = {
		role: "assistant",
		content: [{ type: "text", text }],
		api: "anthropic-messages",
		provider: "anthropic",
		model: "test-model",
		usage: {
			input: 1,
			output: 1,
			cacheRead: 0,
			cacheWrite: 0,
			totalTokens: 2,
			cost: {
				input: 0,
				output: 0,
				cacheRead: 0,
				cacheWrite: 0,
				total: 0,
			},
		},
		stopReason: "stop",
		timestamp: Date.now(),
	};
	return Object.assign(message, { turnId });
}

const response = [
	"# Overview",
	"First section.",
	"",
	"## Details",
	"Second section.",
].join("\n");

describe("pager drafts", () => {
	test("preserves committed notes and position when closed and reopened", () => {
		const pager = createPagerController();
		expect(pager.tryActivate([assistantMessage(response)])).toBe(true);
		pager.setNote(0, "Keep this note");
		pager.nextSection();
		pager.closeView();

		expect(pager.active).toBe(false);
		expect(pager.notes.get(0)).toBe("Keep this note");
		expect(pager.reopen()).toBe(true);
		expect(pager.currentIndex).toBe(1);
	});

	test("replaces the draft when a different response is paged", () => {
		const pager = createPagerController();
		pager.tryActivate([assistantMessage(response)]);
		pager.setNote(0, "Old note");
		pager.closeView();

		pager.tryActivate([
			assistantMessage("A newer assistant response.", "turn-2"),
		]);
		expect(pager.notes.size).toBe(0);
		expect(pager.currentIndex).toBe(0);
	});

	test("does not restore notes for identical text from a different turn", () => {
		const pager = createPagerController();
		pager.tryActivate([assistantMessage(response, "turn-1")]);
		pager.setNote(0, "Old note");

		pager.tryActivate([assistantMessage(response, "turn-2")]);
		expect(pager.notes.size).toBe(0);
		expect(pager.currentIndex).toBe(0);
	});

	test("ignores stale attachment cleanup after a new draft opens", () => {
		const pager = createPagerController();
		pager.tryActivate([assistantMessage(response, "turn-1")]);
		pager.setNote(0, "Old note");
		const staleGeneration = pager.draftGeneration;

		pager.tryActivate([assistantMessage(response, "turn-2")]);
		pager.setNote(0, "New note");
		expect(pager.clearDraft(staleGeneration)).toBe(false);
		expect(pager.active).toBe(true);
		expect(pager.notes.get(0)).toBe("New note");
	});

	test("restores a cloned draft snapshot after controller recreation", () => {
		const original = createPagerController();
		original.tryActivate([assistantMessage(response, "turn-1")]);
		original.setNote(0, "Retained note");
		original.nextSection();
		const snapshot = original.getDraftSnapshot();
		if (!snapshot) throw new Error("Expected a pager draft snapshot");

		const restored = createPagerController();
		restored.restoreDraft(snapshot);
		snapshot.notes.clear();
		expect(restored.active).toBe(false);
		expect(restored.notes.get(0)).toBe("Retained note");
		expect(restored.currentIndex).toBe(1);
		expect(restored.reopen()).toBe(true);
	});

	test("clearDraft removes retained content and notes", () => {
		const pager = createPagerController();
		pager.tryActivate([assistantMessage(response)]);
		pager.setNote(0, "Discard me");

		pager.clearDraft();
		expect(pager.reopen()).toBe(false);
		expect(pager.notes.size).toBe(0);
		expect(pager.sections).toHaveLength(0);
	});

	test("formats committed section notes as pager feedback", () => {
		const pager = createPagerController();
		pager.tryActivate([assistantMessage(response)]);
		pager.setNote(1, "Please clarify this detail.");

		const feedback = pager.getFeedbackMessage();
		expect(feedback).toContain("Here is my feedback");
		expect(feedback).toContain("Overview: Details");
		expect(feedback).toContain("Please clarify this detail.");
		expect(formatPagerFeedbackMessage(pager.sections, new Map())).toBeNull();
	});
});
