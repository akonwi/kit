import { afterEach, expect, test } from "bun:test";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import type { ReviewSkippedSection } from "./model";
import { ReviewSkippedSectionRow } from "./ReviewContent";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

const section: ReviewSkippedSection = {
	id: "context-1",
	beforeHunkIndex: 1,
	rawPatch: "@@ -4,2 +4,2 @@\n unchanged\n context",
	lineCount: 2,
	additionStart: 4,
	deletionStart: 4,
};

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

test("clicking a collapsed review section activates expansion", async () => {
	let activations = 0;
	testSetup = await testRender(
		() => (
			<ReviewSkippedSectionRow
				section={section}
				interactive
				selected={false}
				expanded={false}
				onActivate={() => {
					activations += 1;
				}}
			/>
		),
		{ width: 48, height: 2 },
	);
	await testSetup.renderOnce();
	expect(testSetup.captureCharFrame()).toContain("2 unchanged lines hidden");

	const mouse = createMockMouse(testSetup.renderer);
	await mouse.click(2, 0);
	expect(activations).toBe(1);
});
