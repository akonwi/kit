import { afterEach, expect, test } from "bun:test";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { TIMES } from "./glyphs";
import { WorkspaceTabStrip } from "./WorkspaceTabStrip";

let setup: Awaited<ReturnType<typeof testRender>> | undefined;
afterEach(() => setup?.renderer.destroy());

test("keeps persistent tabs selectable without rendering a close action", async () => {
	let selected = "";
	setup = await testRender(
		() => (
			<WorkspaceTabStrip
				mode="narrow"
				width={() => 80}
				tabs={() => [{ id: "review", label: "Code review", closable: false }]}
				activeTabId={() => "review"}
				selectedSurface={() => "transcript"}
				onSelectTranscript={() => {}}
				onSelectTab={(tabId) => {
					selected = tabId;
				}}
				onCloseTab={() => {
					throw new Error("persistent tab exposed a close action");
				}}
				onCollapse={() => {}}
				onOpenOverflow={() => {}}
			/>
		),
		{ width: 80, height: 4 },
	);
	await setup.renderOnce();
	expect(setup.captureCharFrame()).not.toContain(TIMES);
	const mouse = createMockMouse(setup.renderer);
	await mouse.click(16, 0);
	expect(selected).toBe("review");
});

test("renders and activates narrow workspace tabs", async () => {
	let transcriptSelected = false;
	let selected = "";
	let closed = "";
	setup = await testRender(
		() => (
			<WorkspaceTabStrip
				mode="narrow"
				width={() => 80}
				tabs={() => [{ id: "review", label: "Review" }]}
				activeTabId={() => "review"}
				selectedSurface={() => "transcript"}
				onSelectTranscript={() => {
					transcriptSelected = true;
				}}
				onSelectTab={(tabId) => {
					selected = tabId;
				}}
				onCloseTab={(tabId) => {
					closed = tabId;
				}}
				onCollapse={() => {}}
				onOpenOverflow={() => {}}
			/>
		),
		{ width: 80, height: 4 },
	);
	await setup.renderOnce();
	expect(setup.captureCharFrame()).toContain("Transcript");
	const mouse = createMockMouse(setup.renderer);
	await mouse.click(5, 0);
	expect(transcriptSelected).toBeTrue();
	await mouse.click(16, 0);
	expect(selected).toBe("review");
	await mouse.click(20, 0);
	expect(closed).toBe("review");
});
