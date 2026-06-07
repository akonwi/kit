import { describe, expect, test } from "bun:test";
import type { AssistantMessage, ToolCall } from "../../runtime/agent";
import { groupItemsForDisplay, type TranscriptItem } from "./turns";

function assistantMessage(
	content: AssistantMessage["content"],
): AssistantMessage {
	return {
		role: "assistant",
		content,
		timestamp: 1,
	} as AssistantMessage;
}

function toolCall(name = "bash"): ToolCall {
	return {
		type: "toolCall",
		id: `tool-${crypto.randomUUID()}`,
		name,
		arguments: { command: "echo hidden" },
	} as ToolCall;
}

function assistantItemWithId(
	id: string,
	turnId: string,
	message: AssistantMessage,
): Extract<TranscriptItem, { kind: "assistant" }> {
	return {
		kind: "assistant",
		id,
		turnId,
		message,
		toolResults: new Map(),
		aborted: false,
	};
}

function userItem(id: string, turnId: string): TranscriptItem {
	return {
		kind: "user",
		id,
		turnId,
		message: { role: "user", content: "hello" } as TranscriptItem extends {
			kind: "user";
		}
			? TranscriptItem["message"]
			: never,
		aborted: false,
	} as TranscriptItem;
}

describe("groupItemsForDisplay", () => {
	test("wraps single items as-is", () => {
		const u = userItem("u1", "t1");
		const result = groupItemsForDisplay([u]);
		expect(result).toEqual([{ kind: "single", item: u }]);
	});

	test("does not fold when turn has a single assistant message", () => {
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));
		const result = groupItemsForDisplay([a1]);
		expect(result).toEqual([{ kind: "single", item: a1 }]);
	});

	test("folds all assistant items when none has prose", () => {
		const a1 = assistantItemWithId(
			"a1",
			"t1",
			assistantMessage([toolCall("Read")]),
		);
		const a2 = assistantItemWithId(
			"a2",
			"t1",
			assistantMessage([toolCall("Grep")]),
		);
		const a3 = assistantItemWithId(
			"a3",
			"t1",
			assistantMessage([toolCall("Edit")]),
		);

		const result = groupItemsForDisplay([a1, a2, a3]);
		expect(result).toHaveLength(1);
		expect(result[0].kind).toBe("turn-work");
		if (result[0].kind === "turn-work") {
			expect(result[0].items).toHaveLength(3);
			expect(result[0].turnId).toBe("t1");
		}
	});

	test("folds intermediate items and emits the final prose message as single", () => {
		const a1 = assistantItemWithId(
			"a1",
			"t1",
			assistantMessage([
				{ type: "text", text: "Now reading" },
				toolCall("Read"),
			]),
		);
		const a2 = assistantItemWithId(
			"a2",
			"t1",
			assistantMessage([
				{ type: "text", text: "Now editing" },
				toolCall("Edit"),
			]),
		);
		const final = assistantItemWithId(
			"a3",
			"t1",
			assistantMessage([{ type: "text", text: "All done." }]),
		);

		const result = groupItemsForDisplay([a1, a2, final]);
		expect(result).toHaveLength(2);
		expect(result[0].kind).toBe("turn-work");
		if (result[0].kind === "turn-work") {
			expect(result[0].items).toHaveLength(2);
		}
		expect(result[1]).toEqual({ kind: "single", item: final });
	});

	test("emits user message as single before the turn-work drawer", () => {
		const u = userItem("u1", "t1");
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));
		const a2 = assistantItemWithId("a2", "t1", assistantMessage([toolCall()]));
		const aFinal = assistantItemWithId(
			"a3",
			"t1",
			assistantMessage([{ type: "text", text: "Done" }]),
		);

		const result = groupItemsForDisplay([u, a1, a2, aFinal]);
		expect(result).toHaveLength(3);
		expect(result[0]).toEqual({ kind: "single", item: u });
		expect(result[1].kind).toBe("turn-work");
		if (result[1].kind === "turn-work") {
			expect(result[1].items).toHaveLength(2);
		}
		expect(result[2]).toEqual({ kind: "single", item: aFinal });
	});

	test("does not fold a single intermediate item; emits as single", () => {
		const u = userItem("u1", "t1");
		const intermediate = assistantItemWithId(
			"a1",
			"t1",
			assistantMessage([toolCall()]),
		);
		const final = assistantItemWithId(
			"a2",
			"t1",
			assistantMessage([{ type: "text", text: "Done" }]),
		);

		const result = groupItemsForDisplay([u, intermediate, final]);
		expect(result).toHaveLength(3);
		expect(result[0]).toEqual({ kind: "single", item: u });
		expect(result[1]).toEqual({ kind: "single", item: intermediate });
		expect(result[2]).toEqual({ kind: "single", item: final });
	});

	test("separates folding across different turns", () => {
		const t1a1 = assistantItemWithId(
			"t1a1",
			"t1",
			assistantMessage([toolCall()]),
		);
		const t1a2 = assistantItemWithId(
			"t1a2",
			"t1",
			assistantMessage([toolCall()]),
		);
		const t2a1 = assistantItemWithId(
			"t2a1",
			"t2",
			assistantMessage([toolCall()]),
		);

		const result = groupItemsForDisplay([t1a1, t1a2, t2a1]);
		expect(result).toHaveLength(2);
		expect(result[0].kind).toBe("turn-work");
		if (result[0].kind === "turn-work") {
			expect(result[0].turnId).toBe("t1");
		}
		expect(result[1]).toEqual({ kind: "single", item: t2a1 });
	});
});
