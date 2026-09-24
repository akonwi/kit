import { describe, expect, test } from "bun:test";
import type { AgentMessage } from "./agent";
import { Agent } from "./agent";

function drive(agent: Agent, event: unknown) {
	const internals = agent as unknown as {
		processPiEvent: (e: unknown) => Array<{ type: string }>;
		bus: { publish: (type: string, payload: unknown) => void };
	};
	for (const nextEvent of internals.processPiEvent(event)) {
		const { type, ...payload } = nextEvent;
		internals.bus.publish(type, payload);
	}
}

function userMessage(text: string): Extract<AgentMessage, { role: "user" }> {
	return {
		role: "user",
		content: text,
		timestamp: Date.now(),
	} as Extract<AgentMessage, { role: "user" }>;
}

function assistantMessage(
	text: string,
): Extract<AgentMessage, { role: "assistant" }> {
	return {
		role: "assistant",
		content: [{ type: "text", text }],
		api: "anthropic-messages",
		provider: "anthropic",
		model: "claude-sonnet-4-6",
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
		stopReason: "stop",
		timestamp: Date.now(),
	} as Extract<AgentMessage, { role: "assistant" }>;
}

function toolResultMessage(
	toolCallId: string,
): Extract<AgentMessage, { role: "toolResult" }> {
	return {
		role: "toolResult",
		toolCallId,
		toolName: "read",
		content: [{ type: "text", text: "result" }],
		isError: false,
		timestamp: Date.now(),
	} as Extract<AgentMessage, { role: "toolResult" }>;
}

