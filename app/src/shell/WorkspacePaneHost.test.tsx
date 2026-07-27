import { afterEach, describe, expect, test } from "bun:test";
import type { MousePointerStyle } from "@opentui/core";
import { createMockMouse, MouseButtons } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { createSignal, onCleanup } from "solid-js";
import { WorkspacePaneHost } from "./WorkspacePaneHost";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

describe("WorkspacePaneHost", () => {
	test("retains the hidden secondary surface in narrow transcript mode", async () => {
		const [selected, setSelected] = createSignal<"transcript" | "secondary">(
			"transcript",
		);
		let secondaryMounts = 0;
		let secondaryCleanups = 0;
		function Secondary() {
			secondaryMounts += 1;
			onCleanup(() => {
				secondaryCleanups += 1;
			});
			return <text>review body</text>;
		}
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					secondaryOpen
					secondary={<Secondary />}
					initialWidth={100}
					preferredPaneRatio={0.4}
					minPrimaryColumns={70}
					minSecondaryColumns={60}
					narrowTabs={{
						selected,
						secondaryLabel: () => "Activity",
						onSelect: setSelected,
					}}
					onPreferredPaneRatioChange={() => {}}
					onPreferredPaneRatioCommit={() => {}}
				>
					<text>transcript body</text>
				</WorkspacePaneHost>
			),
			{ width: 100, height: 10 },
		);
		await testSetup.renderOnce();
		expect(testSetup.captureCharFrame()).toContain("transcript body");
		expect(testSetup.captureCharFrame()).toContain("Activity");
		expect(secondaryMounts).toBe(1);

		expect(testSetup.captureCharFrame()).not.toContain("review body");
		expect(secondaryMounts).toBe(1);
		expect(secondaryCleanups).toBe(0);
	});

	test("hides narrow tabs while both panes fit", async () => {
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					secondaryOpen
					secondary={<text>review body</text>}
					initialWidth={140}
					preferredPaneRatio={0.5}
					minPrimaryColumns={60}
					minSecondaryColumns={60}
					narrowTabs={{
						selected: () => "secondary",
						secondaryLabel: () => "Code review",
						onSelect: () => {},
					}}
					onPreferredPaneRatioChange={() => {}}
					onPreferredPaneRatioCommit={() => {}}
				>
					<text>transcript body</text>
				</WorkspacePaneHost>
			),
			{ width: 140, height: 10 },
		);
		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame).toContain("transcript body");
		expect(frame).toContain("review body");
		expect(frame).not.toContain("Code review");
		expect(frame).not.toContain("Transcript");
		expect(frame.split("\n")[0]).toContain("transcript body");
		expect(frame.split("\n")[0]).toContain("review body");
	});

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
