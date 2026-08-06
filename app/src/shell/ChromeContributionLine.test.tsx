import { expect, test } from "bun:test";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { ChromeContributionLine } from "./ChromeContributionLine";
import type { ChromeContribution } from "./chrome-contributions";

test("clicking a chrome contribution invokes its action", async () => {
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
		expect(clicks).toBe(1);
		await mouse.release(1, 0);
		expect(clicks).toBe(1);
	} finally {
		setup.renderer.destroy();
	}
});
