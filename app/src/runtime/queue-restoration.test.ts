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

		expect(runtime.drainPendingMessages()).toEqual([queued]);
		expect(agent.getPendingFollowUps()).toEqual([]);
		expect(published).toEqual({
			type: "chat.message-queue.changed",
			payload: { count: 0, messages: [], steering: [] },
		});
	});
});
