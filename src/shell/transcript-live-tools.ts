import { extractAssistantParts, type TranscriptItem } from "./transcript/turns";

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

export function reconcileLiveTools(
	prev: LiveToolExecutionMap,
	items: TranscriptItem[],
): LiveToolExecutionMap {
	const toolCallIdsByTurn = new Map<string, Set<string>>();
	const committedIdsByTurn = new Map<string, Set<string>>();

	for (const item of items) {
		if (item.kind !== "assistant") continue;
		const toolCallIds = toolCallIdsByTurn.get(item.turnId) ?? new Set<string>();
		for (const toolCall of extractAssistantParts(item.message).toolCalls) {
			toolCallIds.add(toolCall.id);
		}
		toolCallIdsByTurn.set(item.turnId, toolCallIds);

		const committedIds =
			committedIdsByTurn.get(item.turnId) ?? new Set<string>();
		for (const toolCallId of item.toolResults.keys()) {
			committedIds.add(toolCallId);
		}
		committedIdsByTurn.set(item.turnId, committedIds);
	}

	const next: LiveToolExecutionMap = {};
	for (const [turnId, existing] of Object.entries(prev)) {
		const toolCallIds = toolCallIdsByTurn.get(turnId);
		if (!toolCallIds) continue;
		const committedIds = committedIdsByTurn.get(turnId) ?? new Set<string>();
		const keptEntries = Object.entries(existing).filter(([toolCallId]) => {
			if (!toolCallIds.has(toolCallId)) return false;
			if (committedIds.has(toolCallId)) return false;
			return true;
		});
		if (keptEntries.length > 0) {
			next[turnId] = Object.fromEntries(keptEntries);
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
