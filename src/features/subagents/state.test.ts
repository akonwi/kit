import { describe, expect, test } from "bun:test";
import type { Session, SessionEntry } from "../../session";
import type { SubagentDefinition } from "./discovery";
import { SubagentManager } from "./state";

const session: Session = {
	id: "session-1",
	version: 2,
	cwd: "/tmp/project",
	createdAt: "2025-01-01T00:00:00.000Z",
	updatedAt: "2025-01-01T00:00:00.000Z",
	turns: [],
};

const runtime = {
	agentInfo: { model: undefined, thinkingLevel: "medium" as const },
	getAvailableModels: () => [],
	getContextFiles: () => [],
	getSession: () => session,
	getSystemPromptAdditions: () => [],
	getTools: () => [],
	settings: {},
};

const agents: SubagentDefinition[] = [
	{
		name: "scout",
		description: "Fast reconnaissance",
		model: "claude-haiku-4-5",
		instructions: "Scout instructions",
		filePath: "/tmp/scout.md",
		baseDir: "/tmp",
		source: "kit-user",
	},
];

function assistantMessage(text: string) {
	return {
		role: "assistant" as const,
		content: [{ type: "text" as const, text }],
		api: "anthropic-messages",
		provider: "anthropic",
		model: "claude-haiku-4-5",
		usage: {
			input: 1,
			output: 1,
			cacheRead: 0,
			cacheWrite: 0,
			totalTokens: 2,
			cost: {
				input: 0,
				output: 0,
				cacheRead: 0,
				cacheWrite: 0,
				total: 0,
			},
		},
		stopReason: "stop" as const,
		timestamp: Date.now(),
	};
}

describe("SubagentManager", () => {
	test("hydrates active conversations from persisted session entries", async () => {
		const entries: SessionEntry[] = [
			{
				type: "subagent_started",
				id: "1",
				parentId: null,
				timestamp: "2025-01-01T00:00:00.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
				model: "claude-haiku-4-5",
				description: "Fast reconnaissance",
			},
			{
				type: "subagent_message_completed",
				id: "2",
				parentId: "1",
				timestamp: "2025-01-01T00:00:01.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				messageId: "msg-1",
				message: assistantMessage("first result"),
			},
			{
				type: "subagent_started",
				id: "3",
				parentId: "2",
				timestamp: "2025-01-01T00:00:02.000Z",
				agentName: "reviewer",
				subagentConversationId: "conv-2",
				source: "agent",
			},
			{
				type: "subagent_dismissed",
				id: "4",
				parentId: "3",
				timestamp: "2025-01-01T00:00:03.000Z",
				agentName: "reviewer",
				subagentConversationId: "conv-2",
			},
		];
		const manager = new SubagentManager({
			runtime,
			getAgents: () => agents,
			readEntries: async () => entries,
			appendEntries: async () => [],
			createRuntime: async () => {
				throw new Error("not used");
			},
		});

		await manager.hydrate();

		expect(manager.getActive("scout")).toMatchObject({
			agentName: "scout",
			subagentConversationId: "conv-1",
			status: "idle",
			latestMessage: "first result",
		});
		expect(manager.getActive("reviewer")).toBeUndefined();
	});

	test("run persists sub-agent lifecycle entries and updates active state", async () => {
		const persisted: SessionEntry[] = [];
		let nextId = 1;
		const manager = new SubagentManager({
			runtime,
			getAgents: () => agents,
			readEntries: async () => persisted,
			appendEntries: async (_session, entries) => {
				const appended = entries.map((entry, index) => ({
					...entry,
					id: `e-${nextId + index}`,
					parentId:
						persisted.at(-1)?.id ??
						(index > 0 ? `e-${nextId + index - 1}` : null),
				})) as SessionEntry[];
				nextId += entries.length;
				persisted.push(...appended);
				return appended;
			},
			createRuntime: async (options) => ({
				async run() {
					const message = assistantMessage("delegated answer");
					await options.onEntries([
						{
							type: "subagent_message_started",
							timestamp: "2025-01-01T00:00:05.000Z",
							agentName: options.definition.name,
							subagentConversationId: options.subagentConversationId,
							messageId: "msg-1",
						},
						{
							type: "subagent_message_completed",
							timestamp: "2025-01-01T00:00:06.000Z",
							agentName: options.definition.name,
							subagentConversationId: options.subagentConversationId,
							messageId: "msg-1",
							message,
						},
					]);
					options.onCompletedMessage(message, "delegated answer");
					options.onTerminalState("idle");
					return { status: "completed" as const, message: "delegated answer" };
				},
				abort() {},
				dispose() {},
			}),
		});

		const result = await manager.run("scout", "find auth entry points");

		expect(result).toEqual({
			status: "completed",
			message: "delegated answer",
		});
		expect(persisted.map((entry) => entry.type)).toEqual([
			"subagent_started",
			"subagent_prompt",
			"subagent_message_started",
			"subagent_message_completed",
		]);
		const active = manager.getActive("scout");
		expect(active).toMatchObject({
			agentName: "scout",
			status: "idle",
			latestMessage: "delegated answer",
		});
	});
});
