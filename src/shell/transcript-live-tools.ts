import type { Turn } from "../session/types";

export type LiveToolState = "started" | "updated" | "ended";

export type LiveToolExecution = {
	turnId: string;
	toolCallId: string;
	toolName: string;
	args: unknown;
	partialResult: unknown | null;
	result: unknown | null;
	isError: boolean | null;
	state: LiveToolState;
};

export type LiveToolExecutionMap = Record<
	string,
	Record<string, LiveToolExecution>
>;

export type LiveToolsForTurn = Record<string, LiveToolExecution>;

function assistantToolCallIds(turn: Turn): Set<string> {
	const ids = new Set<string>();
	for (const message of turn.messages) {
		if (!("role" in message) || message.role !== "assistant") continue;
		for (const block of message.content) {
			if (block.type === "toolCall" && "id" in block) {
				ids.add(block.id);
			}
		}
	}
	return ids;
}

function committedToolResultIds(turn: Turn): Set<string> {
	const ids = new Set<string>();
	for (const message of turn.messages) {
		if (!("role" in message) || message.role !== "toolResult") continue;
		ids.add(message.toolCallId);
	}
	return ids;
}

export function reconcileLiveTools(
	prev: LiveToolExecutionMap,
	turns: Turn[],
): LiveToolExecutionMap {
	const next: LiveToolExecutionMap = {};
	for (const turn of turns) {
		const existing = prev[turn.id];
		if (!existing) continue;
		const toolCallIds = assistantToolCallIds(turn);
		const committedIds = committedToolResultIds(turn);
		const keptEntries = Object.entries(existing).filter(([toolCallId]) => {
			if (!toolCallIds.has(toolCallId)) return false;
			if (committedIds.has(toolCallId)) return false;
			return true;
		});
		if (keptEntries.length > 0) {
			next[turn.id] = Object.fromEntries(keptEntries);
		}
	}
	return next;
}

export function upsertLiveTool(
	prev: LiveToolExecutionMap,
	nextTool: LiveToolExecution,
): LiveToolExecutionMap {
	return {
		...prev,
		[nextTool.turnId]: {
			...(prev[nextTool.turnId] ?? {}),
			[nextTool.toolCallId]: nextTool,
		},
	};
}

export function extractToolProgressLines(result: unknown): string[] {
	if (!result || typeof result !== "object") return [];
	if (!("content" in result) || !Array.isArray(result.content)) return [];
	const lines: string[] = [];
	for (const block of result.content) {
		if (
			block &&
			typeof block === "object" &&
			"type" in block &&
			block.type === "text" &&
			"text" in block &&
			typeof block.text === "string" &&
			block.text.length > 0
		) {
			lines.push(...block.text.split("\n"));
		}
	}
	return lines;
}
