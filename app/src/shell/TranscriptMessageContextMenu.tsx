import type { KeyEvent } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import { onCleanup } from "solid-js";
import { resolveContextMenuPosition } from "./SelectionContextMenu";
import { theme } from "./theme";

const MENU_WIDTH = 8;
const MENU_HEIGHT = 1;

export type TranscriptMessageContextMenuProps = {
	x: number;
	y: number;
	containerWidth: number;
	containerHeight: number;
	onCopyMarkdown: () => void;
	onClose: () => void;
};

export function TranscriptMessageContextMenu(
	props: TranscriptMessageContextMenuProps,
) {
	const renderer = useRenderer();
	let pressed = false;
	const position = () =>
		resolveContextMenuPosition({
			x: props.x,
			y: props.y,
			containerWidth: props.containerWidth,
			containerHeight: props.containerHeight,
			menuWidth: MENU_WIDTH,
			menuHeight: MENU_HEIGHT,
		});

	function handleKeyDown(event: KeyEvent): void {
		if (event.name === "escape") {
			event.preventDefault();
			props.onClose();
			return;
		}
		if (event.name === "return" || event.name === "c") {
			event.preventDefault();
			props.onCopyMarkdown();
		}
	}

	onCleanup(() => renderer.setMousePointer("default"));

	return (
		<box
			position="absolute"
			left={position().left}
			top={position().top}
			width={MENU_WIDTH}
			height={MENU_HEIGHT}
			zIndex={60}
			paddingX={1}
			backgroundColor={theme.pickerFocusedBg}
			focusable
			focused
			onKeyDown={handleKeyDown}
			onMouseOver={() => renderer.setMousePointer("pointer")}
			onMouseOut={() => {
				pressed = false;
				renderer.setMousePointer("default");
			}}
			onMouseDown={(event) => {
				event.preventDefault();
				event.stopPropagation();
				pressed = event.button === 0;
			}}
			onMouseUp={(event) => {
				event.preventDefault();
				event.stopPropagation();
				const shouldCopy = event.button === 0 && pressed;
				pressed = false;
				if (shouldCopy) props.onCopyMarkdown();
			}}
		>
			<text
				selectable={false}
				fg={theme.pickerFocusedText}
				bg={theme.pickerFocusedBg}
			>
				Copy
			</text>
		</box>
	);
}
