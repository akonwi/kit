import { describe, expect, mock, test } from "bun:test";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { handoffCommand } from "./handoff";
import type { CommandContext } from "./types";

function context(
	persistSessions: boolean,
	handoffSession: AgentRuntime["handoffSession"],
): CommandContext {
	return {
		runtime: { handoffSession } as unknown as AgentRuntime,
		persistSessions,
		args: "continue here",
		toast: () => {},
	} as unknown as CommandContext;
}

describe("handoff command", () => {
	test("uses the interactive session persistence policy", async () => {
		const handoffSession = mock(
			async (..._args: Parameters<AgentRuntime["handoffSession"]>) =>
				({}) as Awaited<ReturnType<AgentRuntime["handoffSession"]>>,
		);

		await handoffCommand.execute(context(false, handoffSession));

		expect(handoffSession).toHaveBeenCalledWith("continue here", {
			persist: false,
		});
	});
});
