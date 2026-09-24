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
import { ARROW_RIGHT, ELLIPSIS, MIDDLE_DOT } from "../glyphs";

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
	if ("messageId" in message && typeof message.messageId === "string") {
		return message.messageId;
	}
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

export function groupMessagesIntoTurns(messages: KitAgentMessage[]): Turn[] {
	const turns: Turn[] = [];
	for (const message of messages) {
		const current = turns.at(-1);
		if (current?.id === message.turnId) {
			current.messages.push(message);
		} else {
			turns.push({ id: message.turnId, messages: [message] });
		}
	}
	return turns;
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

function sameToolResults(
	a: Map<string, unknown>,
	b: Map<string, unknown>,
): boolean {
	if (a.size !== b.size) return false;
	for (const [key, value] of a) {
		if (b.get(key) !== value) return false;
	}
	return true;
}

export function sameTranscriptItem(
	a: TranscriptItem,
	b: TranscriptItem,
): boolean {
	if (a.kind !== b.kind || a.id !== b.id || a.turnId !== b.turnId) return false;
	switch (a.kind) {
		case "user":
			return (
				b.kind === "user" && a.message === b.message && a.aborted === b.aborted
			);
		case "assistant":
			return (
				b.kind === "assistant" &&
				a.message === b.message &&
				a.aborted === b.aborted &&
				sameToolResults(a.toolResults, b.toolResults)
			);
		case "handoff-summary":
			return (
				b.kind === "handoff-summary" &&
				a.message === b.message &&
				a.aborted === b.aborted
			);
		case "bash":
			return b.kind === "bash" && a.message === b.message;
	}
}

export function reconcileTranscriptItems(
	prev: TranscriptItem[],
	next: TranscriptItem[],
): TranscriptItem[] {
	const previousById = new Map(prev.map((item) => [item.id, item]));
	return next.map((item) => {
		const previous = previousById.get(item.id);
		return previous && sameTranscriptItem(previous, item) ? previous : item;
	});
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

export function extractUserMarkdownSource(
	msg: UserMessage | UserMultipartMessage,
): string {
	const promptCommand = extractPromptCommandSynthetic(msg);
	if (promptCommand) {
		return `/${promptCommand.command}${promptCommand.args === undefined ? "" : ` ${promptCommand.args}`}`;
	}
	if (typeof msg.content === "string") return msg.content;
	return getUserParts(msg)
		.filter(
			(part): part is { type: "text"; text: string } =>
				part.type === "text" && "text" in part && typeof part.text === "string",
		)
		.map((part) => part.text)
		.join("");
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

export function extractAssistantMarkdownSource(msg: AssistantMessage): string {
	return msg.content
		.filter(
			(block): block is { type: "text"; text: string } =>
				block.type === "text" &&
				"text" in block &&
				typeof block.text === "string",
		)
		.map((block) => block.text)
		.join("");
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
const MAX_BASH_COMMAND_SUMMARY_LENGTH = 72;

type BashCommandPart = {
	command: string;
	separator?: "pipe" | "sequence";
};

function splitBashCommand(command: string): {
	parts: BashCommandPart[];
	complexSyntax: boolean;
} {
	const parts: BashCommandPart[] = [];
	let complexSyntax = false;
	let start = 0;
	let quote: "'" | '"' | "`" | null = null;
	let escaped = false;
	let nesting = 0;

	const push = (end: number, separator?: BashCommandPart["separator"]) => {
		const value = command.slice(start, end).trim();
		if (value) parts.push({ command: value, separator });
	};

	for (let index = 0; index < command.length; index += 1) {
		const char = command[index];
		if (escaped) {
			escaped = false;
			continue;
		}
		if (char === "\\" && quote !== "'") {
			escaped = true;
			continue;
		}
		if (quote) {
			if (char === quote) quote = null;
			continue;
		}
		if (char === "'" || char === '"' || char === "`") {
			quote = char;
			continue;
		}
		if (
			char === "#" &&
			(index === 0 || /[\s;&|()]/.test(command[index - 1] ?? ""))
		) {
			const newline = command.indexOf("\n", index);
			if (newline === -1) {
				push(index);
				start = command.length;
				break;
			}
			push(index, "sequence");
			start = newline + 1;
			index = newline;
			continue;
		}
		if (
			char === "<" &&
			command[index - 1] !== "<" &&
			command[index + 1] === "<" &&
			command[index + 2] !== "<"
		) {
			complexSyntax = true;
		}
		if (char === "(" || char === "{" || char === "[") {
			nesting += 1;
			continue;
		}
		if (char === ")" || char === "}" || char === "]") {
			nesting = Math.max(0, nesting - 1);
			continue;
		}
		if (nesting > 0) continue;

		const next = command[index + 1];
		if (char === "|" && next !== "|") {
			push(index, "pipe");
			index += next === "&" ? 1 : 0;
			start = index + 1;
			continue;
		}
		if (
			char === ";" ||
			char === "\n" ||
			(char === "&" && next === "&") ||
			(char === "&" &&
				next !== ">" &&
				command[index - 1] !== ">" &&
				command[index - 1] !== "<") ||
			(char === "|" && next === "|")
		) {
			push(index, "sequence");
			if (next === char) index += 1;
			start = index + 1;
		}
	}
	push(command.length);
	return { parts, complexSyntax };
}

function bashExecutable(command: string): string {
	const words = command.match(/(?:[^\s"']+|"[^"]*"|'[^']*')+/g) ?? [];
	let index = 0;
	while (
		index < words.length &&
		/^[A-Za-z_][A-Za-z0-9_]*=/.test(words[index])
	) {
		index += 1;
	}
	const executable = words[index]?.replace(/^['"]|['"]$/g, "") ?? "shell";
	return executable.split("/").pop() || "shell";
}

export type BashCommandPresentation = {
	text: string;
	summarized: boolean;
	commandCount: number;
};

/** Compact, deterministic presentation for bash tool-call headers. */
export function presentBashCommand(command: string): BashCommandPresentation {
	const trimmed = command.trim();
	const split = splitBashCommand(command);
	const { parts } = split;
	const lineCount = command.split("\n").filter((line) => line.trim()).length;
	const complexSyntax =
		split.complexSyntax ||
		parts.some((part) =>
			["for", "while", "until", "if", "case", "select", "function"].includes(
				bashExecutable(part.command),
			),
		);
	if (complexSyntax) {
		return {
			text:
				lineCount > 1
					? `shell script ${MIDDLE_DOT} ${lineCount} lines`
					: `shell command ${MIDDLE_DOT} ${Math.max(1, parts.length)} steps`,
			summarized: true,
			commandCount: parts.length,
		};
	}
	if (command.includes("\n")) {
		return {
			text:
				parts.length > 1
					? `shell script ${MIDDLE_DOT} ${parts.length} commands`
					: `shell script ${MIDDLE_DOT} ${lineCount} lines`,
			summarized: true,
			commandCount: parts.length,
		};
	}
	const normalizedSingleCommand = trimmed.replace(/\s+/g, " ");
	if (parts.length <= 1 && trimmed.length <= MAX_BASH_COMMAND_SUMMARY_LENGTH) {
		return {
			text: trimmed,
			summarized: false,
			commandCount: parts.length,
		};
	}
	if (parts.length <= 1) {
		return {
			text: `${normalizedSingleCommand.slice(0, MAX_BASH_COMMAND_SUMMARY_LENGTH - 1).trimEnd()}${ELLIPSIS}`,
			summarized: true,
			commandCount: parts.length,
		};
	}

	let text = bashExecutable(parts[0]?.command ?? "");
	for (let index = 1; index < parts.length; index += 1) {
		const separator = parts[index - 1]?.separator;
		text += ` ${separator === "pipe" ? ARROW_RIGHT : MIDDLE_DOT} ${bashExecutable(parts[index]?.command ?? "")}`;
	}
	if (text.length > MAX_BASH_COMMAND_SUMMARY_LENGTH) {
		text = `${text.slice(0, MAX_BASH_COMMAND_SUMMARY_LENGTH - 1).trimEnd()}${ELLIPSIS}`;
	}
	return { text, summarized: true, commandCount: parts.length };
}

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
 * Skill activation uses a human-readable action label. Other calls fall back
 * to their raw tool name.
 */
export function subagentToolAgentName(
	tc: ToolCall,
	args: Record<string, unknown> | undefined = tc.arguments,
): string | null {
	if (tc.name !== "subagent") return null;
	const agent = args?.agent;
	return typeof agent === "string" && agent.trim().length > 0
		? agent.trim()
		: null;
}

export function toolDisplayName(tc: ToolCall): string {
	if (tc.name === "activate_skill") return "activate skill";
	return subagentToolAgentName(tc) ?? tc.name;
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
 * The arg-preview keys to use for tool calls whose useful summary is not
 * covered by the defaults. Subagent calls hide `agent` because it is already
 * the display label; skill activation surfaces the selected skill name.
 */
export function toolArgKeys(tc: ToolCall): readonly string[] | undefined {
	if (tc.name === "subagent") return ["message", "action"];
	if (tc.name === "activate_skill") return ["name"];
	return undefined;
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
			kind: "assistant-prose";
			item: Extract<TranscriptItem, { kind: "assistant" }>;
	  }
	| {
			kind: "turn-work";
			items: TranscriptItem[];
			turnId: string;
	  };

function sameItems(a: TranscriptItem[], b: TranscriptItem[]): boolean {
	return a.length === b.length && a.every((item, index) => item === b[index]);
}

function reuseDisplayItem(
	item: DisplayItem,
	previousByKey: Map<string, DisplayItem> | null,
): DisplayItem {
	if (!previousByKey) return item;
	const key = displayItemKey(item);
	const previous = previousByKey.get(key);
	if (!previous || previous.kind !== item.kind) return item;
	if (item.kind === "single" || item.kind === "assistant-prose") {
		return previous.kind === item.kind && previous.item === item.item
			? previous
			: item;
	}
	return previous.kind === "turn-work" &&
		previous.turnId === item.turnId &&
		sameItems(previous.items, item.items)
		? previous
		: item;
}

export function displayItemKey(item: DisplayItem): string {
	if (item.kind === "single") return `single:${item.item.id}`;
	if (item.kind === "assistant-prose") return `assistant-prose:${item.item.id}`;
	return `turn-work:${item.turnId}:${item.items[0]?.id ?? "empty"}`;
}

function previousDisplayItemsByKey(
	previous?: DisplayItem[],
): Map<string, DisplayItem> | null {
	if (!previous || previous.length === 0) return null;
	const byKey = new Map<string, DisplayItem>();
	for (const item of previous) byKey.set(displayItemKey(item), item);
	return byKey;
}

/**
 * Groups items into stable display units within each turn:
 * - Every assistant prose message is projected into the main transcript.
 * - Tool-bearing and tool-only messages are grouped into adjacent work drawers.
 *   Their complete messages remain available in Activity, so associated prose
 *   may be repeated there for context.
 * - User messages, manual bash executions, and handoff summaries stay standalone.
 *
 * The grouping is deliberately independent of turn completion so transcript
 * rows and open activity drawers do not restructure when streaming ends.
 */
export function groupItemsForDisplay(
	items: TranscriptItem[],
	_inProgressTurnId?: string | null,
	previous?: DisplayItem[],
): DisplayItem[] {
	const result: DisplayItem[] = [];
	const previousByKey = previousDisplayItemsByKey(previous);
	const pushDisplayItem = (item: DisplayItem) => {
		result.push(reuseDisplayItem(item, previousByKey));
	};
	let i = 0;
	while (i < items.length) {
		const turnId = items[i].turnId;
		let j = i;
		while (j < items.length && items[j].turnId === turnId) j++;
		const turnItems = items.slice(i, j);
		i = j;

		let buffer: TranscriptItem[] = [];
		const flushBuffer = () => {
			if (buffer.length === 0) return;
			pushDisplayItem({ kind: "turn-work", items: buffer.slice(), turnId });
			buffer = [];
		};

		for (const item of turnItems) {
			if (
				item.kind === "user" ||
				item.kind === "bash" ||
				item.kind === "handoff-summary"
			) {
				flushBuffer();
				pushDisplayItem({ kind: "single", item });
				continue;
			}
			if (item.kind === "assistant" && assistantHasProse(item)) {
				flushBuffer();
				pushDisplayItem({ kind: "assistant-prose", item });
				if (extractAssistantParts(item.message).toolCalls.length > 0) {
					buffer.push(item);
				}
				continue;
			}
			buffer.push(item);
		}
		flushBuffer();
	}

	return result;
}

export function findTurnWorkItems(
	items: TranscriptItem[],
	turnId: string,
	inProgressTurnId?: string | null,
	anchorItemId?: string,
): TranscriptItem[] {
	const candidates = groupItemsForDisplay(items, inProgressTurnId).filter(
		(item): item is Extract<DisplayItem, { kind: "turn-work" }> =>
			item.kind === "turn-work" && item.turnId === turnId,
	);
	const displayItem = anchorItemId
		? candidates.find((item) =>
				item.items.some((entry) => entry.id === anchorItemId),
			)
		: candidates[0];
	if (displayItem?.kind === "turn-work") return displayItem.items;
	if (anchorItemId) {
		const anchoredItem = items.find((item) => item.id === anchorItemId);
		if (anchoredItem) return [anchoredItem];
	}
	return [];
}
