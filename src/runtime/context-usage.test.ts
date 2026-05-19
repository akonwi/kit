import { describe, expect, test } from "bun:test";
import type { AgentMessage } from "@earendil-works/pi-agent-core";
import type { Api, Model, Usage } from "@earendil-works/pi-ai";
import { getRuntimeContextUsage } from "./context-usage";

const model = {
	id: "claude-sonnet-4-6",
	name: "Claude Sonnet",
	provider: "anthropic",
	contextWindow: 1000,
} as Model<Api>;

function usage(totalTokens: number): Usage {
	return {
		input: totalTokens,
		output: 0,
		cacheRead: 0,
		cacheWrite: 0,
		totalTokens,
		cost: {
			input: 0,
			output: 0,
			cacheRead: 0,
			cacheWrite: 0,
			total: 0,
		},
	};
}

function userMessage(
	text: string,
	timestamp = 1,
): Extract<AgentMessage, { role: "user" }> {
	return {
		role: "user",
		content: text,
		timestamp,
	} as Extract<AgentMessage, { role: "user" }>;
}

function assistantMessage(
	text: string,
	totalTokens: number,
	timestamp = 1,
): Extract<AgentMessage, { role: "assistant" }> {
	return {
		role: "assistant",
		content: [{ type: "text", text }],
		api: "anthropic-messages",
		provider: "anthropic",
		model: "claude-sonnet-4-6",
		usage: usage(totalTokens),
		stopReason: "stop",
		timestamp,
	} as Extract<AgentMessage, { role: "assistant" }>;
}

function compactionSummary(
	text: string,
	timestamp = 10,
): Extract<AgentMessage, { role: "assistant" }> {
	return {
		role: "assistant",
		content: [{ type: "text", text }],
		api: "anthropic-messages",
		provider: "anthropic",
		model: "claude-sonnet-4-6",
		usage: usage(100),
		stopReason: "stop",
		timestamp,
		synthetic: { kind: "compaction-summary" },
	} as Extract<AgentMessage, { role: "assistant" }>;
}

describe("runtime context usage", () => {
	test("uses the latest compatible assistant usage for ordinary sessions", () => {
		const result = getRuntimeContextUsage(
			[userMessage("hello", 1), assistantMessage("world", 420, 2)],
			model,
		);

		expect(result?.usageTokens).toBe(420);
		expect(result?.percent).toBe(42);
		expect(result?.lastUsageIndex).toBe(1);
	});

	test("does not keep using stale pre-compaction usage from kept turns", () => {
		const result = getRuntimeContextUsage(
			[
				compactionSummary("Earlier context was compacted.", 10),
				userMessage("kept recent user turn", 2),
				assistantMessage("kept recent assistant turn", 950, 3),
			],
			model,
		);

		// Immediately after compaction, kept assistant messages still carry usage
		// from the pre-compaction prompt. The header should fall back to estimating
		// the compacted message list until a new assistant response records fresh
		// provider usage for the compacted context.
		expect(result?.usageTokens).toBe(0);
		expect(result?.lastUsageIndex).toBeNull();
		expect(result?.percent).toBeLessThan(10);
	});

	test("uses fresh assistant usage recorded after compaction", () => {
		const result = getRuntimeContextUsage(
			[
				compactionSummary("Earlier context was compacted.", 10),
				userMessage("kept recent user turn", 2),
				assistantMessage("kept recent assistant turn", 950, 3),
				userMessage("new post-compaction turn", 11),
				assistantMessage("fresh post-compaction assistant turn", 120, 12),
			],
			model,
		);

		expect(result?.usageTokens).toBe(120);
		expect(result?.percent).toBe(12);
		expect(result?.lastUsageIndex).toBe(4);
	});
});
