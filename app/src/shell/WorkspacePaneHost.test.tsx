import { afterEach, describe, expect, test } from "bun:test";
import type { MousePointerStyle } from "@opentui/core";
import { createMockMouse, MouseButtons } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { onCleanup } from "solid-js";
import { ANGLE_LEFT } from "./glyphs";
import { WorkspacePaneHost } from "./WorkspacePaneHost";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

const tabs = [{ id: "review", label: "Code review" }];

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

describe("WorkspacePaneHost", () => {
	test("shows an expandable collapsed rail before any tabs open", async () => {
		let expanded = false;
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					tabs={() => []}
					activeTabId={() => ""}
					selectedSurface={() => "transcript"}
					drawerCollapsed={() => true}
					secondary={() => <box />}
					initialWidth={120}
					preferredPaneRatio={() => 0.4}
					minPrimaryColumns={60}
					minSecondaryColumns={() => 30}
					onSelectTranscript={() => {}}
					onSelectTab={() => {}}
					onCloseTab={() => {}}
					onCollapseDrawer={() => {}}
					onExpandDrawer={() => {
						expanded = true;
					}}
					onOpenOverflow={() => {}}
					onPreferredPaneRatioChange={() => {}}
					onPreferredPaneRatioCommit={() => {}}
				>
					<text>transcript body</text>
				</WorkspacePaneHost>
			),
			{ width: 120, height: 10 },
		);
		await testSetup.renderOnce();
		expect(testSetup.captureCharFrame()).toContain(ANGLE_LEFT);
		const mouse = createMockMouse(testSetup.renderer);
		await mouse.click(119, 5);
		expect(expanded).toBeTrue();
	});

	test("retains the hidden workspace surface in narrow transcript mode", async () => {
		let selectedTab = "";
		let closedTab = "";
		let closeCalls = 0;
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
					tabs={() => tabs}
					activeTabId={() => "review"}
					selectedSurface={() => "transcript"}
					drawerCollapsed={() => false}
					secondary={() => <Secondary />}
					initialWidth={100}
					preferredPaneRatio={() => 0.4}
					minPrimaryColumns={70}
					minSecondaryColumns={() => 60}
					onSelectTranscript={() => {}}
					onSelectTab={(tabId) => {
						selectedTab = tabId;
					}}
					onCloseTab={(tabId) => {
						closedTab = tabId;
						closeCalls += 1;
					}}
					onCollapseDrawer={() => {}}
					onExpandDrawer={() => {}}
					onOpenOverflow={() => {}}
					onPreferredPaneRatioChange={() => {}}
					onPreferredPaneRatioCommit={() => {}}
				>
					<text>transcript body</text>
				</WorkspacePaneHost>
			),
			{ width: 100, height: 10 },
		);
		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame).toContain("transcript body");
		expect(frame).toContain("Transcript");
		expect(frame).not.toContain("review body");
		expect(secondaryMounts).toBe(1);
		expect(secondaryCleanups).toBe(0);
		const mouse = createMockMouse(testSetup.renderer);
		await mouse.click(16, 0);
		expect(selectedTab).toBe("review");
		await mouse.click(25, 0);
		expect(closedTab).toBe("review");
		expect(closeCalls).toBe(1);
		selectedTab = "";
		await mouse.click(16, 2);
		await mouse.click(25, 2);
		expect(selectedTab).toBe("");
		expect(closeCalls).toBe(1);
	});

	test("shows an absolute workspace surface when selected in narrow mode", async () => {
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					tabs={() => tabs}
					activeTabId={() => "review"}
					selectedSurface={() => "review"}
					drawerCollapsed={() => false}
					secondary={() => (
						<box
							position="absolute"
							left={0}
							top={0}
							width="100%"
							height="100%"
						>
							<text>narrow review body</text>
						</box>
					)}
					initialWidth={100}
					preferredPaneRatio={() => 0.4}
					minPrimaryColumns={70}
					minSecondaryColumns={() => 60}
					onSelectTranscript={() => {}}
					onSelectTab={() => {}}
					onCloseTab={() => {}}
					onCollapseDrawer={() => {}}
					onExpandDrawer={() => {}}
					onOpenOverflow={() => {}}
					onPreferredPaneRatioChange={() => {}}
					onPreferredPaneRatioCommit={() => {}}
				>
					<text>transcript body</text>
				</WorkspacePaneHost>
			),
			{ width: 100, height: 10 },
		);
		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		expect(frame).toContain("narrow review body");
		expect(frame).not.toContain("transcript body");
	});

	test("renders a wide tab strip while both panes fit", async () => {
		let collapsed = false;
		let collapseCalls = 0;
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					tabs={() => tabs}
					activeTabId={() => "review"}
					selectedSurface={() => "review"}
					drawerCollapsed={() => false}
					secondary={() => <text>review body</text>}
					initialWidth={140}
					preferredPaneRatio={() => 0.5}
					minPrimaryColumns={60}
					minSecondaryColumns={() => 60}
					onSelectTranscript={() => {}}
					onSelectTab={() => {}}
					onCloseTab={() => {}}
					onCollapseDrawer={() => {
						collapsed = true;
						collapseCalls += 1;
					}}
					onExpandDrawer={() => {}}
					onOpenOverflow={() => {}}
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
		expect(frame).toContain("Code review");
		expect(frame).toContain("review body");
		const mouse = createMockMouse(testSetup.renderer);
		await mouse.click(138, 0);
		expect(collapsed).toBeTrue();
		expect(collapseCalls).toBe(1);
		await mouse.click(138, 2);
		expect(collapseCalls).toBe(1);
	});

	test("gives absolute workspace surfaces a visible content viewport", async () => {
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					tabs={() => tabs}
					activeTabId={() => "review"}
					selectedSurface={() => "review"}
					drawerCollapsed={() => false}
					secondary={() => (
						<box
							position="absolute"
							left={0}
							top={0}
							width="100%"
							height="100%"
						>
							<text>absolute review body</text>
						</box>
					)}
					initialWidth={140}
					preferredPaneRatio={() => 0.5}
					minPrimaryColumns={60}
					minSecondaryColumns={() => 60}
					onSelectTranscript={() => {}}
					onSelectTab={() => {}}
					onCloseTab={() => {}}
					onCollapseDrawer={() => {}}
					onExpandDrawer={() => {}}
					onOpenOverflow={() => {}}
					onPreferredPaneRatioChange={() => {}}
					onPreferredPaneRatioCommit={() => {}}
				>
					<text>transcript body</text>
				</WorkspacePaneHost>
			),
			{ width: 140, height: 10 },
		);
		await testSetup.renderOnce();
		expect(testSetup.captureCharFrame()).toContain("absolute review body");
	});

	test("keeps the surface mounted behind a collapsed drawer handle", async () => {
		let cleanups = 0;
		function Secondary() {
			onCleanup(() => {
				cleanups += 1;
			});
			return <text>review body</text>;
		}
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					tabs={() => tabs}
					activeTabId={() => "review"}
					selectedSurface={() => "transcript"}
					drawerCollapsed={() => true}
					secondary={() => <Secondary />}
					initialWidth={120}
					preferredPaneRatio={() => 0.4}
					minPrimaryColumns={50}
					minSecondaryColumns={() => 40}
					onSelectTranscript={() => {}}
					onSelectTab={() => {}}
					onCloseTab={() => {}}
					onCollapseDrawer={() => {}}
					onExpandDrawer={() => {}}
					onOpenOverflow={() => {}}
					onPreferredPaneRatioChange={() => {}}
					onPreferredPaneRatioCommit={() => {}}
				>
					<text>transcript body</text>
				</WorkspacePaneHost>
			),
			{ width: 120, height: 10 },
		);
		await testSetup.renderOnce();
		expect(testSetup.captureCharFrame()).toContain(ANGLE_LEFT);
		expect(testSetup.captureCharFrame()).not.toContain("review body");
		expect(cleanups).toBe(0);
	});

	test("shows a move pointer while the divider is hovered", async () => {
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					tabs={() => tabs}
					activeTabId={() => "review"}
					selectedSurface={() => "review"}
					drawerCollapsed={() => false}
					secondary={() => <box width="100%" height="100%" />}
					initialWidth={100}
					preferredPaneRatio={() => 0.4}
					minPrimaryColumns={40}
					minSecondaryColumns={() => 20}
					onSelectTranscript={() => {}}
					onSelectTab={() => {}}
					onCloseTab={() => {}}
					onCollapseDrawer={() => {}}
					onExpandDrawer={() => {}}
					onOpenOverflow={() => {}}
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
		await mouse.moveTo(60, 3);
		expect(pointerStyles.at(-1)).toBe("move");
		await mouse.moveTo(10, 3);
		expect(pointerStyles.at(-1)).toBe("default");
	});

	test("ignores non-primary-button divider drags", async () => {
		const changes: number[] = [];
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					tabs={() => tabs}
					activeTabId={() => "review"}
					selectedSurface={() => "review"}
					drawerCollapsed={() => false}
					secondary={() => <box width="100%" height="100%" />}
					initialWidth={100}
					preferredPaneRatio={() => 0.4}
					minPrimaryColumns={40}
					minSecondaryColumns={() => 20}
					onSelectTranscript={() => {}}
					onSelectTab={() => {}}
					onCloseTab={() => {}}
					onCollapseDrawer={() => {}}
					onExpandDrawer={() => {}}
					onOpenOverflow={() => {}}
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
		await mouse.drag(60, 3, 50, 3, MouseButtons.RIGHT);
		expect(changes).toHaveLength(0);
	});

	test("collapses when the divider reaches the secondary minimum width", async () => {
		const changes: number[] = [];
		const commits: number[] = [];
		let collapseCalls = 0;
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					tabs={() => tabs}
					activeTabId={() => "review"}
					selectedSurface={() => "review"}
					drawerCollapsed={() => false}
					secondary={() => <box width="100%" height="100%" />}
					initialWidth={100}
					preferredPaneRatio={() => 0.4}
					minPrimaryColumns={40}
					minSecondaryColumns={() => 20}
					onSelectTranscript={() => {}}
					onSelectTab={() => {}}
					onCloseTab={() => {}}
					onCollapseDrawer={() => {
						collapseCalls += 1;
					}}
					onExpandDrawer={() => {}}
					onOpenOverflow={() => {}}
					onPreferredPaneRatioChange={(ratio) => changes.push(ratio)}
					onPreferredPaneRatioCommit={(ratio) => commits.push(ratio)}
				>
					<box width="100%" height="100%" />
				</WorkspacePaneHost>
			),
			{ width: 100, height: 10 },
		);
		await testSetup.renderOnce();
		const mouse = createMockMouse(testSetup.renderer);
		await mouse.drag(60, 3, 80, 3);
		expect(collapseCalls).toBe(1);
		expect(changes.at(-1)).toBeGreaterThan(0.2);
		expect(commits).toHaveLength(0);
	});

	test("drags the divider and commits the preferred ratio", async () => {
		const changes: number[] = [];
		const commits: number[] = [];
		testSetup = await testRender(
			() => (
				<WorkspacePaneHost
					tabs={() => tabs}
					activeTabId={() => "review"}
					selectedSurface={() => "review"}
					drawerCollapsed={() => false}
					secondary={() => <box width="100%" height="100%" />}
					initialWidth={100}
					preferredPaneRatio={() => 0.4}
					minPrimaryColumns={40}
					minSecondaryColumns={() => 20}
					onSelectTranscript={() => {}}
					onSelectTab={() => {}}
					onCloseTab={() => {}}
					onCollapseDrawer={() => {}}
					onExpandDrawer={() => {}}
					onOpenOverflow={() => {}}
					onPreferredPaneRatioChange={(ratio) => changes.push(ratio)}
					onPreferredPaneRatioCommit={(ratio) => commits.push(ratio)}
				>
					<box width="100%" height="100%" />
				</WorkspacePaneHost>
			),
			{ width: 100, height: 10 },
		);
		await testSetup.renderOnce();
		const mouse = createMockMouse(testSetup.renderer);
		await mouse.drag(60, 3, 50, 3);
		expect(changes.at(-1)).toBeCloseTo(0.5);
		expect(commits).toEqual([0.5]);
	});
});
