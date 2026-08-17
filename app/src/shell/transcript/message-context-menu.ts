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

	return {
		onMouseDown(event) {
			pressed = event.button === 2;
			dragged = false;
		},
		onMouseDrag(event) {
			if (pressed && event.button === 2) dragged = true;
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
