import { describe, expect, test } from "bun:test";
import type {
	AssistantMessage,
	CustomAgentMessages,
	ToolCall,
	ToolResultMessage,
} from "../../runtime/agent";
import {
	formatToolArgs,
	groupItemsForDisplay,
	reconcileTranscriptItems,
	type TranscriptItem,
	toolDisplayName,
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

function bashItem(
	id: string,
	turnId: string,
): Extract<TranscriptItem, { kind: "bash" }> {
	return {
		kind: "bash",
		id,
		turnId,
		message: {
			role: "bashExecution",
			command: "echo manual",
			output: "manual\n",
			exitCode: 0,
			timestamp: 1,
		} as CustomAgentMessages["bashExecution"],
	};
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

	test("keeps manual bash executions standalone even when sharing a tool-work turn", () => {
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));
		const a2 = assistantItemWithId("a2", "t1", assistantMessage([toolCall()]));
		const bash = bashItem("bash1", "t1");

		const result = groupItemsForDisplay([a1, a2, bash]);

		expect(result).toHaveLength(2);
		expect(result[0].kind).toBe("turn-work");
		if (result[0].kind === "turn-work") {
			expect(result[0].items).toEqual([a1, a2]);
		}
		expect(result[1]).toEqual({ kind: "single", item: bash });
	});

	test("manual bash executions split surrounding tool-work groups", () => {
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));
		const bash = bashItem("bash1", "t1");
		const a2 = assistantItemWithId("a2", "t1", assistantMessage([toolCall()]));
		const a3 = assistantItemWithId("a3", "t1", assistantMessage([toolCall()]));

		const result = groupItemsForDisplay([a1, bash, a2, a3]);

		expect(result).toHaveLength(3);
		expect(result[0]).toEqual({ kind: "single", item: a1 });
		expect(result[1]).toEqual({ kind: "single", item: bash });
		expect(result[2].kind).toBe("turn-work");
		if (result[2].kind === "turn-work") {
			expect(result[2].items).toEqual([a2, a3]);
		}
	});

	test("in-progress turn: keeps manual bash executions out of the drawer", () => {
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));
		const bash = bashItem("bash1", "t1");

		const result = groupItemsForDisplay([a1, bash], "t1");

		expect(result).toHaveLength(2);
		expect(result[0].kind).toBe("turn-work");
		if (result[0].kind === "turn-work") {
			expect(result[0].items).toEqual([a1]);
		}
		expect(result[1]).toEqual({ kind: "single", item: bash });
	});

	test("in-progress turn: folds a single assistant tool-only item into a drawer", () => {
		const u = userItem("u1", "t1");
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));

		const result = groupItemsForDisplay([u, a1], "t1");
		expect(result).toHaveLength(2);
		expect(result[0]).toEqual({ kind: "single", item: u });
		expect(result[1].kind).toBe("turn-work");
		if (result[1].kind === "turn-work") {
			expect(result[1].items).toHaveLength(1);
			expect(result[1].turnId).toBe("t1");
		}
	});

	test("in-progress turn: does not promote the last prose message to final", () => {
		const u = userItem("u1", "t1");
		const a1 = assistantItemWithId(
			"a1",
			"t1",
			assistantMessage([{ type: "text", text: "Reading" }, toolCall("Read")]),
		);
		const a2 = assistantItemWithId(
			"a2",
			"t1",
			assistantMessage([{ type: "text", text: "Latest prose" }]),
		);

		const result = groupItemsForDisplay([u, a1, a2], "t1");
		expect(result).toHaveLength(2);
		expect(result[0]).toEqual({ kind: "single", item: u });
		expect(result[1].kind).toBe("turn-work");
		if (result[1].kind === "turn-work") {
			expect(result[1].items).toHaveLength(2);
		}
	});

	test("in-progress turn: user-only turn does not emit an empty drawer", () => {
		const u = userItem("u1", "t1");
		const result = groupItemsForDisplay([u], "t1");
		expect(result).toEqual([{ kind: "single", item: u }]);
	});

	test("in-progress turn id only applies to its turn; other turns keep prior behavior", () => {
		const t1a1 = assistantItemWithId(
			"t1a1",
			"t1",
			assistantMessage([toolCall()]),
		);
		const t2a1 = assistantItemWithId(
			"t2a1",
			"t2",
			assistantMessage([toolCall()]),
		);

		// t2 is in progress; t1 should keep its old single-message behavior.
		const result = groupItemsForDisplay([t1a1, t2a1], "t2");
		expect(result).toHaveLength(2);
		expect(result[0]).toEqual({ kind: "single", item: t1a1 });
		expect(result[1].kind).toBe("turn-work");
		if (result[1].kind === "turn-work") {
			expect(result[1].turnId).toBe("t2");
			expect(result[1].items).toHaveLength(1);
		}
	});

	test("clearing in-progress turn id restores final-prose grouping for the same items", () => {
		const u = userItem("u1", "t1");
		const a1 = assistantItemWithId("a1", "t1", assistantMessage([toolCall()]));
		const a2 = assistantItemWithId(
			"a2",
			"t1",
			assistantMessage([{ type: "text", text: "Done" }]),
		);

		const inProgress = groupItemsForDisplay([u, a1, a2], "t1");
		expect(inProgress).toHaveLength(2);
		expect(inProgress[1].kind).toBe("turn-work");

		const completed = groupItemsForDisplay([u, a1, a2], null);
		expect(completed).toHaveLength(3);
		expect(completed[0]).toEqual({ kind: "single", item: u });
		expect(completed[1]).toEqual({ kind: "single", item: a1 });
		expect(completed[2]).toEqual({ kind: "single", item: a2 });
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

	test("reuses unchanged display item objects from previous grouping", () => {
		const t1u = userItem("t1u", "t1");
		const t1a = assistantItemWithId(
			"t1a",
			"t1",
			assistantMessage([{ type: "text", text: "done" }]),
		);
		const previous = groupItemsForDisplay([t1u, t1a]);
		const t2u = userItem("t2u", "t2");

		const next = groupItemsForDisplay([t1u, t1a, t2u], null, previous);

		expect(next[0]).toBe(previous[0]);
		expect(next[1]).toBe(previous[1]);
		expect(next[2]).toEqual({ kind: "single", item: t2u });
	});

	test("does not reuse a display item when the underlying transcript item changes", () => {
		const original = assistantItemWithId(
			"a1",
			"t1",
			assistantMessage([{ type: "text", text: "before" }]),
		);
		const previous = groupItemsForDisplay([original]);
		const updated = assistantItemWithId(
			"a1",
			"t1",
			assistantMessage([{ type: "text", text: "after" }]),
		);

		const next = groupItemsForDisplay([updated], null, previous);

		expect(next[0]).not.toBe(previous[0]);
		expect(next[0]).toEqual({ kind: "single", item: updated });
	});
});

