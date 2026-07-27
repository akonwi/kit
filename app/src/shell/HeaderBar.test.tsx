import { afterEach, describe, expect, test } from "bun:test";
import { testRender } from "@opentui/solid";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { HeaderBar } from "./HeaderBar";
import { createHeaderStatusController } from "./header-status";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

function runtime(): AgentRuntime {
	return {
		contextStats: { percent: 42 },
		agentInfo: {
			model: { name: "Claude Test" },
			thinkingLevel: "medium",
		},
		subscribe: () => () => {},
	} as unknown as AgentRuntime;
}

describe("HeaderBar", () => {
	test("renders fixed shell chrome without wrapping", async () => {
		const header = createHeaderStatusController();
		header.setContribution({
			id: "plugin:one",
			content: "CI passing",
			side: "right",
		});
		testSetup = await testRender(
			() => (
				<HeaderBar
					runtime={runtime()}
					header={header}
					sessionName="Workspace"
					shellWidth={80}
					transcriptWidth={80}
					onOpenOverflow={() => {}}
				/>
			),
			{ width: 80, height: 6 },
		);

		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame).toContain("Workspace");
		expect(frame).not.toContain("k i t");
		expect(frame).toContain("Claude Test · medium · CI passing");
		expect(frame.trimEnd().split("\n")).toHaveLength(2);
	});

	test("keeps plugin overflow accessible at very narrow widths", async () => {
		const header = createHeaderStatusController();
		header.setContribution({
			id: "plugin:one",
			content: "plugin status",
			side: "right",
		});
		testSetup = await testRender(
			() => (
				<HeaderBar
					runtime={runtime()}
					header={header}
					sessionName="Workspace"
					shellWidth={20}
					transcriptWidth={20}
					onOpenOverflow={() => {}}
				/>
			),
			{ width: 20, height: 6 },
		);

		await testSetup.renderOnce();
		await testSetup.renderOnce();
		expect(testSetup.captureCharFrame()).toContain("…");
	});

	test("replaces whole overflowing plugin items with a count", async () => {
		const header = createHeaderStatusController();
		for (let index = 1; index <= 4; index += 1) {
			header.setContribution({
				id: `plugin:${index}`,
				content: `plugin contribution ${index}`,
				side: "right",
			});
		}
		testSetup = await testRender(
			() => (
				<HeaderBar
					runtime={runtime()}
					header={header}
					sessionName="Workspace"
					shellWidth={64}
					transcriptWidth={64}
					onOpenOverflow={() => {}}
				/>
			),
			{ width: 64, height: 6 },
		);

		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame).toContain("… +");
		expect(frame).not.toContain("plugin contribution 4");
	});
});
