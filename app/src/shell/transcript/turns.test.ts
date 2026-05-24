import { describe, expect, test } from "bun:test";
import type { AssistantMessage, ToolCall } from "../../runtime/agent";
import { filterTranscriptItemsForDisplay, type TranscriptItem } from "./turns";

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

function toolCall(): ToolCall {
	return {
		type: "toolCall",
		id: "tool-1",
		name: "bash",
		arguments: { command: "echo hidden" },
	} as ToolCall;
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