describe("Agent user-facing turn boundaries", () => {
	test("records structured prompt user messages before provider events", async () => {
		const agent = new Agent({});
		(agent as unknown as { pi: { prompt: () => Promise<void> } }).pi.prompt =
			async () => {};

		await agent.prompt(userMessage("persist me"));

		expect(agent.turns).toHaveLength(1);
		expect(agent.turns[0]?.messages.map((message) => message.role)).toEqual([
			"user",
		]);
	});

	test("translates Pi streaming into identified Kit message events", () => {
		const agent = new Agent({});
		const events: Array<Record<string, unknown>> = [];
		agent.subscribe((event) => events.push(event));
		const started = assistantMessage("");
		const updated = assistantMessage("hi");

		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: started });
		drive(agent, {
			type: "message_update",
			message: updated,
			assistantMessageEvent: {
				type: "text_delta",
				contentIndex: 0,
				delta: "hi",
				partial: updated,
			},
		});
		const activeMessage = agent.activeAssistantMessage;
		expect(typeof activeMessage?.messageId).toBe("string");
		expect(typeof activeMessage?.turnId).toBe("string");
		expect(activeMessage?.content).toEqual([{ type: "text", text: "hi" }]);
		drive(agent, { type: "message_end", message: updated });
		expect(agent.activeAssistantMessage).toBeNull();

		const semantic = events.filter((event) =>
			[
				"agent.message.started",
				"agent.message.updated",
				"agent.message.ended",
				"message.committed",
			].includes(String(event.type)),
		);
		const messageIds = semantic.map((event) =>
			"message" in event &&
			typeof event.message === "object" &&
			event.message !== null &&
			"messageId" in event.message
				? event.message.messageId
				: null,
		);
		expect(new Set(messageIds).size).toBe(1);
		expect(messageIds[0]).toEqual(expect.any(String));
		expect(semantic[1]).toMatchObject({
			type: "agent.message.updated",
			update: {
				kind: "content.delta",
				contentType: "text",
				contentIndex: 0,
				delta: "hi",
			},
		});
		expect(events.some((event) => event.type === "message.update")).toBe(false);
	});

	test("keeps tool-loop Pi turns inside one Kit turn", () => {
		const agent = new Agent({});
		const user = userMessage("review this");
		const toolAssistant = assistantMessage("using a tool");
		const toolResult = toolResultMessage("call-1");
		const finalAssistant = assistantMessage("done");

		drive(agent, { type: "agent_start" });
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: user });
		drive(agent, { type: "message_end", message: user });
		drive(agent, { type: "message_start", message: toolAssistant });
		drive(agent, { type: "message_end", message: toolAssistant });
		drive(agent, { type: "message_start", message: toolResult });
		drive(agent, { type: "message_end", message: toolResult });
		drive(agent, {
			type: "turn_end",
			message: toolAssistant,
			toolResults: [toolResult],
		});

		// Pi starts another loop cycle after the tool result. Kit should not start
		// another user-facing turn for that internal cycle.
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: finalAssistant });
		drive(agent, { type: "message_end", message: finalAssistant });
		drive(agent, {
			type: "turn_end",
			message: finalAssistant,
			toolResults: [],
		});
		drive(agent, {
			type: "agent_end",
			messages: [user, toolAssistant, toolResult, finalAssistant],
		});

		expect(agent.turns).toHaveLength(1);
		expect(agent.turns[0]?.messages.map((message) => message.role)).toEqual([
			"user",
			"assistant",
			"toolResult",
			"assistant",
		]);
	});

	test("a new prompt starts a new Kit turn after a previous response", async () => {
		const agent = new Agent({});
		(agent as unknown as { pi: { prompt: () => Promise<void> } }).pi.prompt =
			async () => {};

		const firstUser = userMessage("first");
		const firstAssistant = assistantMessage("first answer");
		await agent.prompt(firstUser);
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: firstUser });
		drive(agent, { type: "message_end", message: firstUser });
		drive(agent, { type: "message_start", message: firstAssistant });
		drive(agent, { type: "message_end", message: firstAssistant });
		drive(agent, {
			type: "turn_end",
			message: firstAssistant,
			toolResults: [],
		});

		const secondUser = userMessage("second");
		const secondAssistant = assistantMessage("second answer");
		await agent.prompt(secondUser);
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: secondUser });
		drive(agent, { type: "message_end", message: secondUser });
		drive(agent, { type: "message_start", message: secondAssistant });
		drive(agent, { type: "message_end", message: secondAssistant });

		expect(agent.turns).toHaveLength(2);
		expect(agent.turns.map((turn) => turn.messages[0]?.role)).toEqual([
			"user",
			"user",
		]);
	});

	test("steering stays in the current Kit turn and follow-up starts a new one", () => {
		const agent = new Agent({});
		const user = userMessage("initial");
		const assistant = assistantMessage("working");
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: user });
		drive(agent, { type: "message_end", message: user });
		drive(agent, { type: "message_start", message: assistant });
		drive(agent, { type: "message_end", message: assistant });
		drive(agent, { type: "turn_end", message: assistant, toolResults: [] });

		const steering = userMessage("steer");
		agent.steer(steering);
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: steering });
		drive(agent, { type: "message_end", message: steering });
		drive(agent, {
			type: "message_end",
			message: assistantMessage("steered response"),
		});

		const followUp = userMessage("follow up");
		const secondFollowUp = userMessage("follow up again");
		agent.followUp(followUp);
		agent.followUp(secondFollowUp);
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: followUp });
		drive(agent, { type: "message_end", message: followUp });
		drive(agent, { type: "message_start", message: secondFollowUp });
		drive(agent, { type: "message_end", message: secondFollowUp });
		drive(agent, {
			type: "message_end",
			message: assistantMessage("follow-up response"),
		});

		expect(agent.turns).toHaveLength(2);
		expect(
			agent.turns[0]?.messages
				.filter((message) => message.role === "user")
				.map((message) => message.content),
		).toEqual(["initial", "steer"]);
		expect(
			agent.turns[1]?.messages
				.filter((message) => message.role === "user")
				.map((message) => message.content),
		).toEqual(["follow up", "follow up again"]);
	});
});
