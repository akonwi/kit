import { expect, test } from "bun:test";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { insertPagerSelectionQuote } from "./PagerContent";
import {
	PagerDocument,
	type PagerSelectionMenuController,
} from "./PagerDocument";

test("inserts quoted selections without replacing an unsaved note", () => {
	const result = insertPagerSelectionQuote({
		text: "unsaved draft",
		selection: "selected text",
		cursorOffset: 7,
	});
	expect(result).toEqual({
		text: "unsaved\n> selected text\n\n draft",
		cursorOffset: 25,
	});
});

test("detects text selections for the pager context menu", async () => {
	const menuVisibility: boolean[] = [];
	let menuController: PagerSelectionMenuController | undefined;
	const setup = await testRender(
		() => (
			<PagerDocument
				sectionTitle="Section"
				body="Selectable pager content"
				zIndex={0}
				bindScroll={() => {}}
				copyText={async () => {}}
				onCopyError={() => {}}
				onQuote={() => {}}
				onMenuVisibilityChange={(visible) => menuVisibility.push(visible)}
				registerSelectionMenu={(controller) => {
					menuController = controller;
				}}
			/>
		),
		{ width: 60, height: 16 },
	);
	try {
		let frame = "";
		for (let attempt = 0; attempt < 20; attempt += 1) {
			await Bun.sleep(10);
			await setup.renderOnce();
			frame = setup.captureCharFrame();
			if (frame.includes("Selectable pager content")) break;
		}
		const row = frame
			.split("\n")
			.findIndex((line) => line.includes("Selectable pager content"));
		const column = frame.split("\n")[row]?.indexOf("Selectable") ?? -1;
		expect(row).toBeGreaterThanOrEqual(0);
		expect(column).toBeGreaterThanOrEqual(0);

		const mouse = createMockMouse(setup.renderer);
		await mouse.drag(column, row, column + 5, row);

		expect(setup.renderer.getSelection()?.getSelectedText()).toBe("Selec");
		expect(menuVisibility).toEqual([true]);

		menuController?.close();
		expect(setup.renderer.getSelection()).toBeNull();
		expect(menuVisibility).toEqual([true, false]);
	} finally {
		setup.renderer.destroy();
	}
});
