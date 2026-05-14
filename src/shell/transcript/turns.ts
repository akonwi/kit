import type {
	AgentMessage,
	CustomAgentMessages,
} from "@earendil-works/pi-agent-core";
import type {
	AssistantMessage,
	ToolCall,
	ToolResultMessage,
	UserMessage,
} from "@earendil-works/pi-ai";
import type { MessagePart, UserMultipartMessage } from "../../messages/parts";
import type { KitAgentMessage, Turn } from "../../session/types";

export type BashExecutionMessage = CustomAgentMessages["bashExecution"];

export type HandoffSummaryMessage = AssistantMessage & {
	timestamp: number;
	synthetic?: {
		kind: "handoff-summary";
		sourceSessionName?: string;
	};
};

export type TranscriptItem =
	| {
			kind: "user";
			id: string;
			turnId: string;
			message: UserMessage | UserMultipartMessage;
			aborted: boolean;
	  }
	| {
			kind: "assistant";
			id: string;
			turnId: string;
			message: AssistantMessage;
			toolResults: Map<string, ToolResultMessage>;
			aborted: boolean;
	  }
	| {
			kind: "handoff-summary";
			id: string;
			turnId: string;
			message: HandoffSummaryMessage;
			aborted: boolean;
	  }
	| {
			kind: "bash";
			id: string;
			turnId: string;
			message: BashExecutionMessage;
	  };

function buildToolResults(turn: Turn): Map<string, ToolResultMessage> {
	const toolResults = new Map<string, ToolResultMessage>();
	for (const msg of turn.messages) {
		if (msg.role === "toolResult") {
			toolResults.set(msg.toolCallId, msg as ToolResultMessage);
		}
	}
	return toolResults;
}

function isTurnAborted(turn: Turn): boolean {
	return turn.messages.some(
		(msg) =>
			msg.role === "assistant" &&
			(msg as AssistantMessage).stopReason === "aborted",
	);
}

function buildTranscriptItemId(
	message: AgentMessage,
	turnId: string,
	index: number,
): string {
	if ("id" in message && typeof message.id === "string") {
		return `${turnId}:${message.role}:${message.id}`;
	}
	if ("responseId" in message && typeof message.responseId === "string") {
		return `${turnId}:${message.role}:${message.responseId}`;
	}
	if ("timestamp" in message && typeof message.timestamp === "number") {
		return `${turnId}:${message.role}:${message.timestamp}:${index}`;
	}
	return `${turnId}:${message.role}:${index}`;
}

export function buildUserTranscriptItem(
	message: Extract<KitAgentMessage, { role: "user" }>,
	aborted = false,
): Extract<TranscriptItem, { kind: "user" }> {
	return {
		kind: "user",
		id: buildTranscriptItemId(message, message.turnId, 0),
		turnId: message.turnId,
		message: message as UserMessage | UserMultipartMessage,
		aborted,
	};
}

export function buildAssistantTranscriptItem(
	turn: Turn,
	message: Extract<KitAgentMessage, { role: "assistant" }>,
	toolResults = buildToolResults(turn),
	aborted = isTurnAborted(turn),
): Extract<TranscriptItem, { kind: "assistant" | "handoff-summary" }> {
	const base = {
		id: buildTranscriptItemId(message, turn.id, turn.messages.indexOf(message)),
		turnId: turn.id,
		aborted,
	} as const;
	if (isHandoffSummaryMessage(message)) {
		return {
			kind: "handoff-summary",
			...base,
			message,
		};
	}
	return {
		kind: "assistant",
		...base,
		message: message as AssistantMessage,
		toolResults,
	};
}

export function buildBashTranscriptItem(
	message: Extract<KitAgentMessage, { role: "bashExecution" }>,
): Extract<TranscriptItem, { kind: "bash" }> {
	return {
		kind: "bash",
		id: buildTranscriptItemId(message, message.turnId, 0),
		turnId: message.turnId,
		message: message as BashExecutionMessage,
	};
}

export function flattenTurnsToTranscriptItems(turns: Turn[]): TranscriptItem[] {
	const items: TranscriptItem[] = [];
	for (const turn of turns) {
		const toolResults = buildToolResults(turn);
		const aborted = isTurnAborted(turn);
		for (const message of turn.messages) {
			switch (message.role) {
				case "user":
					items.push(buildUserTranscriptItem(message, aborted));
					break;
				case "assistant":
					items.push(
						buildAssistantTranscriptItem(turn, message, toolResults, aborted),
					);
					break;
				case "bashExecution":
					items.push(buildBashTranscriptItem(message));
					break;
				default:
					break;
			}
		}
	}
	return items;
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

export type PromptCommandSynthetic = {
	kind: "prompt-command";
	command: string;
	args?: string;
};

export function extractPromptCommandSynthetic(
	msg: UserMessage | UserMultipartMessage,
): PromptCommandSynthetic | null {
	const synthetic = (msg as { synthetic?: unknown }).synthetic;
	if (!synthetic || typeof synthetic !== "object") return null;
	const candidate = synthetic as {
		kind?: unknown;
		command?: unknown;
		args?: unknown;
	};
	if (candidate.kind !== "prompt-command") return null;
	if (
		typeof candidate.command !== "string" ||
		candidate.command.trim().length === 0
	) {
		return null;
	}
	if (candidate.args !== undefined && typeof candidate.args !== "string") {
		return null;
	}
	return {
		kind: "prompt-command",
		command: candidate.command,
		...(typeof candidate.args === "string" && candidate.args.trim().length > 0
			? { args: candidate.args }
			: {}),
	};
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

const MAX_TOOL_ARG_SUMMARY_LENGTH = 80;

function summarizeToolArg(value: string): string {
	const singleLine = value.replace(/\s+/g, " ").trim();
	if (singleLine.length <= MAX_TOOL_ARG_SUMMARY_LENGTH) {
		return singleLine;
	}
	return `${singleLine.slice(0, MAX_TOOL_ARG_SUMMARY_LENGTH - 3)}...`;
}

export function formatToolArgs(args?: Record<string, unknown>): string {
	if (!args) return "";
	if ("command" in args && typeof args.command === "string") {
		return ` ${summarizeToolArg(args.command)}`;
	}
	if ("path" in args && typeof args.path === "string") {
		return ` ${summarizeToolArg(args.path)}`;
	}
	if ("agent" in args && typeof args.agent === "string") {
		return ` ${summarizeToolArg(args.agent)}`;
	}
	return "";
}

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
