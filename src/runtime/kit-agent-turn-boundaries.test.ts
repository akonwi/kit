import { describe, expect, test } from "bun:test";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { KitAgent } from "./kit-agent";

function drive(agent: KitAgent, event: unknown) {
	const events = (
		agent as unknown as {
			processPiEvent: (e: unknown) => unknown[];
			emit: (e: unknown) => void;
		}
	).processPiEvent(event);
	for (const nextEvent of events) {
		(agent as unknown as { emit: (e: unknown) => void }).emit(nextEvent);
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

describe("KitAgent user-facing turn boundaries", () => {
	test("keeps tool-loop Pi turns inside one Kit turn", () => {
		const agent = new KitAgent({});
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
		const agent = new KitAgent({});
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
		const agent = new KitAgent({});
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
		agent.followUp(followUp);
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: followUp });
		drive(agent, { type: "message_end", message: followUp });
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
		const firstFollowUpMessage = agent.turns[1]?.messages[0];
		expect(firstFollowUpMessage?.role).toBe("user");
		expect(
			firstFollowUpMessage?.role === "user"
				? firstFollowUpMessage.content
				: undefined,
		).toBe("follow up");
	});
});
