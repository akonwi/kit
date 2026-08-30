import { isRecord, type ToolActivity } from "@akonwi/kit-session-client";
import type { KitAgentMessage } from "../session/types";
import {
	extractAssistantParts,
	flattenTurnsToTranscriptItems,
	groupMessagesIntoTurns,
	type TranscriptItem,
} from "../shell/transcript/turns";

function isOptionalString(value: unknown): boolean {
	return value === undefined || typeof value === "string";
}

function isUserContentPart(value: unknown): boolean {
	if (!isRecord(value) || typeof value.type !== "string") return false;
	switch (value.type) {
		case "text":
			return typeof value.text === "string";
		case "image":
			return (
				typeof value.data === "string" &&
				typeof value.mimeType === "string" &&
				isOptionalString(value.filename) &&
				isOptionalString(value.attachmentId)
			);
		case "code-review":
			return (
				isRecord(value.review) &&
				Array.isArray(value.review.files) &&
				value.review.files.every(
					(file) =>
						isRecord(file) &&
						typeof file.path === "string" &&
						typeof file.fileComment === "string" &&
						Array.isArray(file.ranges),
				)
			);
		default:
			return false;
	}
}

function isAssistantContentPart(value: unknown): boolean {
	if (!isRecord(value) || typeof value.type !== "string") return false;
	switch (value.type) {
		case "text":
			return typeof value.text === "string";
		case "thinking":
			return typeof value.thinking === "string";
		case "toolCall":
			return (
				typeof value.id === "string" &&
				typeof value.name === "string" &&
				(value.arguments === undefined || isRecord(value.arguments))
			);
		default:
			return false;
	}
}

function hasValidSyntheticMetadata(value: Record<string, unknown>): boolean {
	return (
		value.synthetic === undefined ||
		(isRecord(value.synthetic) &&
			isOptionalString(value.synthetic.sourceSessionName))
	);
}

function isProtocolTranscriptMessage(
	value: Record<string, unknown>,
): value is Record<string, unknown> & {
	messageId: string;
	turnId: string;
} {
	if (typeof value.messageId !== "string" || typeof value.turnId !== "string") {
		return false;
	}
	switch (value.role) {
		case "user":
			return (
				typeof value.content === "string" ||
				(Array.isArray(value.content) && value.content.every(isUserContentPart))
			);
		case "assistant":
			return (
				Array.isArray(value.content) &&
				value.content.every(isAssistantContentPart) &&
				isOptionalString(value.errorMessage) &&
				hasValidSyntheticMetadata(value)
			);
		case "toolResult":
			return (
				typeof value.toolCallId === "string" &&
				typeof value.toolName === "string" &&
				Array.isArray(value.content) &&
				value.content.every(isRecord)
			);
		case "bashExecution":
			return (
				typeof value.command === "string" &&
				(value.output === undefined || typeof value.output === "string") &&
				(value.exitCode === undefined || typeof value.exitCode === "number") &&
				(value.cancelled === undefined || typeof value.cancelled === "boolean")
			);
		default:
			return false;
	}
}

function placeholderMessage(
	message: Record<string, unknown>,
	text: string,
): KitAgentMessage {
	const role = message.role === "user" ? "user" : "assistant";
	const identity = {
		messageId:
			typeof message.messageId === "string"
				? message.messageId
				: `unavailable:${String(message.messageIndex ?? "unknown")}`,
		turnId: typeof message.turnId === "string" ? message.turnId : "unavailable",
		timestamp: 0,
	};
	if (role === "user") {
		return { ...identity, role, content: text } as KitAgentMessage;
	}
	return {
		...identity,
		role,
		content: [{ type: "text", text }],
		stopReason: "stop",
	} as KitAgentMessage;
}

export function liveToolsForTranscriptItems(
	selectedItems: TranscriptItem[],
	allItems: TranscriptItem[],
	tools: ToolActivity[],
	activeTurnId: string | null,
): ToolActivity[] {
	const turnId = selectedItems[0]?.turnId;
	if (!turnId) return [];
	const selectedItemIds = new Set(selectedItems.map((item) => item.id));
	const selectedToolCallIds = new Set(
		selectedItems.flatMap((item) =>
			item.kind === "assistant"
				? extractAssistantParts(item.message).toolCalls.map(
						(toolCall) => toolCall.id,
					)
				: [],
		),
	);
	const turnItems = allItems.filter((item) => item.turnId === turnId);
	const allTurnToolCallIds = new Set(
		turnItems.flatMap((item) =>
			item.kind === "assistant"
				? extractAssistantParts(item.message).toolCalls.map(
						(toolCall) => toolCall.id,
					)
				: [],
		),
	);
	const latestAssistant = turnItems.findLast(
		(item) => item.kind === "assistant",
	);
	const ownsUnassociatedLiveTools =
		activeTurnId === turnId &&
		latestAssistant !== undefined &&
		selectedItemIds.has(latestAssistant.id);
	return tools.filter(
		(tool) =>
			tool.turnId === turnId &&
			(selectedToolCallIds.has(tool.id) ||
				(!allTurnToolCallIds.has(tool.id) && ownsUnassociatedLiveTools)),
	);
}

export function protocolMessagesToTranscriptItems(
	messages: unknown[],
): ReturnType<typeof flattenTurnsToTranscriptItems> {
	const transcriptMessages: KitAgentMessage[] = [];
	for (const value of messages) {
		if (!isRecord(value)) continue;
		if (value.type === "message_reference") {
			transcriptMessages.push(placeholderMessage(value, "Loading message…"));
			continue;
		}
		if (value.type === "message_unavailable") {
			transcriptMessages.push(
				placeholderMessage(value, "This message is too large to display."),
			);
			continue;
		}
		if (!isProtocolTranscriptMessage(value)) continue;
		transcriptMessages.push(value as unknown as KitAgentMessage);
	}
	return flattenTurnsToTranscriptItems(
		groupMessagesIntoTurns(transcriptMessages),
	);
}