describe("reconcileTranscriptItems", () => {
	test("reuses unchanged transcript item objects", () => {
		const unchanged = userItem("u1", "t1");
		const added = userItem("u2", "t2");

		const result = reconcileTranscriptItems([unchanged], [unchanged, added]);

		expect(result[0]).toBe(unchanged);
		expect(result[1]).toBe(added);
	});

	test("does not reuse items with the same id but changed message object", () => {
		const original = assistantItemWithId(
			"a1",
			"t1",
			assistantMessage([{ type: "text", text: "before" }]),
		);
		const updated = assistantItemWithId(
			"a1",
			"t1",
			assistantMessage([{ type: "text", text: "after" }]),
		);

		const result = reconcileTranscriptItems([original], [updated]);

		expect(result[0]).toBe(updated);
	});

	test("does not reuse assistant items when tool results change", () => {
		const message = assistantMessage([toolCall()]);
		const original = assistantItemWithId("a1", "t1", message);
		const resultMessage = {
			role: "toolResult",
			toolCallId: "tool-1",
			toolName: "bash",
			content: [],
			isError: false,
			timestamp: 1,
		} satisfies ToolResultMessage;
		const updated = {
			...original,
			toolResults: new Map([["tool-1", resultMessage]]),
		} satisfies Extract<TranscriptItem, { kind: "assistant" }>;

		const result = reconcileTranscriptItems([original], [updated]);

		expect(result[0]).toBe(updated);
	});
});

describe("toolDisplayName", () => {
	test("returns the raw name for non-subagent tools", () => {
		const tc = {
			type: "toolCall",
			id: "a",
			name: "read",
			arguments: { path: "/x" },
		} as ToolCall;
		expect(toolDisplayName(tc)).toBe("read");
	});

	test("returns the agent name for subagent calls with an agent arg", () => {
		const tc = {
			type: "toolCall",
			id: "a",
			name: "subagent",
			arguments: { action: "run", agent: "summarizer", message: "hi" },
		} as ToolCall;
		expect(toolDisplayName(tc)).toBe("summarizer");
	});

	test("falls back to 'subagent' when agent arg is missing or empty", () => {
		const noAgent = {
			type: "toolCall",
			id: "a",
			name: "subagent",
			arguments: { action: "list_agents" },
		} as ToolCall;
		expect(toolDisplayName(noAgent)).toBe("subagent");

		const blankAgent = {
			type: "toolCall",
			id: "b",
			name: "subagent",
			arguments: { action: "run", agent: "  " },
		} as ToolCall;
		expect(toolDisplayName(blankAgent)).toBe("subagent");
	});
});

describe("formatToolArgs", () => {
	test("prefers message over action when keys are restricted", () => {
		const out = formatToolArgs(
			{ action: "run", agent: "summarizer", message: "summarize this" },
			{ keys: ["message", "action"] },
		);
		expect(out.trim()).toBe("summarize this");
	});

	test("falls back to action when no message is present", () => {
		const out = formatToolArgs(
			{ action: "list_agents" },
			{ keys: ["message", "action"] },
		);
		expect(out.trim()).toBe("list_agents");
	});

	test("returns empty when none of the requested keys match", () => {
		// Important for subagent: ensures we never fall through to `agent`
		// and duplicate the display label.
		const out = formatToolArgs(
			{ agent: "summarizer" },
			{ keys: ["message", "action"] },
		);
		expect(out).toBe("");
	});

	test("uses default keys (command, path, agent) when none provided", () => {
		expect(formatToolArgs({ path: "/x" }).trim()).toBe("/x");
		expect(formatToolArgs({ command: "ls -al" }).trim()).toBe("ls -al");
		expect(formatToolArgs({ agent: "summarizer" }).trim()).toBe("summarizer");
	});
});
