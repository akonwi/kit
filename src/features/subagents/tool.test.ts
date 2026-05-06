import { describe, expect, test } from "bun:test";
import type { SubagentDefinition } from "./discovery";
import { SubagentManager, SubagentManagerError } from "./state";
import { createSubagentTool } from "./tool";

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

describe("createSubagentTool", () => {
	test("lists discovered sub-agents", async () => {
		const tool = createSubagentTool({
			getAgents: () => agents,
			manager: {
				getActive: () => undefined,
				dismiss: async () => false,
				run: async () => ({ status: "completed", message: "done" }),
			},
		});

		const result = await tool.execute("call-1", { action: "list_agents" });
		expect(result.details).toEqual({
			ok: true,
			action: "list_agents",
			agents: [
				{
					name: "scout",
					description: "Fast reconnaissance",
					model: "claude-haiku-4-5",
					source: "kit-user",
				},
			],
		});
	});

	test("runs a named sub-agent", async () => {
		const tool = createSubagentTool({
			getAgents: () => agents,
			manager: {
				getActive: () => undefined,
				dismiss: async () => false,
				run: async (agent, message) => ({
					status: "completed",
					message: `${agent}:${message}`,
				}),
			},
		});

		const result = await tool.execute("call-1", {
			action: "run",
			agent: "scout",
			message: "find auth entry points",
		});
		expect(result.details).toEqual({
			ok: true,
			action: "run",
			agent: "scout",
			status: "completed",
			message: "scout:find auth entry points",
		});
	});

	test("reports manager errors for run", async () => {
		const tool = createSubagentTool({
			getAgents: () => agents,
			manager: {
				getActive: () => undefined,
				dismiss: async () => false,
				run: async () => {
					throw new SubagentManagerError(
						"SUBAGENT_BUSY",
						'Sub-agent "scout" is already running.',
					);
				},
			},
		});

		const result = await tool.execute("call-1", {
			action: "run",
			agent: "scout",
			message: "find auth entry points",
		});
		expect(result.details).toEqual({
			ok: false,
			action: "run",
			code: "SUBAGENT_BUSY",
			message: 'Sub-agent "scout" is already running.',
		});
	});

	test("reports inactive status when no active conversation exists", async () => {
		const tool = createSubagentTool({
			getAgents: () => agents,
			manager: {
				getActive: () => undefined,
				dismiss: async () => false,
				run: async () => ({ status: "completed" }),
			},
		});

		const result = await tool.execute("call-1", {
			action: "status",
			agent: "scout",
		});
		expect(result.details).toEqual({
			ok: true,
			action: "status",
			agent: "scout",
			active: false,
		});
	});

	test("returns active status details for an active conversation", async () => {
		const manager = Object.create(SubagentManager.prototype) as Pick<
			SubagentManager,
			"getActive" | "dismiss" | "run"
		>;
		manager.getActive = () => ({
			agentName: "scout",
			subagentConversationId: "conv-1",
			status: "idle",
			model: "claude-haiku-4-5",
			description: "Fast reconnaissance",
			lastActivityAt: "2025-01-01T00:00:00.000Z",
			latestMessage: "done",
		});
		manager.dismiss = async () => false;
		manager.run = async () => ({ status: "completed" });

		const tool = createSubagentTool({
			getAgents: () => agents,
			manager,
		});

		const result = await tool.execute("call-1", {
			action: "status",
			agent: "scout",
		});
		expect(result.details).toEqual({
			ok: true,
			action: "status",
			agent: "scout",
			active: true,
			status: "idle",
			model: "claude-haiku-4-5",
			lastActivityAt: "2025-01-01T00:00:00.000Z",
			latestMessage: "done",
		});
	});

	test("dismisses an active conversation", async () => {
		const tool = createSubagentTool({
			getAgents: () => agents,
			manager: {
				getActive: () => undefined,
				dismiss: async () => true,
				run: async () => ({ status: "completed" }),
			},
		});

		const result = await tool.execute("call-1", {
			action: "dismiss",
			agent: "scout",
		});
		expect(result.details).toEqual({
			ok: true,
			action: "dismiss",
			agent: "scout",
			dismissed: true,
		});
	});

	test("rejects missing run inputs", async () => {
		const tool = createSubagentTool({
			getAgents: () => agents,
			manager: {
				getActive: () => undefined,
				dismiss: async () => false,
				run: async () => ({ status: "completed" }),
			},
		});

		const missingAgent = await tool.execute("call-1", { action: "run" });
		expect(missingAgent.details).toEqual({
			ok: false,
			action: "run",
			code: "INVALID_INPUT",
			message: "Provide a sub-agent name.",
		});

		const missingMessage = await tool.execute("call-2", {
			action: "run",
			agent: "scout",
		});
		expect(missingMessage.details).toEqual({
			ok: false,
			action: "run",
			code: "INVALID_INPUT",
			message: "Provide a sub-agent message.",
		});
	});
});
