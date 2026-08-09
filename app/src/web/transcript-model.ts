import type { KitAgentMessage } from "../session/types";
import {
	type DisplayItem,
	flattenTurnsToTranscriptItems,
	groupItemsForDisplay,
	groupMessagesIntoTurns,
} from "../shell/transcript/turns";
import { isRecord } from "./client-state";

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

export function protocolMessagesToDisplayItems(
	messages: unknown[],
	activeTurnId: string | null,
	previous?: DisplayItem[],
): DisplayItem[] {
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
	const items = flattenTurnsToTranscriptItems(
		groupMessagesIntoTurns(transcriptMessages),
	);
	return groupItemsForDisplay(items, activeTurnId, previous);
}
