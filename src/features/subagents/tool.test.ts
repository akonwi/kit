import { describe, expect, test } from "bun:test";
import type { SubagentDefinition } from "./discovery";
import { SubagentManager } from "./state";
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
		const manager = new SubagentManager();
		const tool = createSubagentTool({
			getAgents: () => agents,
			manager,
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
		expect(result.content[0]?.type).toBe("text");
		if (result.content[0]?.type !== "text") {
			throw new Error("Expected text content");
		}
		expect(result.content[0].text).toContain("scout");
	});

	test("reports inactive status when no active conversation exists", async () => {
		const tool = createSubagentTool({
			getAgents: () => agents,
			manager: new SubagentManager(),
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
		const manager = new SubagentManager();
		manager.setActive({
			agentName: "scout",
			subagentConversationId: "conv-1",
			status: "idle",
			model: "claude-haiku-4-5",
			description: "Fast reconnaissance",
			lastActivityAt: "2025-01-01T00:00:00.000Z",
		});
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
		});
	});

	test("dismisses an active conversation", async () => {
		const manager = new SubagentManager();
		manager.setActive({
			agentName: "scout",
			subagentConversationId: "conv-1",
			status: "failed",
			lastActivityAt: "2025-01-01T00:00:00.000Z",
		});
		const tool = createSubagentTool({
			getAgents: () => agents,
			manager,
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
		expect(manager.getActive("scout")).toBeUndefined();
	});

	test("rejects status and dismiss without an agent name", async () => {
		const tool = createSubagentTool({
			getAgents: () => agents,
			manager: new SubagentManager(),
		});

		const statusResult = await tool.execute("call-1", { action: "status" });
		expect(statusResult.details).toEqual({
			ok: false,
			action: "status",
			code: "INVALID_INPUT",
			message: "Provide a sub-agent name.",
		});

		const dismissResult = await tool.execute("call-2", { action: "dismiss" });
		expect(dismissResult.details).toEqual({
			ok: false,
			action: "dismiss",
			code: "INVALID_INPUT",
			message: "Provide a sub-agent name.",
		});
	});
});
