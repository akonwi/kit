import { describe, expect, test } from "bun:test";
import { Agent, type AgentMessage } from "./agent";
import { AgentRuntime } from "./agent-runtime";

type RuntimeWithQueue = {
	agent: {
		getPendingFollowUps: () => string[];
		updatePendingFollowUp: (index: number, text: string) => void;
		removePendingFollowUp: (index: number) => void;
		clearPendingFollowUps: () => void;
	};
	bus: {
		publish: (type: string, payload: unknown) => void;
	};
	updatePendingMessage: InstanceType<
		typeof AgentRuntime
	>["updatePendingMessage"];
	removePendingMessage: InstanceType<
		typeof AgentRuntime
	>["removePendingMessage"];
	clearPendingMessages: InstanceType<
		typeof AgentRuntime
	>["clearPendingMessages"];
};

describe("queued follow-up editing", () => {
	test("editing a queued multipart message preserves image attachments", () => {
		const agent = new Agent();
		const message = {
			role: "user",
			content: [
				{ type: "text", text: "before" },
				{
					type: "image",
					data: "base64-data",
					mimeType: "image/png",
					filename: "screenshot.png",
				},
			],
			timestamp: 123,
		} as AgentMessage;

		agent.followUp(message);
		expect(agent.getPendingFollowUps()).toEqual([
			"before\nAttached image: screenshot.png",
		]);
		expect(agent.getPendingFollowUpDrafts()).toEqual(["before"]);

		agent.updatePendingFollowUp(0, "after");

		expect(agent.getPendingFollowUps()).toEqual([
			"after\nAttached image: screenshot.png",
		]);
		expect(agent.getPendingFollowUpDrafts()).toEqual(["after"]);
		const queued = (agent as unknown as { _queuedFollowUps: AgentMessage[] })
			._queuedFollowUps[0] as AgentMessage & {
			content: Array<{ type: string; text?: string; filename?: string }>;
		};
		expect(queued.content).toEqual([
			{ type: "text", text: "after" },
			{
				type: "image",
				data: "base64-data",
				mimeType: "image/png",
				filename: "screenshot.png",
			},
		]);
	});

	test("editing a queued structured message preserves metadata", () => {
		const agent = new Agent();
		const message = {
			role: "user",
			content: [{ type: "text", text: "before" }],
			timestamp: 123,
			synthetic: {
				kind: "prompt-command",
				command: "review",
				args: "auth flow",
			},
		} as AgentMessage;

		agent.followUp(message);
		agent.updatePendingFollowUp(0, "after");

		expect(agent.getPendingFollowUps()).toEqual(["after"]);
		const queued = (agent as unknown as { _queuedFollowUps: AgentMessage[] })
			._queuedFollowUps[0] as AgentMessage & {
			synthetic?: { kind: string; command: string; args?: string };
		};
		expect(queued.synthetic).toEqual({
			kind: "prompt-command",
			command: "review",
			args: "auth flow",
		});
		expect(queued.timestamp).toBe(123);
	});

	test("removing a queued message updates the pending text list", () => {
		const agent = new Agent();
		agent.followUp({ role: "user", content: "first", timestamp: 1 });
		agent.followUp({ role: "user", content: "second", timestamp: 2 });

		agent.removePendingFollowUp(0);

		expect(agent.getPendingFollowUps()).toEqual(["second"]);
	});

	test("runtime queue change event includes updated messages", () => {
		let messages = ["first", "second"];
		let published: unknown = null;
		const runtime = Object.create(AgentRuntime.prototype) as RuntimeWithQueue;
		runtime.agent = {
			getPendingFollowUps: () => messages,
			updatePendingFollowUp: (index, text) => {
				messages = messages.map((message, messageIndex) =>
					messageIndex === index ? text : message,
				);
			},
			removePendingFollowUp: (index) => {
				messages = messages.filter((_, messageIndex) => messageIndex !== index);
			},
			clearPendingFollowUps: () => {
				messages = [];
			},
		};
		runtime.bus = {
			publish: (type, payload) => {
				published = { type, payload };
			},
		};

		runtime.updatePendingMessage(1, "updated");
		expect(published).toEqual({
			type: "chat.message-queue.changed",
			payload: { count: 2, messages: ["first", "updated"] },
		});

		runtime.removePendingMessage(0);
		expect(published).toEqual({
			type: "chat.message-queue.changed",
			payload: { count: 1, messages: ["updated"] },
		});

		runtime.clearPendingMessages();
		expect(published).toEqual({
			type: "chat.message-queue.changed",
			payload: { count: 0, messages: [] },
		});
	});
});
