import type { MessagePart, UserMultipartMessage } from "../../messages/parts";
import type {
	AgentMessage,
	AssistantMessage,
	CustomAgentMessages,
	ToolCall,
	ToolResultMessage,
	UserMessage,
} from "../../runtime/agent";
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

function summarizeToolArg(value: string, full: boolean): string {
	const singleLine = value.replace(/\s+/g, " ").trim();
	if (full || singleLine.length <= MAX_TOOL_ARG_SUMMARY_LENGTH) {
		return singleLine;
	}
	return `${singleLine.slice(0, MAX_TOOL_ARG_SUMMARY_LENGTH - 3)}...`;
}

/**
 * Display label for a tool call.
 *
 * Subagent calls show the agent name when available so the transcript
 * reads as e.g. `summarizer` instead of the generic `subagent` label.
 * Falls back to the raw tool name for `list_agents` (no agent arg) and
 * any non-subagent tool.
 */
export function toolDisplayName(tc: ToolCall): string {
	if (tc.name === "subagent") {
		const agent = tc.arguments?.agent;
		if (typeof agent === "string" && agent.trim().length > 0) {
			return agent;
		}
	}
	return tc.name;
}

const DEFAULT_TOOL_ARG_KEYS = ["command", "path", "agent"] as const;

/**
 * Returns the first string-valued arg matching the configured keys.
 *
 * Callers can override `keys` when the default preview keys would
 * duplicate information shown elsewhere (e.g. subagent calls promote
 * `agent` to the display label, so they pass `["message", "action"]`).
 */
export function formatToolArgs(
	args?: Record<string, unknown>,
	options: { full?: boolean; keys?: readonly string[] } = {},
): string {
	if (!args) return "";
	const full = options.full ?? false;
	const keys = options.keys ?? DEFAULT_TOOL_ARG_KEYS;
	for (const key of keys) {
		const value = args[key];
		if (typeof value === "string") {
			return ` ${summarizeToolArg(value, full)}`;
		}
	}
	return "";
}

/**
 * The arg-preview keys to use for a tool call. Subagent calls hide the
 * `agent` arg because it's already the display label.
 */
export function toolArgKeys(tc: ToolCall): readonly string[] | undefined {
	return tc.name === "subagent" ? ["message", "action"] : undefined;
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

function assistantHasProse(
	item: Extract<TranscriptItem, { kind: "assistant" }>,
): boolean {
	if (isAssistantError(item.message)) return true;
	return extractAssistantParts(item.message).text.trim().length > 0;
}

/**
 * A display-level item: either a single transcript item or a group of
 * intermediate turn items folded into one drawer.
 */
export type DisplayItem =
	| { kind: "single"; item: TranscriptItem }
	| {
			kind: "turn-work";
			items: TranscriptItem[];
			turnId: string;
	  };

/**
 * Groups items into display units. Within each turn:
 * - If the turn has 0 or 1 assistant message: emit all items as singles.
 * - Otherwise, fold intermediate items into a single "turn-work" drawer.
 *   The user message and the "final" assistant message (the last one with
 *   prose) render as singles; everything in between collapses.
 *   If no assistant message in the turn has prose, all assistant items
 *   collapse into the turn-work drawer.
 *
 * Manual bash execution items always render as singles, even if they share
 * a turn id with assistant tool work. They are user-triggered transcript
 * events, not assistant turn-work.
 *
 * When `inProgressTurnId` matches a turn, that turn is treated as in flight:
 * no "final" prose item is extracted, and every non-user, non-bash item
 * collapses into a single growing turn-work drawer (even if there is
 * currently only one such item). This keeps the visible transcript stable
 * while the assistant streams multiple intermediate messages.
 */
export function groupItemsForDisplay(
	items: TranscriptItem[],
	inProgressTurnId?: string | null,
): DisplayItem[] {
	const result: DisplayItem[] = [];
	let i = 0;
	while (i < items.length) {
		const turnId = items[i].turnId;
		let j = i;
		while (j < items.length && items[j].turnId === turnId) j++;
		const turnItems = items.slice(i, j);
		i = j;

		const isInProgress = !!inProgressTurnId && inProgressTurnId === turnId;

		let assistantCount = 0;
		for (const item of turnItems) {
			if (item.kind === "assistant") assistantCount++;
		}

		if (assistantCount <= 1 && !isInProgress) {
			for (const item of turnItems) {
				result.push({ kind: "single", item });
			}
			continue;
		}

		// Multiple assistant messages: identify the "final" item — the last
		// assistant message that has prose. If none, no item is treated as final
		// and everything intermediate collapses. In-progress turns never extract
		// a final, since more messages may still arrive.
		let finalIdx = -1;
		if (!isInProgress) {
			for (let k = turnItems.length - 1; k >= 0; k--) {
				const item = turnItems[k];
				if (item.kind === "assistant" && assistantHasProse(item)) {
					finalIdx = k;
					break;
				}
			}
		}

		let buffer: TranscriptItem[] = [];
		const flushBuffer = () => {
			if (buffer.length === 0) return;
			if (buffer.length === 1 && !isInProgress) {
				result.push({ kind: "single", item: buffer[0] });
			} else {
				result.push({ kind: "turn-work", items: buffer.slice(), turnId });
			}
			buffer = [];
		};

		for (let k = 0; k < turnItems.length; k++) {
			const item = turnItems[k];
			if (item.kind === "user" || item.kind === "bash" || k === finalIdx) {
				flushBuffer();
				result.push({ kind: "single", item });
			} else {
				buffer.push(item);
			}
		}
		flushBuffer();
	}

	return result;
}
