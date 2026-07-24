import { describe, expect, test } from "bun:test";
import {
	createMemorySubagentParentStorage,
	createMemorySubagentSessionStorage,
} from "./memory-storage";

describe("in-memory subagent storage", () => {
	test("stores parent references in memory", async () => {
		const storage = createMemorySubagentParentStorage();
		const session = {
			id: "parent-1",
			version: 2 as const,
			cwd: "/tmp/project",
			createdAt: "2026-01-01T00:00:00.000Z",
			updatedAt: "2026-01-01T00:00:00.000Z",
			turns: [],
		};
		await storage.appendEntries(session, [
			{
				type: "subagent_started",
				timestamp: "2026-01-01T00:00:00.000Z",
				agentName: "reviewer",
				subagentConversationId: "child-1",
				source: "agent",
			},
		]);
		expect(await storage.readEntries("parent-1")).toHaveLength(1);
	});

	test("stores subagent state without filesystem persistence", async () => {
		const storage = createMemorySubagentSessionStorage();
		await storage.create({
			id: "child-1",
			ownerSessionId: "parent-1",
			cwd: "/tmp/project",
			agentName: "reviewer",
			source: "agent",
		});
		const appended = await storage.appendEntries("child-1", [
			{
				type: "subagent_prompt",
				timestamp: "2026-01-01T00:00:00.000Z",
				agentName: "reviewer",
				subagentConversationId: "child-1",
				source: "agent",
				prompt: "Review this",
			},
		]);

		expect(await storage.readHeader("child-1")).toMatchObject({
			id: "child-1",
			ownerSessionId: "parent-1",
		});
		expect(appended).toHaveLength(1);
		expect((await storage.readEntries("child-1"))[0]).toMatchObject({
			type: "subagent_prompt",
			parentId: null,
		});

		await storage.delete("child-1");
		expect(await storage.readHeader("child-1")).toBeNull();
	});
});
