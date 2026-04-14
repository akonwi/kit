import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { Api, Model, Usage } from "@mariozechner/pi-ai";

type SyntheticCompactionSummaryMessage = AgentMessage & {
	synthetic?: { kind: "compaction-summary" };
};

export type RuntimeContextUsage = {
	tokens: number;
	contextWindow: number;
	percent: number;
	usageTokens: number;
	trailingTokens: number;
	lastUsageIndex: number | null;
};

export function calculateContextTokens(usage: Usage): number {
	return (
		usage.totalTokens ||
		usage.input + usage.output + usage.cacheRead + usage.cacheWrite
	);
}

function isSyntheticCompactionSummary(
	message: AgentMessage,
): message is SyntheticCompactionSummaryMessage {
	return (
		typeof message === "object" &&
		message !== null &&
		"synthetic" in message &&
		(message as SyntheticCompactionSummaryMessage).synthetic?.kind ===
			"compaction-summary"
	);
}

function getAssistantUsage(message: AgentMessage): Usage | undefined {
	if (message.role !== "assistant") return undefined;
	if (isSyntheticCompactionSummary(message)) return undefined;
	if (message.stopReason === "aborted" || message.stopReason === "error") {
		return undefined;
	}
	return message.usage;
}

export function estimateTokens(message: AgentMessage): number {
	let chars = 0;

	switch (message.role) {
		case "user": {
			if (typeof message.content === "string") {
				chars = message.content.length;
			} else {
				for (const block of message.content) {
					if (block.type === "text") chars += block.text.length;
					if (block.type === "image") chars += 4800;
				}
			}
			break;
		}

		case "assistant": {
			for (const block of message.content) {
				if (block.type === "text") chars += block.text.length;
				if (block.type === "thinking") chars += block.thinking.length;
				if (block.type === "toolCall") {
					chars += block.name.length + JSON.stringify(block.arguments).length;
				}
			}
			break;
		}

		case "toolResult": {
			for (const block of message.content) {
				if (block.type === "text") chars += block.text.length;
				if (block.type === "image") chars += 4800;
			}
			break;
		}

		default: {
			const fallback = message as Record<string, unknown>;
			if ("content" in fallback) {
				const content = fallback.content;
				if (typeof content === "string") {
					chars = content.length;
				} else if (Array.isArray(content)) {
					for (const block of content) {
						if (
							typeof block === "object" &&
							block !== null &&
							"type" in block &&
							block.type === "text" &&
							"text" in block &&
							typeof block.text === "string"
						) {
							chars += block.text.length;
						}
					}
				}
			}
		}
	}

	return Math.ceil(chars / 4);
}

export function getRuntimeContextUsage(
	messages: AgentMessage[],
	model: Model<Api> | undefined,
): RuntimeContextUsage | null {
	if (!model?.contextWindow) return null;

	for (let index = messages.length - 1; index >= 0; index--) {
		const usage = getAssistantUsage(messages[index]);
		if (!usage) continue;
		const usageTokens = calculateContextTokens(usage);
		let trailingTokens = 0;
		for (let i = index + 1; i < messages.length; i++) {
			trailingTokens += estimateTokens(messages[i]);
		}
		const tokens = usageTokens + trailingTokens;
		return {
			tokens,
			contextWindow: model.contextWindow,
			percent: Math.round((tokens / model.contextWindow) * 100),
			usageTokens,
			trailingTokens,
			lastUsageIndex: index,
		};
	}

	let tokens = 0;
	for (const message of messages) {
		tokens += estimateTokens(message);
	}

	return {
		tokens,
		contextWindow: model.contextWindow,
		percent: Math.round((tokens / model.contextWindow) * 100),
		usageTokens: 0,
		trailingTokens: tokens,
		lastUsageIndex: null,
	};
}
