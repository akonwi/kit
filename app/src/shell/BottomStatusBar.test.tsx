import { afterEach, describe, expect, test } from "bun:test";
import { testRender } from "@opentui/solid";
import type { AgentRuntime } from "../runtime/agent-runtime";
import {
	BottomStatusBar,
	VCS_LOCATION_CONTRIBUTION_ID,
} from "./BottomStatusBar";
import { createFooterStatusController } from "./footer-status";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

function runtime(pending = 0): AgentRuntime {
	return {
		getPendingMessageCount: () => pending,
		subscribe: () => () => {},
	} as unknown as AgentRuntime;
}

describe("BottomStatusBar", () => {
	test("keeps the fixed footer quiet without actionable guidance", async () => {
		const status = createFooterStatusController();
		status.setContribution({
			id: VCS_LOCATION_CONTRIBUTION_ID,
			content: "/Users/example/project (main)",
			side: "right",
		});
		testSetup = await testRender(
			() => (
				<BottomStatusBar
					runtime={runtime()}
					status={status}
					composerMode="normal"
					shellWidth={72}
					onOpenOverflow={() => {}}
				/>
			),
			{ width: 72, height: 6 },
		);

		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame).toContain("/Users/example/project (main)");
		expect(frame).not.toContain("ready");
		expect(frame.trimEnd().split("\n")).toHaveLength(2);
	});

	test("uses the available footer width for an uncontested VCS location", async () => {
		const status = createFooterStatusController();
		const location =
			"/Users/example/a-very-long-project-directory-that-exceeds-the-compact-cap (feature/shell-chrome)";
		status.setContribution({
			id: VCS_LOCATION_CONTRIBUTION_ID,
			content: location,
			side: "right",
		});
		testSetup = await testRender(
			() => (
				<BottomStatusBar
					runtime={runtime()}
					status={status}
					composerMode="normal"
					shellWidth={120}
					onOpenOverflow={() => {}}
				/>
			),
			{ width: 120, height: 6 },
		);

		await testSetup.renderOnce();
		expect(testSetup.captureCharFrame()).toContain(location);
	});

	test("does not compact VCS merely because a plugin contribution exists", async () => {
		const status = createFooterStatusController();
		const location =
			"/Users/example/a-very-long-project-directory-that-exceeds-the-compact-cap (feature/shell-chrome)";
		status.setContribution({
			id: VCS_LOCATION_CONTRIBUTION_ID,
			content: location,
			side: "right",
		});
		status.setContribution({
			id: "plugin:ci",
			content: "CI passing",
			side: "right",
		});
		testSetup = await testRender(
			() => (
				<BottomStatusBar
					runtime={runtime()}
					status={status}
					composerMode="normal"
					shellWidth={160}
					onOpenOverflow={() => {}}
				/>
			),
			{ width: 160, height: 6 },
		);

		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame).toContain(location);
		expect(frame).toContain("CI passing");
	});

	test("preserves privileged VCS when a long plugin can overflow", async () => {
		const status = createFooterStatusController();
		const location = "/Users/example/project (feature/shell-chrome)";
		status.setContribution({
			id: VCS_LOCATION_CONTRIBUTION_ID,
			content: location,
			side: "right",
		});
		status.setContribution({
			id: "plugin:long",
			content:
				"a plugin contribution that is intentionally much too long to fit beside the privileged repository location",
			side: "right",
		});
		testSetup = await testRender(
			() => (
				<BottomStatusBar
					runtime={runtime()}
					status={status}
					composerMode="normal"
					shellWidth={100}
					onOpenOverflow={() => {}}
				/>
			),
			{ width: 100, height: 6 },
		);

		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame).toContain(location);
		expect(frame).toContain("… +1");
	});

	test("keeps footer plugin overflow accessible beside guidance and VCS", async () => {
		const status = createFooterStatusController();
		status.setContribution({
			id: VCS_LOCATION_CONTRIBUTION_ID,
			content: "/Users/example/project (main)",
			side: "right",
		});
		for (let index = 1; index <= 3; index += 1) {
			status.setContribution({
				id: `plugin:${index}`,
				content: `plugin status ${index}`,
				side: "right",
			});
		}
		testSetup = await testRender(
			() => (
				<BottomStatusBar
					runtime={runtime(2)}
					status={status}
					composerMode="normal"
					shellWidth={40}
					onOpenOverflow={() => {}}
				/>
			),
			{ width: 40, height: 6 },
		);

		await testSetup.renderOnce();
		expect(testSetup.captureCharFrame()).toContain("… +");
	});

	test("shows queued-message guidance without wrapping", async () => {
		const status = createFooterStatusController();
		testSetup = await testRender(
			() => (
				<BottomStatusBar
					runtime={runtime(2)}
					status={status}
					composerMode="normal"
					shellWidth={52}
					restoreQueueBinding="Ctrl+R"
					onOpenOverflow={() => {}}
				/>
			),
			{ width: 52, height: 6 },
		);

		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame).toContain("queued messages: 2 · Ctrl+R restore");
		expect(frame.trimEnd().split("\n")).toHaveLength(2);
	});
});
