import { describe, expect, test } from "bun:test";
import { Agent, type AgentMessage } from "./agent";
import { AgentRuntime } from "./agent-runtime";

type RuntimeWithQueue = {
	agent: Agent;
	bus: {
		publish: (type: string, payload: unknown) => void;
	};
	drainPendingMessages: InstanceType<
		typeof AgentRuntime
	>["drainPendingMessages"];
	getPendingMessageCount: InstanceType<
		typeof AgentRuntime
	>["getPendingMessageCount"];
	getPendingMessageDrafts: InstanceType<
		typeof AgentRuntime
	>["getPendingMessageDrafts"];
};

describe("queued message restoration", () => {
	test("preserves queued messages while replacing turns for recovery", () => {
		const agent = new Agent();
		agent.steer({
			role: "user",
			content: "steer me",
			timestamp: 1,
		});
		agent.followUp({
			role: "user",
			content: "follow me",
			timestamp: 2,
		});

		agent.replaceFromTurns([]);

		expect(agent.getPendingSteering()).toEqual(["steer me"]);
		expect(agent.getPendingFollowUps()).toEqual(["follow me"]);
		expect(agent.getPendingFollowUpGeneration()).toBe(1);
		agent.dispose();
	});

	test("advances the generation when reset clears follow-ups", () => {
		const agent = new Agent();
		agent.followUp({ role: "user", content: "queued", timestamp: 1 });

		agent.reset();

		expect(agent.getPendingFollowUps()).toEqual([]);
		expect(agent.getPendingFollowUpGeneration()).toBe(2);
		agent.dispose();
	});

	test("drains structured messages and publishes an empty queue", () => {
		const agent = new Agent();
		const queued = {
			role: "user",
			content: [
				{ type: "text", text: "describe this" },
				{
					type: "image",
					data: "base64-data",
					mimeType: "image/png",
					filename: "screenshot.png",
				},
			],
			timestamp: 123,
		} as AgentMessage;
		agent.followUp(queued);

		let published: unknown;
		const runtime = Object.create(AgentRuntime.prototype) as RuntimeWithQueue;
		runtime.agent = agent;
		runtime.bus = {
			publish: (type, payload) => {
				published = { type, payload };
			},
		};

		expect(agent.getPendingFollowUpGeneration()).toBe(1);
		expect(runtime.getPendingMessageCount()).toBe(1);
		expect(runtime.getPendingMessageDrafts()).toBeNull();
		expect(runtime.drainPendingMessages()).toEqual([queued]);
		expect(agent.getPendingFollowUps()).toEqual([]);
		expect(agent.getPendingFollowUpGeneration()).toBe(2);
		expect(published).toEqual({
			type: "chat.message-queue.changed",
			payload: { count: 0, generation: 2, messages: [], steering: [] },
		});
	});
});
