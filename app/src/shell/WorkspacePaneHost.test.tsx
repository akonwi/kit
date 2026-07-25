import { afterEach, describe, expect, test } from "bun:test";
import type { MousePointerStyle } from "@opentui/core";
import { createMockMouse, MouseButtons } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { WorkspacePaneHost } from "./WorkspacePaneHost";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

describe("WorkspacePaneHost", () => {
	test("shows a move pointer while the divider is hovered", async () => {
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					secondaryOpen
					secondary={<box width="100%" height="100%" />}
					initialWidth={100}
					preferredPaneRatio={0.4}
					minPrimaryColumns={40}
					minSecondaryColumns={20}
					onPreferredPaneRatioChange={() => {}}
					onPreferredPaneRatioCommit={() => {}}
				>
					<box width="100%" height="100%" />
				</WorkspacePaneHost>
			),
			{ width: 100, height: 10 },
		);
		await testSetup.renderOnce();
		const pointerStyles: MousePointerStyle[] = [];
		const setMousePointer = testSetup.renderer.setMousePointer.bind(
			testSetup.renderer,
		);
		testSetup.renderer.setMousePointer = (style) => {
			pointerStyles.push(style);
			setMousePointer(style);
		};

		const mouse = createMockMouse(testSetup.renderer);
		await mouse.moveTo(60, 2);
		expect(pointerStyles.at(-1)).toBe("move");
		await mouse.moveTo(10, 2);
		expect(pointerStyles.at(-1)).toBe("default");
	});

	test("ignores non-primary-button divider drags", async () => {
		const changes: number[] = [];
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					secondaryOpen
					secondary={<box width="100%" height="100%" />}
					initialWidth={100}
					preferredPaneRatio={0.4}
					minPrimaryColumns={40}
					minSecondaryColumns={20}
					onPreferredPaneRatioChange={(ratio) => changes.push(ratio)}
					onPreferredPaneRatioCommit={() => {}}
				>
					<box width="100%" height="100%" />
				</WorkspacePaneHost>
			),
			{ width: 100, height: 10 },
		);
		await testSetup.renderOnce();

		const mouse = createMockMouse(testSetup.renderer);
		await mouse.drag(60, 2, 50, 2, MouseButtons.RIGHT);

		expect(changes).toHaveLength(0);
	});

	test("drags the divider and commits the preferred ratio", async () => {
		const changes: number[] = [];
		const commits: number[] = [];
		let dividerMouseDown = false;
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					secondaryOpen
					secondary={<box width="100%" height="100%" />}
					initialWidth={100}
					preferredPaneRatio={0.4}
					minPrimaryColumns={40}
					minSecondaryColumns={20}
					onPreferredPaneRatioChange={(ratio) => changes.push(ratio)}
					onPreferredPaneRatioCommit={(ratio) => commits.push(ratio)}
					onDividerMouseDown={() => {
						dividerMouseDown = true;
					}}
				>
					<box width="100%" height="100%" />
				</WorkspacePaneHost>
			),
			{ width: 100, height: 10 },
		);
		await testSetup.renderOnce();

		const mouse = createMockMouse(testSetup.renderer);
		await mouse.drag(60, 2, 50, 2);

		expect(dividerMouseDown).toBeTrue();
		expect(changes.at(-1)).toBeCloseTo(0.5);
		expect(commits).toEqual([0.5]);
	});
});
