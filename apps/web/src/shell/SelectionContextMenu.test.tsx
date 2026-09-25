import { expect, test } from "bun:test";
import type { MouseEvent, Selection } from "@opentui/core";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import {
	resolveSelectionMenuPosition,
	SelectionContextMenu,
} from "./SelectionContextMenu";
import {
	applySelectionColors,
	formatSelectionAsQuote,
	restoreSelectionColors,
	type SelectionColorRestore,
} from "./selection";
import { TranscriptMessageContextMenu } from "./TranscriptMessageContextMenu";
import { createMessageContextMenuGesture } from "./transcript/message-context-menu";

test("inverts selection colors and restores each renderable", () => {
	const renderable = {
		isDestroyed: false,
		selectionBg: "old-bg",
		selectionFg: "old-fg",
	};
	const selection = {
		selectedRenderables: [renderable],
	} as unknown as Selection;
	const restore: SelectionColorRestore = new Map();

	applySelectionColors(selection, restore, "new-bg", "new-fg");
	expect(renderable).toEqual({
		isDestroyed: false,
		selectionBg: "new-bg",
		selectionFg: "new-fg",
	});

	restoreSelectionColors(restore);
	expect(renderable).toEqual({
		isDestroyed: false,
		selectionBg: "old-bg",
		selectionFg: "old-fg",
	});
	expect(restore.size).toBe(0);

	renderable.isDestroyed = true;
	restore.set(renderable, { background: "old-bg", foreground: "old-fg" });
	expect(() => restoreSelectionColors(restore)).not.toThrow();
	expect(restore.size).toBe(0);
});

test("formats selected text as a Markdown block quote", () => {
	expect(formatSelectionAsQuote("first\nsecond\n")).toBe(
		"> first\n> second\n\n",
	);
	expect(formatSelectionAsQuote("first", true)).toBe("\n> first\n\n");
	expect(formatSelectionAsQuote("")).toBe("");
});

test("positions the menu beside the selection and clamps it to the shell", () => {
	expect(
		resolveSelectionMenuPosition({
			x: 4,
			y: 2,
			containerWidth: 40,
			containerHeight: 12,
		}),
	).toEqual({ left: 4, top: 2 });
	expect(
		resolveSelectionMenuPosition({
			x: 38,
			y: 10,
			containerWidth: 40,
			containerHeight: 12,
		}),
	).toEqual({ left: 28, top: 9 });
});

test("opens a message menu only for a completed secondary click", () => {
	const requests: Array<{ x: number; y: number; markdown: string }> = [];
	const gesture = createMessageContextMenuGesture(
		() => "**original**",
		(request) => requests.push(request),
	);
	const event = {
		button: 2,
		x: 3,
		y: 4,
		preventDefault() {},
		stopPropagation() {},
	} as MouseEvent;

	gesture.onMouseDown(event);
	gesture.onMouseUp(event);
	expect(requests).toEqual([{ x: 3, y: 4, markdown: "**original**" }]);

	// All-motion mouse tracking may report a drag event for browser jitter.
	// Keep the message click valid within a one-cell tolerance.
	gesture.onMouseDown(event);
	gesture.onMouseDrag({ ...event, x: 4 } as MouseEvent);
	gesture.onMouseUp({ ...event, x: 4 } as MouseEvent);
	expect(requests).toHaveLength(2);

	gesture.onMouseDown(event);
	gesture.onMouseDrag({ ...event, x: 5 } as MouseEvent);
	gesture.onMouseUp({ ...event, x: 5 } as MouseEvent);
	gesture.onMouseUp(event);
	expect(requests).toHaveLength(2);
});

test("copies a whole transcript message as Markdown", async () => {
	let copies = 0;
	const setup = await testRender(
		() => (
			<TranscriptMessageContextMenu
				x={2}
				y={2}
				containerWidth={30}
				containerHeight={8}
				onCopyMarkdown={() => {
					copies += 1;
				}}
				onClose={() => {}}
			/>
		),
		{ width: 30, height: 8 },
	);
	try {
		await setup.renderOnce();
		const mouse = createMockMouse(setup.renderer);
		await mouse.click(2, 2);
		expect(copies).toBe(1);
	} finally {
		setup.renderer.destroy();
	}
});

test("activates copy and quote only on a completed click", async () => {
	const actions: string[] = [];
	const setup = await testRender(
		() => (
			<SelectionContextMenu
				x={0}
				y={0}
				containerWidth={30}
				containerHeight={8}
				onCopy={() => actions.push("copy")}
				onQuote={() => actions.push("quote")}
				onClose={() => actions.push("close")}
			/>
		),
		{ width: 30, height: 8 },
	);
	try {
		await setup.renderOnce();
		const mouse = createMockMouse(setup.renderer);
		await mouse.pressDown(2, 1);
		await mouse.release(2, 1);
		expect(actions).toEqual(["copy"]);

		await mouse.pressDown(2, 2);
		await mouse.release(2, 1);
		expect(actions).toEqual(["copy"]);

		await mouse.click(2, 2);
		expect(actions).toEqual(["copy", "quote"]);
	} finally {
		setup.renderer.destroy();
	}
});
