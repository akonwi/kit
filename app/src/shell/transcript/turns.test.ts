import { describe, expect, test } from "bun:test";
import type { AssistantMessage, ToolCall } from "../../runtime/agent";
import {
	filterTranscriptItemsForDisplay,
	groupItemsForDisplay,
	type TranscriptItem,
} from "./turns";

function assistantMessage(
	content: AssistantMessage["content"],
): AssistantMessage {
	return {
		role: "assistant",
		content,
		timestamp: 1,
	} as AssistantMessage;
}

function assistantItem(message: AssistantMessage): TranscriptItem {
	return {
		kind: "assistant",
		id: "turn-1:assistant:1",
		turnId: "turn-1",
		message,
		toolResults: new Map(),
		aborted: false,
	};
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

describe("filterTranscriptItemsForDisplay", () => {
	test("keeps all transcript items outside zen mode", () => {
		const toolOnly = assistantItem(assistantMessage([toolCall()]));

		expect(
			filterTranscriptItemsForDisplay([toolOnly], { zenMode: false }),
		).toEqual([toolOnly]);
	});

	test("hides assistant messages that only contain tool calls in zen mode", () => {
		const toolOnly = assistantItem(assistantMessage([toolCall()]));

		expect(
			filterTranscriptItemsForDisplay([toolOnly], { zenMode: true }),
		).toEqual([]);
	});

	test("keeps assistant text and user-triggered bash in zen mode", () => {
		const assistantWithText = assistantItem(
			assistantMessage([
				toolCall(),
				{ type: "text", text: "The answer is 42." },
			]),
		);
		const bashItem: TranscriptItem = {
			kind: "bash",
			id: "turn-2:bashExecution:1",
			turnId: "turn-2",
			message: {
				role: "bashExecution",
				id: "bash-1",
				command: "pwd",
				output: "/tmp",
				exitCode: 0,
				cancelled: false,
				truncated: false,
				excludeFromContext: false,
				timestamp: 2,
			},
		};

		expect(
			filterTranscriptItemsForDisplay([assistantWithText, bashItem], {
				zenMode: true,
			}),
		).toEqual([assistantWithText, bashItem]);
	});
});

describe("groupItemsForDisplay", () => {
	test("wraps single items as-is", () => {
		const user: TranscriptItem = {
			kind: "user",
			id: "u1",
			turnId: "t1",
			message: { role: "user", content: "hello" } as TranscriptItem extends {
				kind: "user";
			}
				? TranscriptItem["message"]
				: never,
			aborted: false,
		} as TranscriptItem;
		const result = groupItemsForDisplay([user]);
		expect(result).toEqual([{ kind: "single", item: user }]);
	});

	test("groups consecutive tool-only assistant items in the same turn", () => {
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
		expect(result[0].kind).toBe("tool-group");
		if (result[0].kind === "tool-group") {
			expect(result[0].items).toHaveLength(3);
			expect(result[0].turnId).toBe("t1");
		}
	});

	test("does not group a single tool-only item", () => {
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));
		const result = groupItemsForDisplay([a1]);
		expect(result).toEqual([{ kind: "single", item: a1 }]);
	});

	test("breaks groups across different turns", () => {
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));
		const a2 = assistantItemWithId("a2", "t2", assistantMessage([toolCall()]));

		const result = groupItemsForDisplay([a1, a2]);
		expect(result).toHaveLength(2);
		expect(result[0]).toEqual({ kind: "single", item: a1 });
		expect(result[1]).toEqual({ kind: "single", item: a2 });
	});

	test("breaks group when prose item is interleaved", () => {
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));
		const prose = assistantItemWithId(
			"a2",
			"t1",
			assistantMessage([{ type: "text", text: "Some explanation" }]),
		);
		const a3 = assistantItemWithId("a3", "t1", assistantMessage([toolCall()]));

		const result = groupItemsForDisplay([a1, prose, a3]);
		expect(result).toHaveLength(3);
		expect(result[0]).toEqual({ kind: "single", item: a1 });
		expect(result[1]).toEqual({ kind: "single", item: prose });
		expect(result[2]).toEqual({ kind: "single", item: a3 });
	});

	test("groups tool-only items followed by prose item", () => {
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));
		const a2 = assistantItemWithId("a2", "t1", assistantMessage([toolCall()]));
		const prose = assistantItemWithId(
			"a3",
			"t1",
			assistantMessage([toolCall(), { type: "text", text: "Done." }]),
		);

		const result = groupItemsForDisplay([a1, a2, prose]);
		expect(result).toHaveLength(2);
		expect(result[0].kind).toBe("tool-group");
		expect(result[1]).toEqual({ kind: "single", item: prose });
	});
});
