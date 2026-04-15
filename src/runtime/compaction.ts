import { randomUUID } from "node:crypto";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { type Api, completeSimple, type Model } from "@mariozechner/pi-ai";
import type { Session } from "../session";
import type { KitAgentMessage, Turn } from "../session/types";
import { estimateTokens } from "./context-usage";

const DEFAULT_KEEP_RECENT_TOKENS = 20_000;
const AUTO_COMPACT_PERCENT = 90;

const SUMMARIZATION_SYSTEM_PROMPT = `You are a context summarization assistant.
Do not continue the conversation.
Do not answer the user's requests.
Only produce a compact, implementation-focused summary of prior context.`;

const SUMMARIZATION_PROMPT = `Summarize the earlier portion of this session for future continuation.

Use this exact structure:

## Goal
[overall task and current desired outcome]

## Progress so far
- [important implementation work already completed]

## Key decisions
- [important architectural or product decisions that must be preserved]

## Open issues / risks
- [remaining bugs, gaps, or edge cases]

## Context to preserve
- [specific details the assistant should know before continuing]

Be concise but specific. Focus on information needed to continue the work accurately.`;

export type CompactionResult = {
	turns: Turn[];
	compactedTurnCount: number;
	keptTurnCount: number;
	tokensBefore: number;
};

function turnTokens(turn: Turn): number {
	return turn.messages.reduce(
		(sum, message) => sum + estimateTokens(message),
		0,
	);
}

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

function summarizeBashExecution(message: Extract<AgentMessage, { role: "bashExecution" }>): string {
	const output = message.output.trim();
	const exit = typeof message.exitCode === "number" ? String(message.exitCode) : "unknown";
	const outputSummary = output
		? `[bash output omitted: ${output.split(/\r?\n/).length} lines, ${output.length} chars]`
		: "[no output]";
	return `[Bash execution]\nCommand: ${message.command}\nExit code: ${exit}${message.cancelled ? " (cancelled)" : ""}\n${outputSummary}`;
}

function serializeConversation(messages: AgentMessage[]): string {
	const parts: string[] = [];

	for (const message of messages) {
		if (message.role === "user") {
			const content =
				typeof message.content === "string"
					? message.content
					: message.content
							.filter((block) => block.type === "text")
							.map((block) => block.text)
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

async function generateSummary(
	messages: AgentMessage[],
	model: Model<Api>,
	apiKey: string,
	signal?: AbortSignal,
): Promise<KitAgentMessage> {
	const promptText = `<conversation>\n${serializeConversation(messages)}\n</conversation>\n\n${SUMMARIZATION_PROMPT}`;
	const response = await completeSimple(
		model,
		{
			systemPrompt: SUMMARIZATION_SYSTEM_PROMPT,
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
		throw new Error(
			response.errorMessage || "Compaction summarization failed.",
		);
	}

	const turnId = randomUUID();
	return {
		...response,
		turnId,
		synthetic: { kind: "compaction-summary" },
	};
}

export function shouldAutoCompact(percent: number | null | undefined): boolean {
	return percent != null && percent >= AUTO_COMPACT_PERCENT;
}

export async function compactSessionTurns(options: {
	session: Session;
	model: Model<Api>;
	apiKey: string;
	signal?: AbortSignal;
}): Promise<CompactionResult | null> {
	const { session, model, apiKey, signal } = options;
	const turns = session.turns;
	if (turns.length < 2) return null;

	const keepRecentTokens = Math.min(
		DEFAULT_KEEP_RECENT_TOKENS,
		Math.floor(model.contextWindow * 0.5),
	);

	let keptStartIndex = turns.length - 1;
	let keptTokens = 0;

	for (let index = turns.length - 1; index >= 0; index--) {
		const tokens = turnTokens(turns[index]);
		if (index === turns.length - 1 || keptTokens < keepRecentTokens) {
			keptStartIndex = index;
			keptTokens += tokens;
			continue;
		}
		break;
	}

	if (keptStartIndex <= 0) return null;

	const compactedTurns = turns.slice(0, keptStartIndex);
	const keptTurns = turns.slice(keptStartIndex);
	const messagesToSummarize = compactedTurns.flatMap((turn) => turn.messages);
	if (messagesToSummarize.length === 0) return null;

	const tokensBefore = turns.reduce((sum, turn) => sum + turnTokens(turn), 0);
	const summaryMessage = await generateSummary(
		messagesToSummarize,
		model,
		apiKey,
		signal,
	);

	const summaryTurn: Turn = {
		id: summaryMessage.turnId,
		messages: [summaryMessage],
	};

	return {
		turns: [summaryTurn, ...keptTurns],
		compactedTurnCount: compactedTurns.length,
		keptTurnCount: keptTurns.length,
		tokensBefore,
	};
}
