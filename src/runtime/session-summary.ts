import { randomUUID } from "node:crypto";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { type Api, completeSimple, type Model } from "@mariozechner/pi-ai";
import { messagePartToPromptText } from "../messages/parts";
import type { KitAgentMessage, SyntheticSummaryKind } from "../session/types";

const MAX_USER_MESSAGE_CHARS = 4_000;
const MAX_ASSISTANT_MESSAGE_CHARS = 4_000;
const MAX_TOOL_CALL_ARGS_CHARS = 200;

function truncateText(text: string, maxChars: number): string {
	const normalized = text.trim();
	if (normalized.length <= maxChars) return normalized;
	return `${normalized.slice(0, maxChars)}… [truncated ${normalized.length - maxChars} chars]`;
}

function summarizeToolResultText(text: string): string {
	const trimmed = text.trim();
	if (!trimmed) return "[empty output omitted]";
	const lines = trimmed.split(/\r?\n/);
	return `[tool output omitted: ${lines.length} lines, ${trimmed.length} chars]`;
}

function summarizeBashExecution(
	message: Extract<AgentMessage, { role: "bashExecution" }>,
): string {
	const output = message.output.trim();
	const exit =
		typeof message.exitCode === "number" ? String(message.exitCode) : "unknown";
	const outputSummary = output
		? `[bash output omitted: ${output.split(/\r?\n/).length} lines, ${output.length} chars]`
		: "[no output]";
	return `[Bash execution]\nCommand: ${message.command}\nExit code: ${exit}${message.cancelled ? " (cancelled)" : ""}\n${outputSummary}`;
}

export function serializeConversation(messages: AgentMessage[]): string {
	const parts: string[] = [];

	for (const message of messages) {
		if (message.role === "user") {
			const content =
				typeof message.content === "string"
					? message.content
					: message.content
							.map((block) => messagePartToPromptText(block as never))
							.filter(Boolean)
							.join("\n");
			if (content) {
				parts.push(`[User]\n${truncateText(content, MAX_USER_MESSAGE_CHARS)}`);
			}
			continue;
		}

		if (message.role === "assistant") {
			const textParts: string[] = [];
			const toolCalls: string[] = [];

			for (const block of message.content) {
				if (block.type === "text") textParts.push(block.text);
				if (block.type === "toolCall") {
					const args = truncateText(
						JSON.stringify(block.arguments, null, 0),
						MAX_TOOL_CALL_ARGS_CHARS,
					);
					toolCalls.push(`${block.name}(${args})`);
				}
			}

			if (textParts.length > 0) {
				parts.push(
					`[Assistant]\n${truncateText(textParts.join("\n"), MAX_ASSISTANT_MESSAGE_CHARS)}`,
				);
			}
			if (toolCalls.length > 0) {
				parts.push(`[Assistant tool calls]\n${toolCalls.join("\n")}`);
			}
			continue;
		}

		if (message.role === "toolResult") {
			const content = message.content
				.filter((block) => block.type === "text")
				.map((block) => block.text)
				.join("\n");
			parts.push(`[Tool result summary]\n${summarizeToolResultText(content)}`);
			continue;
		}

		if (message.role === "bashExecution") {
			if (!message.excludeFromContext) {
				parts.push(summarizeBashExecution(message));
			}
		}
	}

	return parts.join("\n\n");
}

export async function createSyntheticSummaryMessage(options: {
	messages: AgentMessage[];
	model: Model<Api>;
	apiKey: string;
	systemPrompt: string;
	userPrompt: string;
	kind: SyntheticSummaryKind;
	sourceSessionName?: string;
	signal?: AbortSignal;
}): Promise<Extract<KitAgentMessage, { role: "assistant" }>> {
	const {
		messages,
		model,
		apiKey,
		systemPrompt,
		userPrompt,
		kind,
		sourceSessionName,
		signal,
	} = options;
	const promptText = `<conversation>\n${serializeConversation(messages)}\n</conversation>\n\n${userPrompt}`;
	const response = await completeSimple(
		model,
		{
			systemPrompt,
			messages: [
				{
					role: "user",
					content: promptText,
					timestamp: Date.now(),
				},
			],
		},
		model.reasoning
			? { apiKey, signal, maxTokens: 2048, reasoning: "high" }
			: { apiKey, signal, maxTokens: 2048 },
	);

	if (response.stopReason === "error" || response.stopReason === "aborted") {
		throw new Error(response.errorMessage || "Session summarization failed.");
	}

	const turnId = randomUUID();
	return {
		...response,
		turnId,
		synthetic: {
			kind,
			...(sourceSessionName ? { sourceSessionName } : {}),
		},
	} as Extract<KitAgentMessage, { role: "assistant" }>;
}
