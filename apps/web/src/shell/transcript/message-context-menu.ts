import type { MouseEvent } from "@opentui/core";
import type { OpenMessageContextMenu } from "./types";

export function createMessageContextMenuGesture(
	markdown: () => string,
	open: OpenMessageContextMenu,
): {
	onMouseDown: (event: MouseEvent) => void;
	onMouseDrag: (event: MouseEvent) => void;
	onMouseUp: (event: MouseEvent) => void;
} {
	let pressed = false;
	let dragged = false;
	let pressX = 0;
	let pressY = 0;

	return {
		onMouseDown(event) {
			pressed = event.button === 2;
			dragged = false;
			pressX = event.x;
			pressY = event.y;
		},
		onMouseDrag(event) {
			if (
				pressed &&
				event.button === 2 &&
				(Math.abs(event.x - pressX) > 1 || Math.abs(event.y - pressY) > 1)
			) {
				dragged = true;
			}
		},
		onMouseUp(event) {
			const source = markdown();
			const shouldOpen =
				event.button === 2 && pressed && !dragged && source.length > 0;
			pressed = false;
			dragged = false;
			if (!shouldOpen) return;
			event.preventDefault();
			event.stopPropagation();
			open({ x: event.x, y: event.y, markdown: source });
		},
	};
}
