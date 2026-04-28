import type {
	AgentMessage,
	CustomAgentMessages,
} from "@mariozechner/pi-agent-core";
import type {
	AssistantMessage,
	ToolCall,
	ToolResultMessage,
	UserMessage,
} from "@mariozechner/pi-ai";
import type { MessagePart, UserMultipartMessage } from "../../messages/parts";
import type { KitAgentMessage, Turn } from "../../session/types";

export type BashExecutionMessage = CustomAgentMessages["bashExecution"];

export type TranscriptTurn = {
	id: string;
	user: (UserMessage | UserMultipartMessage) | null;
	entries: AgentMessage[];
	toolResults: Map<string, ToolResultMessage>;
	aborted: boolean;
};

export function toTranscriptTurn(turn: Turn): TranscriptTurn {
	let user: (UserMessage | UserMultipartMessage) | null = null;
	const entries: AgentMessage[] = [];
	const toolResults = new Map<string, ToolResultMessage>();
	let aborted = false;

	for (const msg of turn.messages) {
		if (!("role" in msg)) continue;
		if (msg.role === "user" && user === null) {
			user = msg as UserMessage | UserMultipartMessage;
			continue;
		}

		entries.push(msg);

		if (msg.role === "toolResult") {
			toolResults.set(msg.toolCallId, msg as ToolResultMessage);
		}

		if (
			msg.role === "assistant" &&
			(msg as AssistantMessage).stopReason === "aborted"
		) {
			aborted = true;
		}
	}

	return { id: turn.id, user, entries, toolResults, aborted };
}

export function getUserParts(
	msg: UserMessage | UserMultipartMessage,
): MessagePart[] {
	if (typeof msg.content === "string") {
		return [{ type: "text", text: msg.content }];
	}
	return msg.content as MessagePart[];
}

export function extractUserText(
	msg: UserMessage | UserMultipartMessage,
): string {
	return getUserParts(msg)
		.filter(
			(part): part is { type: "text"; text: string } =>
				part.type === "text" && "text" in part && typeof part.text === "string",
		)
		.map((part) => part.text)
		.join("\n");
}

export function extractUserCustomParts(
	msg: UserMessage | UserMultipartMessage,
): MessagePart[] {
	return getUserParts(msg).filter((part) => part.type !== "text");
}

export function extractAssistantParts(msg: AssistantMessage): {
	text: string;
	toolCalls: ToolCall[];
} {
	const textParts: string[] = [];
	const toolCalls: ToolCall[] = [];
	for (const block of msg.content) {
		if (block.type === "text" && "text" in block && block.text) {
			textParts.push(block.text);
		} else if (block.type === "toolCall" && "name" in block) {
			toolCalls.push(block as ToolCall);
		}
	}
	return { text: textParts.join("\n\n"), toolCalls };
}

export function extractToolResultLines(msg: ToolResultMessage): string[] {
	const lines: string[] = [];
	for (const block of msg.content) {
		if (block.type === "text" && "text" in block && block.text) {
			lines.push(...block.text.split("\n"));
		}
	}
	return lines;
}

export function formatToolArgs(args?: Record<string, unknown>): string {
	if (!args) return "";
	if ("command" in args && typeof args.command === "string") {
		return ` ${args.command}`;
	}
	if ("path" in args && typeof args.path === "string") return ` ${args.path}`;
	return "";
}

export type HandoffSummaryMessage = AssistantMessage & {
	timestamp: number;
	synthetic?: {
		kind: "handoff-summary";
		sourceSessionName?: string;
	};
};

export function isHandoffSummaryMessage(
	message: AgentMessage,
): message is HandoffSummaryMessage {
	return (
		message.role === "assistant" &&
		(message as KitAgentMessage).synthetic?.kind === "handoff-summary"
	);
}

export function isAssistantError(msg: AssistantMessage): boolean {
	return msg.stopReason === "error" && !!msg.errorMessage;
}
