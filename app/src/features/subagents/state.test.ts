import { describe, expect, test } from "bun:test";
import type { AgentRuntimeEvent } from "../../runtime/agent-runtime";
import type { Session, SessionEntry } from "../../session";
import type { SubagentDefinition } from "./discovery";
import { createSubagentCompactionEntry, SubagentManager } from "./state";

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

function assistantMessage(
	text: string,
	content?: Array<
		| { type: "text"; text: string }
		| {
				type: "toolCall";
				id: string;
				name: string;
				arguments: Record<string, unknown>;
		  }
	>,
) {
	return {
		role: "assistant" as const,
		content: content ?? [{ type: "text" as const, text }],
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
	test("creates normalized sub-agent compaction entries from runtime events", () => {
		const keptTurn = {
			id: "kept-turn",
			messages: [
				{
					role: "user" as const,
					content: "kept question",
					timestamp: Date.parse("2025-01-01T00:00:04.000Z"),
					turnId: "stale-turn",
				},
			],
		};
		const event: Extract<
			AgentRuntimeEvent,
			{ type: "session.compaction.completed.auto" }
		> = {
			type: "session.compaction.completed.auto",
			contextPercent: 91,
			compactedTurnCount: 3,
			keptTurnCount: 1,
			tokensBefore: 123,
			firstKeptTurnId: "kept-turn",
			keptTurns: [keptTurn],
			summaryMessage: {
				...assistantMessage("summary"),
				turnId: "summary-turn",
			},
		};

		const entry = createSubagentCompactionEntry(event, {
			timestamp: "2025-01-01T00:00:05.000Z",
			agentName: "scout",
			subagentConversationId: "conv-1",
		});

		expect(entry).toMatchObject({
			type: "subagent_compaction",
			agentName: "scout",
			subagentConversationId: "conv-1",
			compactedTurnCount: 3,
			keptTurnCount: 1,
			tokensBefore: 123,
		});
		if (entry.type !== "subagent_compaction") {
			throw new Error("Expected subagent compaction entry");
		}
		expect(entry.keptTurns[0]?.messages[0]?.turnId).toBe("kept-turn");
		expect("turnId" in entry.message).toBe(false);
	});

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

	test("reset aborts running sub-agent runtimes before disposal", () => {
		const calls: string[] = [];
		const manager = new SubagentManager({
			runtime,
			getAgents: () => agents,
			readEntries: async () => [],
			appendEntries: async () => [],
			createRuntime: async () => {
				throw new Error("not used");
			},
		});
		manager.setActive({
			agentName: "scout",
			subagentConversationId: "conv-1",
			status: "running",
			lastActivityAt: "2025-01-01T00:00:00.000Z",
			runtime: {
				async run() {
					return { status: "completed" as const };
				},
				abort(reason?: string) {
					calls.push(`abort:${reason}`);
				},
				dispose() {
					calls.push("dispose");
				},
			},
		});

		manager.reset();

		expect(calls).toEqual(["abort:Session closed", "dispose"]);
		expect(manager.getActive("scout")).toBeUndefined();
	});

	test("reset disposes idle sub-agent runtimes without aborting", () => {
		const calls: string[] = [];
		const manager = new SubagentManager({
			runtime,
			getAgents: () => agents,
			readEntries: async () => [],
			appendEntries: async () => [],
			createRuntime: async () => {
				throw new Error("not used");
			},
		});
		manager.setActive({
			agentName: "scout",
			subagentConversationId: "conv-1",
			status: "idle",
			lastActivityAt: "2025-01-01T00:00:00.000Z",
			runtime: {
				async run() {
					return { status: "completed" as const };
				},
				abort(reason?: string) {
					calls.push(`abort:${reason}`);
				},
				dispose() {
					calls.push("dispose");
				},
			},
		});

		manager.reset();

		expect(calls).toEqual(["dispose"]);
		expect(manager.getActive("scout")).toBeUndefined();
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

	test("replays only compacted sub-agent history after latest compaction", async () => {
		const summary = assistantMessage("older work summarized");
		const keptTurn = {
			id: "kept-turn",
			messages: [
				{
					role: "user" as const,
					content: "kept question",
					timestamp: Date.parse("2025-01-01T00:00:04.000Z"),
					turnId: "kept-turn",
				},
				{
					...assistantMessage("kept answer"),
					turnId: "kept-turn",
				},
			],
		};
		const persisted: SessionEntry[] = [
			{
				type: "subagent_started",
				id: "1",
				parentId: null,
				timestamp: "2025-01-01T00:00:00.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
			},
			{
				type: "subagent_prompt",
				id: "2",
				parentId: "1",
				timestamp: "2025-01-01T00:00:01.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
				prompt: "old prompt should be compacted away",
			},
			{
				type: "subagent_compaction",
				id: "3",
				parentId: "2",
				timestamp: "2025-01-01T00:00:03.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				message: summary,
				compactedTurnCount: 1,
				keptTurnCount: 1,
				tokensBefore: 100,
				keptTurns: [keptTurn],
			},
			{
				type: "subagent_prompt",
				id: "4",
				parentId: "3",
				timestamp: "2025-01-01T00:00:05.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
				prompt: "new prompt",
			},
			{
				type: "subagent_message_completed",
				id: "5",
				parentId: "4",
				timestamp: "2025-01-01T00:00:06.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				messageId: "msg-1",
				message: assistantMessage("new answer"),
			},
		];
		let capturedTurns: Session["turns"] = [];
		const manager = new SubagentManager({
			runtime,
			getAgents: () => agents,
			readEntries: async () => persisted,
			appendEntries: async () => [],
			createRuntime: async (options) => {
				capturedTurns = options.historyTurns;
				return {
					async run() {
						return { status: "completed" as const, message: "continued" };
					},
					abort() {},
					dispose() {},
				};
			},
		});

		await manager.hydrate();
		await manager.run("scout", "continue");

		expect(capturedTurns.map((turn) => turn.id)).toEqual([
			"conv-1:3",
			"kept-turn",
			"conv-1:4",
		]);
		expect(capturedTurns[0]?.messages[0]).toMatchObject({
			role: "assistant",
			content: summary.content,
		});
		expect(capturedTurns[2]?.messages.map((message) => message.role)).toEqual([
			"user",
			"assistant",
		]);
	});

	test("reconstructs prior tool results into history turns for continued runs", async () => {
		const persisted: SessionEntry[] = [
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
				type: "subagent_prompt",
				id: "2",
				parentId: "1",
				timestamp: "2025-01-01T00:00:01.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
				prompt: "inspect auth",
			},
			{
				type: "subagent_message_completed",
				id: "3",
				parentId: "2",
				timestamp: "2025-01-01T00:00:02.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				messageId: "msg-1",
				message: assistantMessage("", [
					{
						type: "toolCall",
						id: "tool-1",
						name: "grep",
						arguments: { path: "src", pattern: "auth" },
					},
				]),
			},
			{
				type: "subagent_tool_completed",
				id: "4",
				parentId: "3",
				timestamp: "2025-01-01T00:00:03.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				toolCallId: "tool-1",
				toolName: "grep",
				result: {
					content: [{ type: "text", text: "src/auth/index.ts" }],
					details: { matches: 1 },
				},
				isError: false,
			},
			{
				type: "subagent_message_completed",
				id: "5",
				parentId: "4",
				timestamp: "2025-01-01T00:00:04.000Z",
				agentName: "scout",
				subagentConversationId: "conv-1",
				messageId: "msg-2",
				message: assistantMessage("found auth entry point"),
			},
		];
		let capturedTurns: Session["turns"] = [];
		const manager = new SubagentManager({
			runtime,
			getAgents: () => agents,
			readEntries: async () => persisted,
			appendEntries: async () => [],
			createRuntime: async (options) => {
				capturedTurns = options.historyTurns;
				return {
					async run() {
						return { status: "completed" as const, message: "continued" };
					},
					abort() {},
					dispose() {},
				};
			},
		});

		await manager.hydrate();
		await manager.run("scout", "continue with oauth state");

		expect(capturedTurns).toHaveLength(1);
		expect(capturedTurns[0]?.messages.map((message) => message.role)).toEqual([
			"user",
			"assistant",
			"toolResult",
			"assistant",
		]);
		const toolResult = capturedTurns[0]?.messages[2];
		expect(toolResult).toMatchObject({
			role: "toolResult",
			toolCallId: "tool-1",
			toolName: "grep",
			isError: false,
		});
	});
});
