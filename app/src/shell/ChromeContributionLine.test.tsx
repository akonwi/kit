import { expect, test } from "bun:test";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { createSignal } from "solid-js";
import { ChromeContributionLine } from "./ChromeContributionLine";
import type { ChromeContribution } from "./chrome-contributions";

test("clicking a chrome contribution invokes its callback", async () => {
	let clicks = 0;
	const contribution: ChromeContribution = {
		id: "git.location",
		content: [{ text: "PR #25" }],
		plainText: "PR #25",
		side: "right",
		onClick: () => {
			clicks += 1;
		},
	};
	const setup = await testRender(
		() => <ChromeContributionLine contributions={[contribution]} />,
		{ width: 20, height: 2 },
	);
	try {
		await setup.renderOnce();
		const mouse = createMockMouse(setup.renderer);
		await mouse.pressDown(1, 0);
		expect(clicks).toBe(0);
		await mouse.release(1, 0);
		expect(clicks).toBe(1);
	} finally {
		setup.renderer.destroy();
	}
});

test("a gesture that starts while disabled does not click through", async () => {
	let clicks = 0;
	const contribution: ChromeContribution = {
		id: "session.name",
		content: [{ text: "Workspace" }],
		plainText: "Workspace",
		side: "left",
		onClick: () => {
			clicks += 1;
		},
	};
	const setup = await testRender(
		() => {
			const [disabled, setDisabled] = createSignal(true);
			return (
				<box onMouseDown={() => setDisabled(false)}>
					<ChromeContributionLine
						contributions={[contribution]}
						disabled={disabled}
					/>
				</box>
			);
		},
		{ width: 20, height: 2 },
	);
	try {
		await setup.renderOnce();
		const mouse = createMockMouse(setup.renderer);
		await mouse.pressDown(1, 0);
		await mouse.release(1, 0);
		expect(clicks).toBe(0);
		await setup.renderOnce();
		await mouse.click(1, 0);
		expect(clicks).toBe(1);
	} finally {
		setup.renderer.destroy();
	}
});

test("dragging from a chrome contribution does not activate it", async () => {
	let clicks = 0;
	const contribution: ChromeContribution = {
		id: "git.location",
		content: [{ text: "PR #25" }],
		plainText: "PR #25",
		side: "right",
		onClick: () => {
			clicks += 1;
		},
	};
	const setup = await testRender(
		() => <ChromeContributionLine contributions={[contribution]} />,
		{ width: 20, height: 2 },
	);
	try {
		await setup.renderOnce();
		const mouse = createMockMouse(setup.renderer);
		await mouse.drag(1, 0, 10, 0);
		expect(clicks).toBe(0);
	} finally {
		setup.renderer.destroy();
	}
});

test("clicking a declarative URL action uses the renderer opener", async () => {
	const opened: string[] = [];
	const contribution: ChromeContribution = {
		id: "git.pull-request",
		content: [{ text: "PR #25" }],
		plainText: "PR #25",
		side: "right",
		action: { type: "open-url", url: "https://github.com/owner/repo/pull/25" },
	};
	const setup = await testRender(
		() => (
			<ChromeContributionLine
				contributions={[contribution]}
				onOpenUrl={(url) => {
					opened.push(url);
				}}
			/>
		),
		{ width: 20, height: 2 },
	);
	try {
		await setup.renderOnce();
		const mouse = createMockMouse(setup.renderer);
		await mouse.click(1, 0);
		expect(opened).toEqual(["https://github.com/owner/repo/pull/25"]);
	} finally {
		setup.renderer.destroy();
	}
});
