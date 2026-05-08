import { describe, expect, test } from "bun:test";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { AgentRuntime } from "./agent-runtime";

type SubmissionTestRuntime = {
	agent: {
		state: { isStreaming: boolean };
		prompt: (message: AgentMessage) => Promise<void>;
		followUp: (message: AgentMessage) => void;
	};
	waitForRecovery: () => Promise<void>;
	syncPendingState: () => void;
	submitPromptCommandMessage: InstanceType<
		typeof AgentRuntime
	>["submitPromptCommandMessage"];
};

describe("AgentRuntime prompt-command submission", () => {
	test("queues structured prompt-command messages while streaming", async () => {
		let promptedMessage: AgentMessage | null = null;
		let queuedMessage: AgentMessage | null = null;
		let syncedPendingState = false;

		const runtime = Object.create(
			AgentRuntime.prototype,
		) as SubmissionTestRuntime;
		runtime.agent = {
			state: { isStreaming: true },
			prompt: async (message) => {
				promptedMessage = message;
			},
			followUp: (message) => {
				queuedMessage = message;
			},
		};
		runtime.waitForRecovery = async () => {};
		runtime.syncPendingState = () => {
			syncedPendingState = true;
		};

		await runtime.submitPromptCommandMessage(
			"review",
			"auth flow",
			"Please review the auth flow.",
		);

		expect(promptedMessage).toBeNull();
		expect(syncedPendingState).toBe(true);
		expect(queuedMessage).toMatchObject({
			role: "user",
			synthetic: {
				kind: "prompt-command",
				command: "review",
				args: "auth flow",
			},
		});
	});
});
